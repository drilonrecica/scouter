// Package server exposes the state document to the phone.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
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
	mux     *http.ServeMux
	clients atomic.Int64
}

// New returns the server. phoneToken guards everything but /healthz.
func New(store *state.Store, phoneToken string) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	s.mux.Handle("GET /v1/state", requireToken(phoneToken, getState(store)))
	s.mux.Handle("GET /v1/stream", requireToken(phoneToken, s.stream(store)))
	s.mux.Handle("PUT /v1/settings", requireToken(phoneToken, putSettings(store)))
	return s
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
		w.Header().Set("Cache-Control", "no-cache")
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
