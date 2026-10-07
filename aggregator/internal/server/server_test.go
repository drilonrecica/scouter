package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

func get(t *testing.T, srv *httptest.Server, path, token string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAuth(t *testing.T) {
	srv := httptest.NewServer(New(state.NewStore(""), "secret"))
	defer srv.Close()
	for _, tok := range []string{"", "wrong"} {
		if resp := get(t, srv, "/v1/state", tok, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q: status %d", tok, resp.StatusCode)
		}
	}
	if resp := get(t, srv, "/healthz", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
}

func TestStateETag(t *testing.T) {
	store := state.NewStore("")
	srv := httptest.NewServer(New(store, "secret"))
	defer srv.Close()

	resp := get(t, srv, "/v1/state", "secret", nil)
	var st state.State
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	tag := resp.Header.Get("ETag")
	if resp := get(t, srv, "/v1/state", "secret", map[string]string{"If-None-Match": tag}); resp.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged state: status %d, want 304", resp.StatusCode)
	}
	store.Update(func(in *state.Inputs) { in.Projects["me/a"] = state.Project{Name: "a", FullName: "me/a"} })
	if resp := get(t, srv, "/v1/state", "secret", map[string]string{"If-None-Match": tag}); resp.StatusCode != http.StatusOK {
		t.Fatalf("changed state: status %d, want 200", resp.StatusCode)
	}
}

func TestStreamSendsInitialAndUpdates(t *testing.T) {
	Heartbeat = 50 * time.Millisecond
	store := state.NewStore("")
	srv := httptest.NewServer(New(store, "secret"))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}

	lines := bufio.NewScanner(resp.Body)
	next := func(prefix string) string {
		for lines.Scan() {
			if l := lines.Text(); strings.HasPrefix(l, prefix) {
				return strings.TrimPrefix(l, prefix)
			}
		}
		t.Fatalf("stream ended waiting for %q: %v", prefix, lines.Err())
		return ""
	}

	var first state.State
	json.Unmarshal([]byte(next("data: ")), &first)
	next(": ping")

	store.Update(func(in *state.Inputs) { in.Projects["me/a"] = state.Project{Name: "a", FullName: "me/a"} })
	var second state.State
	json.Unmarshal([]byte(next("data: ")), &second)
	if second.Version != first.Version+1 || second.Focus != "me/a" {
		t.Fatalf("second = %+v, first version %d", second, first.Version)
	}
}

func put(t *testing.T, srv *httptest.Server, path, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestPutSettings(t *testing.T) {
	store := state.NewStore("")
	srv := httptest.NewServer(New(store, "secret"))
	defer srv.Close()

	good, _ := json.Marshal(func() state.Settings { s := state.DefaultSettings(); s.Kiosk = true; return s }())
	if resp := put(t, srv, "/v1/settings", "wrong", string(good)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	if resp := put(t, srv, "/v1/settings", "secret", string(good)); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("good settings: %d", resp.StatusCode)
	}
	if !store.Settings().Kiosk {
		t.Fatal("settings not stored")
	}
	for name, body := range map[string]string{
		"invalid":       strings.Replace(string(good), `"alert_hours":12`, `"alert_hours":0`, 1),
		"unknown field": strings.Replace(string(good), `{`, `{"token":"x",`, 1),
		"too big":       `{"hidden":["` + strings.Repeat("a", 20<<10) + `"]}`,
		"not json":      `kiosk=true`,
	} {
		if resp := put(t, srv, "/v1/settings", "secret", body); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", name, resp.StatusCode)
		}
	}
	if !store.Settings().Kiosk {
		t.Fatal("a rejected update changed the stored settings")
	}
}
