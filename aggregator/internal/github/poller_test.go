package github

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

// fakeGitHub serves canned JSON per path with ETags, and counts full (200) responses.
type fakeGitHub struct {
	mu     sync.Mutex
	bodies map[string]any
	full   map[string]int
	fail   bool
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return
	}
	if f.fail {
		http.Error(w, "boom", http.StatusBadGateway)
		return
	}
	body, ok := f.bodies[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, _ := json.Marshal(body)
	etag := `"` + string(rune('a'+len(b)%26)) + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	f.full[r.URL.Path]++
	w.Header().Set("ETag", etag)
	w.Write(b)
}

func setup(t *testing.T) (*fakeGitHub, *Poller, *state.Store) {
	t.Helper()
	gh := &fakeGitHub{full: map[string]int{}, bodies: map[string]any{
		"/user/repos": []map[string]any{
			{"name": "app", "full_name": "me/app", "default_branch": "master", "pushed_at": t0},
			{"name": "dusty", "full_name": "me/dusty", "default_branch": "main", "pushed_at": t0.Add(-1000 * 60 * 60 * 1e9), "archived": true},
			{"name": "noise", "full_name": "me/noise", "default_branch": "main", "pushed_at": t0},
		},
		"/repos/me/app/actions/runs": map[string]any{"workflow_runs": []workflowRun{
			wr("CI", "master", "abc", "completed", "failure", -5, -1),
		}},
		"/repos/me/app/pulls": []map[string]any{{}, {}},
	}}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	store := state.NewStore("")
	p := NewPoller(NewClient(srv.URL, "tok", srv.Client()), store, []string{"me/noise"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return gh, p, store
}

func TestPollerBuildsProjects(t *testing.T) {
	_, p, store := setup(t)
	p.step(context.Background())

	st := store.Get()
	if len(st.Projects) != 1 {
		t.Fatalf("projects = %+v, want only me/app (archived and ignored repos skipped)", st.Projects)
	}
	app := st.Projects[0]
	if app.CI == nil || app.CI.Status != state.CIFailure || app.CI.SHA != "abc" || app.OpenPRs != 2 {
		t.Fatalf("app = %+v ci=%+v", app, app.CI)
	}
	if len(st.Alerts) != 1 {
		t.Fatalf("alerts = %+v", st.Alerts)
	}
	if src := st.Sources["github"]; !src.OK {
		t.Fatalf("source = %+v", src)
	}
}

func TestPollerReusesETags(t *testing.T) {
	gh, p, store := setup(t)
	p.step(context.Background())
	v := store.Get().Version

	// Force everything due again: unchanged upstream must answer 304 and
	// leave the published document untouched.
	p.nextList = p.now().Add(-1)
	for _, tr := range p.repos {
		tr.nextRuns, tr.nextPulls = p.now().Add(-1), p.now().Add(-1)
	}
	p.step(context.Background())

	if n := gh.full["/repos/me/app/actions/runs"]; n != 1 {
		t.Fatalf("runs fetched in full %d times, want 1 (second must be a 304)", n)
	}
	if store.Get().Version != v {
		t.Fatalf("version changed on a no-op poll")
	}
}

func TestPollerReportsErrorsAsSourceStatus(t *testing.T) {
	gh, p, store := setup(t)
	gh.fail = true
	p.step(context.Background())
	if src := store.Get().Sources["github"]; src.OK || src.Error == "" {
		t.Fatalf("source = %+v, want error", src)
	}
}

func TestPollerKeepsRestoredCIUntilRepolled(t *testing.T) {
	gh, p, store := setup(t)
	restored := state.Project{Name: "app", FullName: "me/app", DefaultBranch: "master", PushedAt: t0,
		CI: &state.Run{Status: state.CIFailure, SHA: "abc"}, OpenPRs: 2}
	store.Update(func(in *state.Inputs) { in.Projects[restored.FullName] = restored })

	// Runs are unreachable: only the repo list succeeds.
	delete(gh.bodies, "/repos/me/app/actions/runs")
	if err := p.listRepos(context.Background()); err != nil {
		t.Fatal(err)
	}
	app := store.Get().Projects[0]
	if app.CI == nil || app.CI.Status != state.CIFailure || app.OpenPRs != 2 {
		t.Fatalf("app = %+v: the repo list must not wipe restored CI state", app)
	}
}
