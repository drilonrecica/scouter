// Package admin serves the password-protected settings UI under /admin/.
//
// Security model: one password from the environment; server-side sessions
// (random IDs, HttpOnly + SameSite=Strict cookie); a per-session CSRF token
// plus an Origin/Referer check on every POST; rate-limited logins; a CSP
// that forbids scripts entirely (the UI has none). Secrets are never shown.
package admin

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

//go:embed web
var web embed.FS

// MinPassword is the shortest password the UI accepts at all.
const MinPassword = 12

const (
	cookieName     = "scouter_admin"
	sessionTTL     = 12 * time.Hour
	attemptWindow  = 10 * time.Minute
	globalAttempts = 10 // failed logins per window, from anywhere
	ipAttempts     = 5  // failed logins per window, per client
)

// failDelay slows down every failed login; a variable so tests run fast.
var failDelay = time.Second

// Status is what the status page shows; main fills it from the running parts.
type Status struct {
	Clients  int64
	Rate     int64
	LastPoll time.Time
}

// Secret describes an environment variable without revealing its value.
type Secret struct {
	Name, Purpose string
	Set           bool
}

// Config wires the UI to the rest of the aggregator.
type Config struct {
	Password string
	Store    *state.Store
	Status   func() Status
	Secrets  []Secret
	Log      *slog.Logger
	// APKPath is where an uploaded APK is stored for over-the-air updates;
	// empty disables the App page.
	APKPath string
}

const maxAPK = state.MaxAPK

type session struct {
	csrf    string
	expires time.Time
	flash   *flash
}

type flash struct {
	OK   bool
	Text string
}

type ui struct {
	cfg      Config
	digest   [32]byte
	pages    map[string]*template.Template
	login    *template.Template
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]*session
	failures []attempt
}

type attempt struct {
	at time.Time
	ip string
}

// New returns the /admin/ handler, or a plain 404 handler when no usable
// password is configured: the UI is off unless deliberately enabled.
func New(cfg Config) http.Handler {
	if len(cfg.Password) < MinPassword {
		if cfg.Password != "" {
			cfg.Log.Warn("SCOUTER_ADMIN_PASSWORD too short; admin UI disabled", "min", MinPassword)
		}
		return http.NotFoundHandler()
	}
	u := &ui{cfg: cfg, digest: sha256.Sum256([]byte(cfg.Password)), now: time.Now, sessions: map[string]*session{}, pages: map[string]*template.Template{}}
	u.login = template.Must(template.ParseFS(web, "web/login.html"))
	for _, p := range []string{"status", "projects", "phone", "app"} {
		u.pages[p] = template.Must(template.ParseFS(web, "web/layout.html", "web/"+p+".html"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/static/admin.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		b, _ := web.ReadFile("web/admin.css")
		w.Write(b)
	})
	mux.HandleFunc("GET /admin/static/mark.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		b, _ := web.ReadFile("web/mark.svg")
		w.Write(b)
	})
	mux.HandleFunc("GET /admin/login", u.loginPage)
	mux.HandleFunc("POST /admin/login", u.doLogin)
	mux.Handle("POST /admin/logout", u.authed(u.doLogout))
	mux.Handle("GET /admin/{$}", u.authed(u.statusPage))
	mux.Handle("GET /admin/projects", u.authed(u.projectsPage))
	mux.Handle("POST /admin/projects", u.authed(u.saveProjects))
	mux.Handle("GET /admin/phone", u.authed(u.phonePage))
	mux.Handle("POST /admin/phone", u.authed(u.savePhone))
	if cfg.APKPath != "" {
		mux.Handle("GET /admin/app", u.authed(u.appPage))
		mux.Handle("POST /admin/app", u.authed(u.uploadAPK))
	}
	mux.Handle("GET /admin", http.RedirectHandler("/admin/", http.StatusSeeOther))
	return secure(mux)
}

// secure adds headers that make the pages inert outside their own origin.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// same-origin, not no-referrer: with no-referrer browsers send
		// "Origin: null" on form posts, and the CSRF origin check could
		// not tell our own forms from anyone else's.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// ---- sessions ------------------------------------------------------------------

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b) // never fails on supported platforms
	return base64.RawURLEncoding.EncodeToString(b)
}

func (u *ui) current(r *http.Request) (string, *session) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	s, ok := u.sessions[c.Value]
	if !ok {
		return "", nil
	}
	if u.now().After(s.expires) {
		delete(u.sessions, c.Value)
		return "", nil
	}
	return c.Value, s
}

type authedHandler func(w http.ResponseWriter, r *http.Request, id string, s *session)

// authed requires a session, and for POSTs a same-origin request carrying
// the session's CSRF token.
func (u *ui) authed(h authedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, s := u.current(r)
		if s == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxAPK+1<<20)
			if !sameOrigin(r) {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
			// ParseForm ignores multipart bodies, which would leave the CSRF
			// field of the APK upload empty: parse those as multipart.
			parse := r.ParseForm
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				parse = func() error { return r.ParseMultipartForm(4 << 20) } // larger parts spill to disk
			}
			if err := parse(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(s.csrf)) != 1 {
				http.Error(w, "missing or stale form token, reload the page", http.StatusForbidden)
				return
			}
		}
		h(w, r, id, s)
	})
}

// sameOrigin accepts a POST only when Origin (or, failing that, Referer)
// names this host. Browsers always send one of them on form posts.
func sameOrigin(r *http.Request) bool {
	src := r.Header.Get("Origin")
	if src == "" || src == "null" {
		src = r.Header.Get("Referer")
	}
	o, err := url.Parse(src)
	return err == nil && src != "" && o.Host == r.Host
}

func isLocal(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// ---- login -----------------------------------------------------------------------

// clientIP is the first X-Forwarded-For hop (set by Coolify's proxy), else the peer.
// It is spoofable, which is why a global limit backs it up.
func clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(strings.Split(f, ",")[0])
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

// limited reports whether another login attempt from ip must be refused.
func (u *ui) limited(ip string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	cut := u.now().Add(-attemptWindow)
	u.failures = slices.DeleteFunc(u.failures, func(a attempt) bool { return a.at.Before(cut) })
	fromIP := 0
	for _, a := range u.failures {
		if a.ip == ip {
			fromIP++
		}
	}
	return len(u.failures) >= globalAttempts || fromIP >= ipAttempts
}

func (u *ui) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, s := u.current(r); s != nil {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	u.login.Execute(w, map[string]any{"Favicon": favicon(ledIdle)})
}

func (u *ui) doLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if u.limited(ip) {
		w.Header().Set("Retry-After", strconv.Itoa(int(attemptWindow.Seconds())))
		w.WriteHeader(http.StatusTooManyRequests)
		u.login.Execute(w, map[string]any{"Favicon": favicon(ledIdle), "Error": "Too many attempts. Try again in a few minutes."})
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	got := sha256.Sum256([]byte(r.PostFormValue("password")))
	if subtle.ConstantTimeCompare(got[:], u.digest[:]) != 1 {
		u.mu.Lock()
		u.failures = append(u.failures, attempt{at: u.now(), ip: ip})
		u.mu.Unlock()
		u.cfg.Log.Warn("admin login failed", "ip", ip)
		time.Sleep(failDelay)
		w.WriteHeader(http.StatusUnauthorized)
		u.login.Execute(w, map[string]any{"Favicon": favicon(ledIdle), "Error": "Wrong password."})
		return
	}
	id := randomToken()
	u.mu.Lock()
	for k, old := range u.sessions { // logins are rare: a good moment to drop expired sessions
		if u.now().After(old.expires) {
			delete(u.sessions, k)
		}
	}
	u.sessions[id] = &session{csrf: randomToken(), expires: u.now().Add(sessionTTL)}
	u.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/admin", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: !isLocal(r.Host),
	})
	u.cfg.Log.Info("admin login", "ip", ip)
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (u *ui) doLogout(w http.ResponseWriter, r *http.Request, id string, _ *session) {
	u.mu.Lock()
	delete(u.sessions, id)
	u.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: !isLocal(r.Host)})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ---- pages -----------------------------------------------------------------------

type page struct {
	Title, Page, CSRF string
	Flash             *flash
	Data              any
	Favicon           template.URL
}

func (u *ui) render(w http.ResponseWriter, s *session, name, title string, data any) {
	u.mu.Lock()
	f := s.flash
	s.flash = nil
	u.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	icon := favicon(led(u.cfg.Store.Get(), u.cfg.Store.Heartbeat(), u.now()))
	if err := u.pages[name].ExecuteTemplate(w, name+".html", page{Title: title, Page: name, CSRF: s.csrf, Flash: f, Data: data, Favicon: icon}); err != nil {
		u.cfg.Log.Error("render", "page", name, "err", err)
	}
}

func (u *ui) setFlash(s *session, ok bool, text string) {
	u.mu.Lock()
	s.flash = &flash{OK: ok, Text: text}
	u.mu.Unlock()
}

type sourceRow struct {
	Name, Error, Since string
	OK                 bool
}

func (u *ui) statusPage(w http.ResponseWriter, _ *http.Request, _ string, s *session) {
	st := u.cfg.Store.Get()
	status := u.cfg.Status()
	b, _ := json.Marshal(st)
	var sources []sourceRow
	for name, src := range st.Sources {
		sources = append(sources, sourceRow{Name: name, OK: src.OK, Error: src.Error, Since: src.UpdatedAt.Format("2006-01-02 15:04 UTC")})
	}
	slices.SortFunc(sources, func(a, b sourceRow) int { return cmp.Compare(a.Name, b.Name) })
	last := "—"
	if !status.LastPoll.IsZero() && status.LastPoll.Unix() > 0 {
		last = fmt.Sprintf("%ds ago", int(u.now().Sub(status.LastPoll).Seconds()))
	}
	data := map[string]any{
		"Version": st.Version, "Bytes": len(b), "Clients": status.Clients, "Rate": status.Rate, "LastPoll": last,
		"Projects": len(st.Projects), "Alerts": len(st.Alerts), "Sources": sources, "Secrets": u.cfg.Secrets,
	}
	if h := u.cfg.Store.Heartbeat(); h != nil {
		ago := u.now().Sub(h.At)
		data["Phone"] = h
		data["PhoneSeen"] = humanize(ago) + " ago"
		data["PhoneSilent"] = ago > phoneSilentAfter
		data["PhoneHot"] = h.TempC > 45
		data["PhoneMB"] = float64(h.PssKB) / 1024
		data["PhoneUptime"] = humanize(time.Duration(h.UptimeS) * time.Second)
	}
	u.render(w, s, "status", "status", data)
}

func humanize(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (u *ui) appPage(w http.ResponseWriter, _ *http.Request, _ string, s *session) {
	data := map[string]any{"Phone": u.cfg.Store.Heartbeat()}
	if a := u.cfg.Store.Get().App; a != nil {
		data["App"] = map[string]any{"Short": a.SHA256[:12], "SizeKB": a.Size / 1024, "Uploaded": a.UploadedAt.Format("2006-01-02 15:04 UTC")}
	}
	u.render(w, s, "app", "app", data)
}

// uploadAPK stores an APK for the phone (see state.SaveAPK for the checks).
func (u *ui) uploadAPK(w http.ResponseWriter, r *http.Request, _ string, s *session) {
	f, _, err := r.FormFile("apk")
	if err != nil {
		u.setFlash(s, false, "No file received.")
		http.Redirect(w, r, "/admin/app", http.StatusSeeOther)
		return
	}
	defer f.Close()
	rel, err := state.SaveAPK(u.cfg.APKPath, f)
	if err != nil {
		msg := "Could not store the APK: " + err.Error()
		if errors.Is(err, state.ErrNotAPK) {
			msg = "Not an APK (expected a ZIP file under 30 MB)."
		}
		u.setFlash(s, false, msg)
		http.Redirect(w, r, "/admin/app", http.StatusSeeOther)
		return
	}
	u.cfg.Store.SetApp(rel)
	u.cfg.Log.Info("apk uploaded", "by", "admin", "sha256", rel.SHA256, "bytes", rel.Size, "ip", clientIP(r))
	u.setFlash(s, true, "Uploaded. The phone installs it within a minute.")
	http.Redirect(w, r, "/admin/app", http.StatusSeeOther)
}

type projectRow struct {
	Name             string
	Favorite, Hidden bool
	Order            int
}

func (u *ui) projectsPage(w http.ResponseWriter, _ *http.Request, _ string, s *session) {
	set := u.cfg.Store.Settings()
	names := append(slices.Clone(u.cfg.Store.Get().Available), set.Hidden...)
	names = append(names, set.Favorites...)
	slices.Sort(names)
	names = slices.Compact(names)
	rows := make([]projectRow, 0, len(names))
	for _, n := range names {
		rows = append(rows, projectRow{Name: n, Favorite: set.IsFavorite(n), Hidden: set.IsHidden(n), Order: slices.Index(set.Favorites, n) + 1})
	}
	// Favorites first in their order, then the rest alphabetically.
	slices.SortStableFunc(rows, func(a, b projectRow) int {
		switch {
		case a.Favorite && b.Favorite:
			return cmp.Compare(a.Order, b.Order)
		case a.Favorite:
			return -1
		case b.Favorite:
			return 1
		}
		return 0
	})
	u.render(w, s, "projects", "projects", map[string]any{"Rows": rows})
}

func (u *ui) saveProjects(w http.ResponseWriter, r *http.Request, _ string, s *session) {
	set := u.cfg.Store.Settings()
	type fav struct {
		name  string
		order int
	}
	var favs []fav
	for _, n := range r.PostForm["fav"] {
		o, err := strconv.Atoi(r.PostFormValue("order:" + n))
		if err != nil || o < 1 {
			o = 99
		}
		favs = append(favs, fav{n, o})
	}
	slices.SortStableFunc(favs, func(a, b fav) int { return cmp.Or(cmp.Compare(a.order, b.order), cmp.Compare(a.name, b.name)) })
	set.Favorites = make([]string, 0, len(favs))
	for _, f := range favs {
		set.Favorites = append(set.Favorites, f.name)
	}
	set.Hidden = append([]string{}, r.PostForm["hide"]...)
	if set.FocusMode == state.FocusPinned && set.IsHidden(set.Pinned) {
		set.FocusMode, set.Pinned = state.FocusLatest, ""
	}
	u.save(w, r, s, set, "/admin/projects")
}

func (u *ui) phonePage(w http.ResponseWriter, _ *http.Request, _ string, s *session) {
	set := u.cfg.Store.Settings()
	from, to, _ := strings.Cut(set.Schedule.Days, "-")
	repos := append(slices.Clone(u.cfg.Store.Get().Available), set.Favorites...)
	slices.Sort(repos)
	u.render(w, s, "phone", "phone", map[string]any{"S": set, "DayFrom": from, "DayTo": to, "Repos": slices.Compact(repos)})
}

func (u *ui) savePhone(w http.ResponseWriter, r *http.Request, _ string, s *session) {
	set := u.cfg.Store.Settings()
	f := r.PostFormValue
	num := func(k string) int { n, _ := strconv.Atoi(f(k)); return n }
	set.FocusMode = f("focus_mode")
	set.Pinned = f("pinned")
	set.RotateMinutes = num("rotate_minutes")
	set.Schedule = state.Schedule{Days: f("day_from") + "-" + f("day_to"), On: f("on"), Off: f("off")}
	set.Kiosk = f("kiosk") == "on"
	set.AlertHours = num("alert_hours")
	set.Background = state.Background{Aura: f("aura") == "on", Stars: f("stars") == "on", Mesh: f("mesh") == "on",
		Stage: f("stage") == "on", Weather: f("weather") == "on"}
	set.Wave = f("wave") == "on"
	set.Briefing = f("briefing") == "on"
	u.save(w, r, s, set, "/admin/phone")
}

func (u *ui) save(w http.ResponseWriter, r *http.Request, s *session, set state.Settings, back string) {
	if err := u.cfg.Store.SetSettings(set); err != nil {
		u.setFlash(s, false, "Not saved: "+err.Error())
	} else {
		u.cfg.Log.Info("settings changed", "by", "admin", "ip", clientIP(r))
		u.setFlash(s, true, "Saved. The phone picks it up within seconds.")
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
