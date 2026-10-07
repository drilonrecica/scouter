package coolify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

func TestRepoName(t *testing.T) {
	for in, want := range map[string]string{
		"drilonrecica/scouter":                        "drilonrecica/scouter",
		"https://github.com/DrilonRecica/Scouter.git": "drilonrecica/scouter",
		"git@github.com:drilonrecica/recica.dev.git":  "drilonrecica/recica.dev",
		"https://github.com/me/app/":                  "me/app",
		"":                                            "",
	} {
		if got := RepoName(in); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatus(t *testing.T) {
	for in, want := range map[string]state.DeployStatus{
		"finished": state.DeploySuccess, "failed": state.DeployFailure, "in_progress": state.DeployRunning,
		"queued": state.DeployQueued, "cancelled-by-user": state.DeployCancelled,
	} {
		if got := Status(in); got != want {
			t.Errorf("Status(%q) = %q", in, got)
		}
	}
}

func TestPollerMatchesAppsAndRecordsDeploys(t *testing.T) {
	when := time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Unauthenticated."}`))
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"uuid": "stag", "name": "app-staging", "git_repository": "https://github.com/me/app.git", "git_branch": "develop", "status": "running:healthy"},
			{"uuid": "prod", "name": "app", "git_repository": "me/app", "git_branch": "master", "status": "running:unhealthy"},
			{"uuid": "db", "name": "postgres", "git_repository": "", "status": "running"},
			{"uuid": "other", "name": "untracked", "git_repository": "me/elsewhere", "git_branch": "main"},
		})
	})
	mux.HandleFunc("/api/v1/deployments/applications/prod", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"commit": "abcdef123", "status": "finished", "updated_at": when, "logs": strings.Repeat("x", 10000)}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	store := state.NewStore("")
	store.Update(func(in *state.Inputs) {
		in.Projects["me/app"] = state.Project{Name: "app", FullName: "me/app", DefaultBranch: "master", PushedAt: when}
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := NewPoller(srv.URL, "tok", srv.Client(), store, log)
	p.step(context.Background())

	st := store.Get()
	d := st.Projects[0].Deploy
	if d == nil || d.Status != state.DeploySuccess || d.Commit != "abcdef123" || d.Apps != 2 || d.Health != "running:unhealthy" || d.Branch != "master" {
		t.Fatalf("deploy = %+v: want the master app (prod) of 2", d)
	}
	if !st.Sources["coolify"].OK {
		t.Fatalf("source = %+v", st.Sources["coolify"])
	}

	bad := NewPoller(srv.URL, "wrong", srv.Client(), store, log)
	bad.step(context.Background())
	if src := store.Get().Sources["coolify"]; src.OK || !strings.Contains(src.Error, "read-only API token") {
		t.Fatalf("401 should explain itself: %+v", src)
	}
}
