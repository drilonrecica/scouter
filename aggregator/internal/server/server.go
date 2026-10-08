// Package server exposes the state document to the phone.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

// Heartbeat keeps the SSE connection alive through proxies (Coolify's
// Traefik) and lets the phone notice a dead connection quickly.
var Heartbeat = 25 * time.Second

// Server is the phone-facing API. Mount extra handlers (the admin UI) with Handle.
type Server struct {
	mux        *http.ServeMux
	clients    atomic.Int64
	phoneToken string
}

// New returns the server. phoneToken guards everything but /healthz.
func New(store *state.Store, phoneToken string) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	s.mux.Handle("GET /v1/state", requireToken(phoneToken, getState(store)))
	s.mux.Handle("GET /v1/stream", requireToken(phoneToken, s.stream(store)))
	s.mux.Handle("PUT /v1/settings", requireToken(phoneToken, putSettings(store)))
	s.mux.Handle("POST /v1/heartbeat", requireToken(phoneToken, postHeartbeat(store)))
	s.phoneToken = phoneToken
	return s
}

// AcceptAgentEvents takes Claude Code hook events. hookToken (optional) is a
// narrower token for the laptop's hooks; the phone token also works.
func (s *Server) AcceptAgentEvents(store *state.Store, hookToken string) {
	tokens := []string{s.phoneToken}
	if hookToken != "" {
		tokens = append(tokens, hookToken)
	}
	s.mux.Handle("POST /v1/agent-events", requireAnyToken(tokens, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
			Event     string `json:"hook_event_name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&ev); err != nil || ev.SessionID == "" {
			http.Error(w, "bad event", http.StatusBadRequest)
			return
		}
		store.AgentEvent(ev.SessionID, ev.Cwd, ev.Event)
		w.WriteHeader(http.StatusNoContent)
	})))
}

func requireAnyToken(tokens []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		ok := 0
		for _, t := range tokens {
			ok |= subtle.ConstantTimeCompare(got, []byte("Bearer "+t))
		}
		if ok != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AcceptReleases lets a release tool publish a new APK with releaseToken,
// a token that can do nothing else. GET reports what the phone runs, so the
// tool can confirm the update landed. Off when releaseToken is empty.
func (s *Server) AcceptReleases(store *state.Store, apkPath, releaseToken string, log *slog.Logger) {
	if releaseToken == "" {
		return
	}
	s.mux.Handle("POST /v1/release", requireToken(releaseToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel, err := state.SaveAPK(apkPath, http.MaxBytesReader(w, r.Body, state.MaxAPK+1))
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, state.ErrNotAPK) {
				code = http.StatusBadRequest
			}
			http.Error(w, err.Error(), code)
			return
		}
		store.SetApp(rel)
		log.Info("apk uploaded", "by", "release token", "sha256", rel.SHA256, "bytes", rel.Size)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rel)
	})))
	s.mux.Handle("GET /v1/release", requireToken(releaseToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"offered": store.Get().App, "phone": store.Heartbeat()})
	})))
}

// ServeAPK lets the phone download the APK at path for over-the-air updates.
func (s *Server) ServeAPK(path string) {
	s.mux.Handle("GET /v1/app.apk", requireToken(s.phoneToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})))
}

func postHeartbeat(store *state.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var h state.Heartbeat
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&h); err != nil {
			http.Error(w, "bad heartbeat: "+err.Error(), http.StatusBadRequest)
			return
		}
		store.SetHeartbeat(h)
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Handle mounts another handler, e.g. the admin UI under /admin/.
func (s *Server) Handle(pattern string, h http.Handler) { s.mux.Handle(pattern, h) }

// Clients is the number of open streams: normally 1, the phone.
func (s *Server) Clients() int64 { return s.clients.Load() }

// putSettings lets the phone edit settings. It replaces them as a whole, so
// the phone sends back the full object it received with its changes applied.
func putSettings(store *state.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		var next state.Settings
		if err := dec.Decode(&next); err != nil {
			http.Error(w, "bad settings: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.SetSettings(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func etag(st state.State) string { return `"` + strconv.FormatInt(st.Version, 10) + `"` }

func getState(store *state.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := store.Get()
		w.Header().Set("ETag", etag(st))
		w.Header().Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag(st) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st)
	})
}

// stream sends the full state on connect and after every change. The
// document is a few KB, so whole-document pushes beat diffing on both ends.
func (s *Server) stream(store *state.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.clients.Add(1)
		defer s.clients.Add(-1)
		rc := http.NewResponseController(w)
		w.Header().Set("Content-Type", "text/event-stream")
		// Proxies must pass every event through at once. A compressing proxy
		// (Coolify's Traefik with gzip on) buffered the whole stream: the phone
		// connected but never got a byte. "identity" marks the body as already
		// encoded, which compressors skip; no-transform covers Cloudflare.
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Content-Encoding", "identity")
		w.Header().Set("X-Accel-Buffering", "no")

		updates, cancel := store.Subscribe()
		defer cancel()

		send := func(st state.State) error {
			b, err := json.Marshal(st)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: state\ndata: %s\n\n", st.Version, b); err != nil {
				return err
			}
			return rc.Flush()
		}
		if err := send(store.Get()); err != nil {
			return
		}
		hb := time.NewTicker(Heartbeat)
		defer hb.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case st := <-updates:
				if send(st) != nil {
					return
				}
			case <-hb.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			}
		}
	})
}
