package icons

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/github"
	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

func TestPollerFindsRepoIconThenSiteFaviconAndKeepsLastGood(t *testing.T) {
	logo := encodePNG(t, square(40, 40, 0))
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			io.WriteString(w, `<html><head><link rel="icon" sizes="64x64" href="/i.png"></head></html>`)
		case "/i.png":
			blue := square(64, 64, 0)
			for i := range blue.Pix {
				if i%4 == 2 { // blue channel: a different icon from the repo's red one
					blue.Pix[i] = 220
				}
			}
			w.Write(encodePNG(t, blue))
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()

	githubDown := false
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if githubDown {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/repos/me/app/git/trees/main":
			io.WriteString(w, `{"tree":[{"path":"logo.png","type":"blob","size":900},{"path":"src","type":"tree"}]}`)
		case "/repos/me/app/contents/logo.png":
			w.Write(logo)
		case "/repos/me/web/git/trees/main", "/repos/me/bare/git/trees/main":
			io.WriteString(w, `{"tree":[{"path":"main.go","type":"blob","size":100}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()

	store := state.NewStore("")
	now := time.Now().UTC()
	store.Update(func(in *state.Inputs) {
		in.Projects["me/app"] = state.Project{Name: "app", FullName: "me/app", DefaultBranch: "main", PushedAt: now}
		in.Projects["me/web"] = state.Project{Name: "web", FullName: "me/web", DefaultBranch: "main", PushedAt: now.Add(-time.Hour), Homepage: site.URL}
		in.Projects["me/bare"] = state.Project{Name: "bare", FullName: "me/bare", DefaultBranch: "main", PushedAt: now.Add(-2 * time.Hour)}
	})
	dir := t.TempDir()
	p := NewPoller(github.NewClient(gh.URL, "t", gh.Client()), site.Client(), store, dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	for p.step(ctx) {
	}

	icons := map[string]string{}
	for _, pr := range store.Get().Projects {
		icons[pr.Name] = pr.Icon
	}
	if icons["app"] == "" || icons["web"] == "" || icons["app"] == icons["web"] {
		t.Fatalf("icons = %v, want distinct icons for app (repo) and web (site)", icons)
	}
	if icons["bare"] != "" {
		t.Errorf("bare has no icon anywhere, got %q", icons["bare"])
	}
	for _, h := range []string{icons["app"], icons["web"]} {
		if _, err := os.Stat(filepath.Join(dir, h+".png")); err != nil {
			t.Errorf("icon %s not stored: %v", h, err)
		}
	}

	// Nothing due: one pass checks each project once.
	if p.step(ctx) {
		t.Error("nothing should be due right after a full pass")
	}

	// A push makes app due again; GitHub failing then must not drop its icon.
	githubDown = true
	store.Update(func(in *state.Inputs) {
		pr := in.Projects["me/app"]
		pr.PushedAt = now.Add(time.Minute)
		in.Projects["me/app"] = pr
	})
	if !p.step(ctx) {
		t.Fatal("a push should make the project due")
	}
	for _, pr := range store.Get().Projects {
		if pr.Name == "app" && pr.Icon != icons["app"] {
			t.Errorf("icon after a failed lookup = %q, want the last good %q", pr.Icon, icons["app"])
		}
	}
}

func TestSiteFallsBackFromCoolifyToHomepageToDomainName(t *testing.T) {
	for _, c := range []struct {
		p    state.Project
		want string
	}{
		{state.Project{Name: "app", Deploy: &state.Deploy{URL: "https://app.example.com"}, Homepage: "https://home"}, "https://app.example.com"},
		{state.Project{Name: "app", Homepage: "https://home"}, "https://home"},
		{state.Project{Name: "PaLeter.com"}, "https://paleter.com"},
		{state.Project{Name: "luaj.club"}, "https://luaj.club"},
		{state.Project{Name: "scouter"}, ""},
		{state.Project{Name: "v1.2"}, ""},      // a version, not a domain
		{state.Project{Name: "my.app.42"}, ""}, // a TLD has letters only
	} {
		if got := site(c.p); got != c.want {
			t.Errorf("site(%q) = %q, want %q", c.p.Name, got, c.want)
		}
	}
}
