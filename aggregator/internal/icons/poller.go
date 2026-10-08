package icons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/github"
	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

const (
	tick       = 20 * time.Second // one project per tick: icons are never urgent
	recheck    = 24 * time.Hour   // a site's favicon can change without a push
	reqTimeout = 20 * time.Second
)

// Poller looks up one project's icon per tick: when it is new, after each
// push or site change, and once a day.
type Poller struct {
	gh    *github.Client
	http  *http.Client
	store *state.Store
	dir   string
	log   *slog.Logger
	now   func() time.Time
	seen  map[string]checked // by project full name
}

type checked struct {
	key string // what the lookup depended on: last push and site URL
	at  time.Time
}

// NewPoller stores icons in dir as <hash>.png; the server serves them from there.
func NewPoller(gh *github.Client, hc *http.Client, store *state.Store, dir string, log *slog.Logger) *Poller {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn("icons: cannot create dir", "dir", dir, "err", err)
	}
	return &Poller{gh: gh, http: hc, store: store, dir: dir, log: log, now: time.Now, seen: map[string]checked{}}
}

// Run looks up icons until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		p.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// step looks up the first project that is due and reports whether there was one.
func (p *Poller) step(ctx context.Context) bool {
	for _, pr := range p.store.Get().Projects {
		key := pr.PushedAt.String() + " " + site(pr)
		if c, ok := p.seen[pr.FullName]; ok && c.key == key && p.now().Sub(c.at) < recheck {
			continue
		}
		p.seen[pr.FullName] = checked{key, p.now()}
		p.check(ctx, pr)
		return true
	}
	return false
}

// domainName matches repos named after their site, like "recica.dev".
var domainName = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)

// site is where a project is served: its Coolify app, else its homepage,
// else the domain its repo is named after.
func site(pr state.Project) string {
	switch name := strings.ToLower(pr.Name); {
	case pr.Deploy != nil && pr.Deploy.URL != "":
		return pr.Deploy.URL
	case pr.Homepage != "":
		return pr.Homepage
	case domainName.MatchString(name):
		return "https://" + name
	}
	return ""
}

func (p *Poller) check(ctx context.Context, pr state.Project) {
	icon, err := p.find(ctx, pr)
	if err != nil {
		// Keep whatever icon the project had: a lookup failing says nothing new.
		p.log.Debug("icon lookup failed", "repo", pr.FullName, "err", err)
		return
	}
	hash := ""
	if icon != nil {
		sum := sha256.Sum256(icon)
		hash = hex.EncodeToString(sum[:8])
		if err := p.save(hash, icon); err != nil {
			p.log.Warn("icon not stored", "repo", pr.FullName, "err", err)
			return
		}
	}
	p.store.Update(func(in *state.Inputs) { in.Icons[pr.FullName] = hash })
}

// find returns the normalized icon: a repo file first, else the site's
// favicon. Nil without error means the project has none.
func (p *Poller) find(ctx context.Context, pr state.Project) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	files, err := p.gh.Tree(ctx, pr.FullName, pr.DefaultBranch)
	if err != nil {
		return nil, err
	}
	if path := pick(files); path != "" {
		b, err := p.gh.Raw(ctx, pr.FullName, path, pr.DefaultBranch, maxFile)
		if err == nil {
			if icon, err := normalize(b); err == nil {
				return icon, nil
			}
		}
		// An unreadable candidate is not fatal: the site may still have one.
	}
	if u := site(pr); u != "" {
		return fromSite(ctx, p.http, u)
	}
	return nil, nil
}

func (p *Poller) save(hash string, icon []byte) error {
	path := filepath.Join(p.dir, hash+".png")
	if _, err := os.Stat(path); err == nil {
		return nil // same content, already stored
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, icon, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
