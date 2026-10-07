package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

// Cadences. Active repos are polled often because that is where a build is
// about to finish; quiet repos rarely change and their 304s cost nothing.
const (
	tick           = 5 * time.Second
	listEvery      = time.Minute
	activeFor      = 2 * time.Hour
	runsActive     = 20 * time.Second
	runsQuiet      = 10 * time.Minute
	pullsActive    = 5 * time.Minute
	pullsQuiet     = 30 * time.Minute
	trackedRepos   = state.MaxProjects
	listedRepos    = 30
	sourceName     = "github"
	requestTimeout = 15 * time.Second
)

type repo struct {
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	PushedAt      time.Time `json:"pushed_at"`
	Archived      bool      `json:"archived"`
}

type tracked struct {
	project   state.Project
	nextRuns  time.Time
	nextPulls time.Time
}

// Poller keeps the store's projects in sync with GitHub.
type Poller struct {
	c      *Client
	store  *state.Store
	ignore map[string]bool // full names
	log    *slog.Logger
	now    func() time.Time

	nextList time.Time
	repos    map[string]*tracked
}

func NewPoller(c *Client, store *state.Store, ignore []string, log *slog.Logger) *Poller {
	ig := map[string]bool{}
	for _, r := range ignore {
		ig[r] = true
	}
	return &Poller{c: c, store: store, ignore: ig, log: log, now: func() time.Time { return time.Now().UTC() }, repos: map[string]*tracked{}}
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

// step does whatever is due now. Errors are reported through the store's
// source status rather than stopping the loop: GitHub hiccups are routine.
func (p *Poller) step(ctx context.Context) {
	now := p.now()
	var err error
	if !now.Before(p.nextList) {
		if err = p.listRepos(ctx); err == nil {
			p.nextList = now.Add(listEvery)
		}
	}
	for _, t := range p.repos {
		if err != nil {
			break
		}
		if !now.Before(t.nextRuns) {
			err = p.pollRuns(ctx, t)
		}
		if err == nil && !now.Before(t.nextPulls) {
			err = p.pollPulls(ctx, t)
		}
	}
	src := state.Source{OK: err == nil, UpdatedAt: now}
	if err != nil {
		src.Error = err.Error()
		p.log.Warn("github poll failed", "err", err)
	}
	p.store.Update(func(in *state.Inputs) {
		// Record only changes of health (UpdatedAt = since when), or the
		// document would change, and the phone redraw, on every tick.
		if prev, ok := in.Sources[sourceName]; ok && prev.OK == src.OK && prev.Error == src.Error {
			return
		}
		in.Sources[sourceName] = src
	})
}

func (p *Poller) listRepos(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var repos []repo
	if err := p.c.get(ctx, fmt.Sprintf("/user/repos?affiliation=owner&sort=pushed&per_page=%d", listedRepos), &repos); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, r := range repos {
		if r.Archived || p.ignore[r.FullName] || len(keep) == trackedRepos {
			continue
		}
		keep[r.FullName] = true
		t, ok := p.repos[r.FullName]
		if !ok {
			t = &tracked{}
			// Start from what the store already knows (the snapshot after a
			// restart), or the project would flash "no CI" until its runs are
			// polled, and its alert would vanish and then wake the phone again.
			t.project, _ = p.store.Project(r.FullName)
			p.repos[r.FullName] = t
		}
		if t.project.PushedAt.Before(r.PushedAt) {
			t.nextRuns = time.Time{} // new push: look at its CI right away
		}
		t.project.Name, t.project.FullName = r.Name, r.FullName
		t.project.DefaultBranch, t.project.PushedAt = r.DefaultBranch, r.PushedAt
	}
	for name := range p.repos {
		if !keep[name] {
			delete(p.repos, name)
		}
	}
	p.store.Update(func(in *state.Inputs) {
		for name := range in.Projects {
			if !keep[name] {
				delete(in.Projects, name)
			}
		}
		for name, t := range p.repos {
			in.Projects[name] = t.project
		}
	})
	return nil
}

func (p *Poller) pollRuns(ctx context.Context, t *tracked) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var resp struct {
		WorkflowRuns []workflowRun `json:"workflow_runs"`
	}
	if err := p.c.get(ctx, "/repos/"+t.project.FullName+"/actions/runs?per_page=30", &resp); err != nil {
		return err
	}
	t.project.CI, t.project.Latest = summarize(resp.WorkflowRuns, t.project.DefaultBranch)
	interval := runsQuiet
	if p.active(t) {
		interval = runsActive
	}
	t.nextRuns = p.now().Add(interval)
	p.publish(t)
	return nil
}

func (p *Poller) pollPulls(ctx context.Context, t *tracked) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var pulls []struct {
		User struct {
			Type string `json:"type"`
		} `json:"user"`
	}
	if err := p.c.get(ctx, "/repos/"+t.project.FullName+"/pulls?state=open&per_page=100", &pulls); err != nil {
		return err
	}
	t.project.OpenPRs = 0
	for _, pr := range pulls {
		if pr.User.Type != "Bot" { // dependency bumps are not work waiting on the owner
			t.project.OpenPRs++
		}
	}
	interval := pullsQuiet
	if p.active(t) {
		interval = pullsActive
	}
	t.nextPulls = p.now().Add(interval)
	p.publish(t)
	return nil
}

func (p *Poller) active(t *tracked) bool {
	if p.now().Sub(t.project.PushedAt) < activeFor {
		return true
	}
	for _, r := range []*state.Run{t.project.CI, t.project.Latest} {
		if r != nil && r.Status == state.CIRunning {
			return true
		}
	}
	return false
}

func (p *Poller) publish(t *tracked) {
	proj := t.project
	p.store.Update(func(in *state.Inputs) { in.Projects[proj.FullName] = proj })
}
