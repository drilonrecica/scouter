// Package server exposes the state document to the phone.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

// Heartbeat keeps the SSE connection alive through proxies (Coolify's
// Traefik) and lets the phone notice a dead connection quickly.
var Heartbeat = 25 * time.Second

// New returns the HTTP handler. phoneToken guards everything but /healthz.
func New(store *state.Store, phoneToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	mux.Handle("GET /v1/state", requireToken(phoneToken, getState(store)))
	mux.Handle("GET /v1/stream", requireToken(phoneToken, stream(store)))
	return mux
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
func stream(store *state.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
