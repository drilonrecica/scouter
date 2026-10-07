package admin

import (
	"bytes"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

const pw = "correct horse battery"

func init() { failDelay = 0 }

func newUI(t *testing.T, password string) (*httptest.Server, *state.Store) {
	t.Helper()
	store := state.NewStore("")
	store.Update(func(in *state.Inputs) { in.Available = []string{"me/a", "me/b"} })
	h := New(Config{
		Password: password, Store: store,
		Status:  func() Status { return Status{Clients: 1, Rate: 4999, LastPoll: time.Now()} },
		Secrets: []Secret{{Name: "SCOUTER_GITHUB_TOKEN", Purpose: "GitHub API", Set: true}},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, store
}

// client follows redirects, keeps cookies, and sends Origin like a browser.
func client(t *testing.T, srv *httptest.Server) *http.Client {
	jar, _ := cookiejar.New(nil)
	c := srv.Client()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func post(t *testing.T, c *http.Client, srv *httptest.Server, path string, form url.Values, origin string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func get(t *testing.T, c *http.Client, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func login(t *testing.T, srv *httptest.Server) (*http.Client, string) {
	t.Helper()
	c := client(t, srv)
	resp := post(t, c, srv, "/admin/login", url.Values{"password": {pw}}, srv.URL)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	_, body := get(t, c, srv, "/admin/phone")
	m := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no csrf token in page")
	}
	return c, m[1]
}

func TestDisabledWithoutPassword(t *testing.T) {
	for _, p := range []string{"", "short"} {
		srv, _ := newUI(t, p)
		if resp, _ := get(t, client(t, srv), srv, "/admin/login"); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("password %q: status %d, want 404", p, resp.StatusCode)
		}
	}
}

func TestPagesNeedLoginAndSendSecurityHeaders(t *testing.T) {
	srv, _ := newUI(t, pw)
	c := client(t, srv)
	for _, p := range []string{"/admin/", "/admin/projects", "/admin/phone"} {
		resp, _ := get(t, c, srv, p)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
			t.Fatalf("%s without session: %d %s", p, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	resp, _ := get(t, c, srv, "/admin/login")
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Fatalf("CSP = %q: scripts must stay forbidden", csp)
	}
	// no-referrer would make browsers send "Origin: null" on our own form
	// posts, which the origin check rightly refuses: login would be impossible.
	if rp := resp.Header.Get("Referrer-Policy"); rp != "same-origin" {
		t.Fatalf("Referrer-Policy = %q, want same-origin", rp)
	}
	for _, h := range []string{"X-Frame-Options", "X-Content-Type-Options", "Cache-Control"} {
		if resp.Header.Get(h) == "" {
			t.Fatalf("missing %s", h)
		}
	}
}

func TestLoginCookieAndRateLimit(t *testing.T) {
	srv, _ := newUI(t, pw)
	c := client(t, srv)
	resp := post(t, c, srv, "/admin/login", url.Values{"password": {"nope"}}, srv.URL)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	for range ipAttempts - 1 {
		post(t, c, srv, "/admin/login", url.Values{"password": {"nope"}}, srv.URL)
	}
	if resp := post(t, c, srv, "/admin/login", url.Values{"password": {pw}}, srv.URL); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after %d failures even the right password must wait: %d", ipAttempts, resp.StatusCode)
	}

	srv2, _ := newUI(t, pw)
	resp = post(t, client(t, srv2), srv2, "/admin/login", url.Values{"password": {pw}}, srv2.URL)
	ck := resp.Cookies()
	if len(ck) != 1 || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode || ck[0].Path != "/admin" {
		t.Fatalf("cookie = %+v", ck)
	}
}

func TestPostsNeedCSRFAndSameOrigin(t *testing.T) {
	srv, store := newUI(t, pw)
	c, csrf := login(t, srv)
	form := url.Values{"focus_mode": {"latest"}, "rotate_minutes": {"5"}, "day_from": {"1"}, "day_to": {"7"},
		"on": {"08:00"}, "off": {"20:00"}, "alert_hours": {"6"}, "kiosk": {"on"}, "aura": {"on"}}

	if resp := post(t, c, srv, "/admin/phone", form, srv.URL); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no csrf: %d", resp.StatusCode)
	}
	form.Set("csrf", csrf)
	if resp := post(t, c, srv, "/admin/phone", form, "https://evil.example"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", resp.StatusCode)
	}
	if resp := post(t, c, srv, "/admin/phone", form, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no origin or referer: %d", resp.StatusCode)
	}
	if resp := post(t, c, srv, "/admin/phone", form, "null"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Origin: null: %d", resp.StatusCode)
	}
	if store.Settings().Kiosk {
		t.Fatal("a refused request changed settings")
	}

	v := store.Get().Version
	if resp := post(t, c, srv, "/admin/phone", form, srv.URL); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid save: %d", resp.StatusCode)
	}
	got := store.Settings()
	if !got.Kiosk || got.AlertHours != 6 || got.Schedule.Days != "1-7" || got.Background.Stars || store.Get().Version != v+1 {
		t.Fatalf("settings = %+v", got)
	}
	if _, body := get(t, c, srv, "/admin/phone"); !strings.Contains(body, "Saved.") {
		t.Fatal("no confirmation shown")
	}

	form.Set("alert_hours", "500")
	post(t, c, srv, "/admin/phone", form, srv.URL)
	if _, body := get(t, c, srv, "/admin/phone"); !strings.Contains(body, "Not saved") || store.Settings().AlertHours != 6 {
		t.Fatal("invalid input must be rejected with a message")
	}
}

func TestProjectsSave(t *testing.T) {
	srv, store := newUI(t, pw)
	c, csrf := login(t, srv)
	form := url.Values{"csrf": {csrf}, "repo": {"me/a", "me/b"}, "fav": {"me/a", "me/b"}, "order:me/a": {"2"}, "order:me/b": {"1"}}
	post(t, c, srv, "/admin/projects", form, srv.URL)
	if got := store.Settings().Favorites; len(got) != 2 || got[0] != "me/b" {
		t.Fatalf("favorites = %v, want ordered [me/b me/a]", got)
	}
	form = url.Values{"csrf": {csrf}, "repo": {"me/a", "me/b"}, "hide": {"me/a"}}
	post(t, c, srv, "/admin/projects", form, srv.URL)
	if s := store.Settings(); !s.IsHidden("me/a") || len(s.Favorites) != 0 {
		t.Fatalf("settings = %+v", s)
	}
	if _, body := get(t, c, srv, "/admin/"); strings.Contains(body, "ghp_") || !strings.Contains(body, "✓ set") {
		t.Fatal("status page must show secrets as set/unset only")
	}
}

func TestLogout(t *testing.T) {
	srv, _ := newUI(t, pw)
	c, csrf := login(t, srv)
	post(t, c, srv, "/admin/logout", url.Values{"csrf": {csrf}}, srv.URL)
	if resp, _ := get(t, c, srv, "/admin/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("after logout: %d", resp.StatusCode)
	}
}

func TestAPKUpload(t *testing.T) {
	store := state.NewStore("")
	apk := t.TempDir() + "/app/scouter.apk"
	h := New(Config{Password: pw, Store: store, APKPath: apk, Status: func() Status { return Status{} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, csrf := login(t, srv)

	upload := func(content []byte, token string) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		mw.WriteField("csrf", token)
		fw, _ := mw.CreateFormFile("apk", "scouter.apk")
		fw.Write(content)
		mw.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/app", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Origin", srv.URL)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	upload([]byte("PK\x03\x04payload"), "wrong-csrf")
	if store.Get().App != nil {
		t.Fatal("upload without a valid CSRF token was accepted")
	}
	upload([]byte("<html>not an apk"), csrf)
	if _, body := get(t, c, srv, "/admin/app"); store.Get().App != nil || !strings.Contains(body, "Not an APK") {
		t.Fatalf("non-APK accepted: app=%+v body=%.600s", store.Get().App, body)
	}
	upload([]byte("PK\x03\x04payload"), csrf)
	app := store.Get().App
	if app == nil || len(app.SHA256) != 64 || app.Size != 11 {
		t.Fatalf("app = %+v", app)
	}
	if b, _ := os.ReadFile(apk); string(b) != "PK\x03\x04payload" {
		t.Fatalf("stored = %q", b)
	}
}
