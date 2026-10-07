// Package coolify polls a Coolify instance for each repo's latest deployment.
package coolify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

const (
	tick          = 5 * time.Second
	listEvery     = time.Minute
	busyEvery     = 30 * time.Second // deploying, or a recent push
	quietEvery    = 5 * time.Minute
	recentPush    = 10 * time.Minute
	sourceName    = "coolify"
	reqTimeout    = 15 * time.Second
	maxBodyBytes  = 8 << 20 // deployment records carry build logs
	deploymentsQS = "?skip=0&take=1"
)

type app struct {
	UUID          string `json:"uuid"`
	Name          string `json:"name"`
	GitRepository string `json:"git_repository"`
	GitBranch     string `json:"git_branch"`
	Status        string `json:"status"`
}

type deployment struct {
	Commit    string    `json:"commit"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Poller writes the latest deployment per repo into the store.
type Poller struct {
	base, token string
	http        *http.Client
	store       *state.Store
	log         *slog.Logger
	now         func() time.Time

	nextList time.Time
	apps     map[string]*tracked // by project full name
}

type tracked struct {
	app     app
	count   int
	next    time.Time
	deploy  state.Deploy
	hasData bool
}

func NewPoller(base, token string, hc *http.Client, store *state.Store, log *slog.Logger) *Poller {
	return &Poller{base: strings.TrimRight(base, "/"), token: token, http: hc, store: store, log: log,
		now: func() time.Time { return time.Now().UTC() }, apps: map[string]*tracked{}}
}

// Run polls until ctx is cancelled.
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

func (p *Poller) step(ctx context.Context) {
	now := p.now()
	var err error
	if !now.Before(p.nextList) {
		if err = p.listApps(ctx); err == nil {
			p.nextList = now.Add(listEvery)
		}
	}
	for name, t := range p.apps {
		if err != nil {
			break
		}
		if now.Before(t.next) {
			continue
		}
		err = p.pollDeployment(ctx, name, t)
	}
	src := state.Source{OK: err == nil, UpdatedAt: now}
	if err != nil {
		src.Error = err.Error()
		p.log.Warn("coolify poll failed", "err", err)
	}
	p.store.Update(func(in *state.Inputs) {
		if prev, ok := in.Sources[sourceName]; ok && prev.OK == src.OK && prev.Error == src.Error {
			return
		}
		in.Sources[sourceName] = src
	})
}

var repoPath = regexp.MustCompile(`(?i)([A-Za-z0-9-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// RepoName turns any form Coolify stores (owner/repo, https URL, ssh URL)
// into lower-case owner/repo, the key for matching GitHub projects.
func RepoName(gitRepository string) string {
	m := repoPath.FindStringSubmatch(strings.TrimSpace(gitRepository))
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1] + "/" + m[2])
}

func (p *Poller) listApps(ctx context.Context) error {
	var apps []app
	if err := p.get(ctx, "/api/v1/applications", &apps); err != nil {
		return err
	}
	// Match apps to the projects GitHub is tracking.
	projects := map[string]state.Project{}
	for _, pr := range p.store.Get().Projects {
		projects[strings.ToLower(pr.FullName)] = pr
	}
	byRepo := map[string][]app{}
	for _, a := range apps {
		if key := RepoName(a.GitRepository); key != "" {
			byRepo[key] = append(byRepo[key], a)
		}
	}
	seen := map[string]bool{}
	for key, list := range byRepo {
		pr, ok := projects[key]
		if !ok {
			continue
		}
		chosen := list[0]
		for _, a := range list { // the app deploying the default branch is the one that matters
			if a.GitBranch == pr.DefaultBranch {
				chosen = a
				break
			}
		}
		seen[pr.FullName] = true
		t, ok := p.apps[pr.FullName]
		if !ok {
			t = &tracked{}
			p.apps[pr.FullName] = t
		}
		if t.app.UUID != chosen.UUID {
			t.next = time.Time{}
		}
		t.app, t.count = chosen, len(list)
		if p.now().Sub(pr.PushedAt) < recentPush {
			t.next = time.Time{} // a push usually starts a deploy: look now
		}
	}
	for name := range p.apps {
		if !seen[name] {
			delete(p.apps, name)
		}
	}
	p.store.Update(func(in *state.Inputs) {
		for name := range in.Deploys {
			if !seen[name] {
				delete(in.Deploys, name)
			}
		}
	})
	return nil
}

// Status maps Coolify's deployment queue states to Scouter's.
func Status(s string) state.DeployStatus {
	switch {
	case s == "finished":
		return state.DeploySuccess
	case s == "failed":
		return state.DeployFailure
	case s == "in_progress":
		return state.DeployRunning
	case s == "queued":
		return state.DeployQueued
	case strings.HasPrefix(s, "cancelled"):
		return state.DeployCancelled
	}
	return state.DeployStatus(s)
}

func (p *Poller) pollDeployment(ctx context.Context, name string, t *tracked) error {
	var resp json.RawMessage
	if err := p.get(ctx, "/api/v1/deployments/applications/"+t.app.UUID+deploymentsQS, &resp); err != nil {
		return err
	}
	// Coolify has returned both a bare list and {"deployments": [...]} over versions.
	var list []deployment
	if err := json.Unmarshal(resp, &list); err != nil {
		var wrapped struct {
			Deployments []deployment `json:"deployments"`
		}
		if err := json.Unmarshal(resp, &wrapped); err != nil {
			return fmt.Errorf("deployments of %s: unexpected response", t.app.Name)
		}
		list = wrapped.Deployments
	}
	interval := quietEvery
	if len(list) > 0 {
		d := list[0]
		t.deploy = state.Deploy{Status: Status(d.Status), Commit: d.Commit, Branch: t.app.GitBranch, At: d.UpdatedAt.UTC(), Health: t.app.Status, Apps: t.count}
		t.hasData = true
		if t.deploy.Status == state.DeployRunning || t.deploy.Status == state.DeployQueued {
			interval = busyEvery
		}
		dep := t.deploy
		p.store.Update(func(in *state.Inputs) { in.Deploys[name] = dep })
	}
	t.next = p.now().Add(interval)
	return nil
}

func (p *Poller) get(ctx context.Context, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		hint := ""
		if resp.StatusCode == http.StatusUnauthorized {
			hint = " (create a new read-only API token in Coolify)"
		}
		return fmt.Errorf("GET %s: %s%s: %s", path, resp.Status, hint, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(v)
}
