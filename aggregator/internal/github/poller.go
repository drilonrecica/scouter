package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
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
	failBackoff    = 20 * time.Second // first retry of a failing repo, doubling
	failBackoffMax = 30 * time.Minute
)

type repo struct {
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	PushedAt      time.Time `json:"pushed_at"`
	Archived      bool      `json:"archived"`
	Homepage      string    `json:"homepage"`
}

type tracked struct {
	project   state.Project
	nextRuns  time.Time
	nextPulls time.Time
	failures  int // consecutive failed polls; spaces out retries
}

// Poller keeps the store's projects in sync with GitHub.
type Poller struct {
	c      *Client
	store  *state.Store
	ignore map[string]bool // full names
	log    *slog.Logger
	now    func() time.Time

	nextList    time.Time
	pausedUntil time.Time // GitHub said the rate limit is spent
	repos       map[string]*tracked
	lastStep    atomic.Int64 // unix seconds of the last completed poll step
}

// LastPoll is when the poller last finished a step, for the admin status page.
func (p *Poller) LastPoll() time.Time { return time.Unix(p.lastStep.Load(), 0).UTC() }

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
// A repo that fails (renamed, access revoked) backs off on its own and does
// not hold up the others; a rate limit pauses everything until it resets.
func (p *Poller) step(ctx context.Context) {
	now := p.now()
	err := p.poll(ctx, now)
	var rl *RateLimitError
	if errors.As(err, &rl) && rl.Until.After(now) {
		p.pausedUntil = rl.Until
	}
	p.lastStep.Store(p.now().Unix())
	src := state.Source{OK: err == nil, UpdatedAt: now}
	if err != nil {
		src.Error = err.Error()
		p.log.Warn("github poll failed", "err", err)
	} else if n, first := p.failing(); n > 0 {
		// Some repos fail while GitHub itself answers: worth a line on the
		// admin page, not a "GITHUB ERROR" on the phone.
		src.Error = fmt.Sprintf("%d repo(s) failing, e.g. %s", n, first)
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

// poll returns an error only when GitHub as a whole is unusable: the repo
// list failed, a rate limit, or every tracked repo failed this step.
func (p *Poller) poll(ctx context.Context, now time.Time) error {
	if now.Before(p.pausedUntil) {
		return &RateLimitError{Until: p.pausedUntil}
	}
	if !now.Before(p.nextList) {
		if err := p.listRepos(ctx); err != nil {
			return err
		}
		p.nextList = now.Add(listEvery)
	}
	failed := 0
	var last error
	for _, t := range p.repos {
		due := !now.Before(t.nextRuns) || !now.Before(t.nextPulls)
		if !due {
			continue
		}
		err := p.pollRepo(ctx, t, now)
		var rl *RateLimitError
		if errors.As(err, &rl) {
			return err
		}
		if err != nil {
			failed++
			last = err
			t.failures++
			wait := min(failBackoff<<min(t.failures-1, 16), failBackoffMax)
			t.nextRuns, t.nextPulls = now.Add(wait), now.Add(wait)
			p.log.Info("repo poll failed", "repo", t.project.FullName, "failures", t.failures, "retry_in", wait, "err", err)
			continue
		}
		t.failures = 0
	}
	if failed > 0 && failed == len(p.repos) {
		return last // nothing works: more likely GitHub or the token than the repos
	}
	return nil
}

func (p *Poller) pollRepo(ctx context.Context, t *tracked, now time.Time) error {
	if !now.Before(t.nextRuns) {
		if err := p.pollRuns(ctx, t); err != nil {
			return err
		}
	}
	if !now.Before(t.nextPulls) {
		return p.pollPulls(ctx, t)
	}
	return nil
}

// failing counts repos whose last poll failed, naming one for the status page.
func (p *Poller) failing() (int, string) {
	var names []string
	for name, t := range p.repos {
		if t.failures > 0 {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return 0, ""
	}
	slices.Sort(names)
	return len(names), names[0]
}

func (p *Poller) listRepos(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var repos []repo
	if err := p.c.get(ctx, fmt.Sprintf("/user/repos?affiliation=owner&sort=pushed&per_page=%d", listedRepos), &repos); err != nil {
		return err
	}
	set := p.store.Settings()
	hidden := func(name string) bool { return p.ignore[name] || set.IsHidden(name) }

	// Favorites are tracked even when they are older than the listed page.
	listed := map[string]bool{}
	for _, r := range repos {
		listed[r.FullName] = true
	}
	for _, f := range set.Favorites {
		if listed[f] || hidden(f) {
			continue
		}
		var r repo
		if err := p.c.get(ctx, "/repos/"+f, &r); err != nil {
			p.log.Warn("favorite not reachable", "repo", f, "err", err)
			continue // a deleted or renamed favorite must not stop polling
		}
		repos = append(repos, r)
	}

	keep := map[string]bool{}
	var available []string
	recent := 0
	for _, r := range repos {
		if r.Archived || p.ignore[r.FullName] {
			continue
		}
		available = append(available, r.FullName)
		if set.IsHidden(r.FullName) {
			continue
		}
		if !set.IsFavorite(r.FullName) {
			if recent == trackedRepos {
				continue
			}
			recent++
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
		t.project.Homepage = r.Homepage
	}
	for name := range p.repos {
		if !keep[name] {
			delete(p.repos, name)
			p.c.Forget(name)
		}
	}
	slices.Sort(available)
	p.store.Update(func(in *state.Inputs) {
		for name := range in.Projects {
			if !keep[name] {
				delete(in.Projects, name)
			}
		}
		for name, t := range p.repos {
			in.Projects[name] = t.project
		}
		in.Available = available
	})
	return nil
}

func (p *Poller) pollRuns(ctx context.Context, t *tracked) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var resp struct {
		WorkflowRuns []workflowRun `json:"workflow_runs"`
	}
	if err := p.c.get(ctx, "/repos/"+t.project.FullName+"/actions/runs?per_page=50", &resp); err != nil {
		return err
	}
	t.project.CI, t.project.Latest = summarize(resp.WorkflowRuns, t.project.DefaultBranch)
	t.project.Power = power(resp.WorkflowRuns, t.project.DefaultBranch)
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
