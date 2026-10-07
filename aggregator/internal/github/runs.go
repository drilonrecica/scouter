package github

import (
	"math"
	"strings"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

// workflowRun is the subset of GitHub's workflow run object Scouter reads.
type workflowRun struct {
	Name         string    `json:"name"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	DisplayTitle string    `json:"display_title"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`     // queued, in_progress, completed, ...
	Conclusion   string    `json:"conclusion"` // success, failure, cancelled, skipped, ...
	CreatedAt    time.Time `json:"created_at"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HTMLURL      string    `json:"html_url"`
	Actor        struct {
		Type string `json:"type"`
	} `json:"triggering_actor"`
}

// ignoredEvents are runs that say nothing about the health of a commit:
// bots and housekeeping workflows triggered by someone else's PR.
var ignoredEvents = map[string]bool{"pull_request_target": true, "dynamic": true, "issues": true, "issue_comment": true}

// isBot reports runs started by dependency bots: they arrive in floods, fail
// routinely, and would bury the owner's own commits.
func isBot(r workflowRun) bool {
	return r.Actor.Type == "Bot" || strings.HasPrefix(r.HeadBranch, "dependabot/") || strings.HasPrefix(r.HeadBranch, "renovate/")
}

// group drops runs that say nothing about a commit's health and groups the
// rest by commit. GitHub returns runs newest first; order keeps that.
func group(runs []workflowRun) (order []string, groups map[string][]workflowRun) {
	groups = map[string][]workflowRun{}
	for _, r := range runs {
		if ignoredEvents[r.Event] || isBot(r) || r.Conclusion == "skipped" || r.Conclusion == "neutral" {
			continue
		}
		if _, seen := groups[r.HeadSHA]; !seen {
			order = append(order, r.HeadSHA)
		}
		groups[r.HeadSHA] = append(groups[r.HeadSHA], r)
	}
	return order, groups
}

// summarize combines runs (newest first, as GitHub returns them) into the
// latest result for the default branch and the latest result on any other
// branch, when that one is newer.
func summarize(runs []workflowRun, defaultBranch string) (ci, latest *state.Run) {
	order, groups := group(runs)
	for _, sha := range order {
		run := combine(groups[sha])
		if run.Branch == defaultBranch {
			if ci == nil {
				ci = run
			}
		} else if latest == nil && ci == nil {
			latest = run // only interesting while newer than the default branch
		}
		if ci != nil {
			break
		}
	}
	return ci, latest
}

// powerCommits is how many recent default-branch commits the power level covers.
const powerCommits = 20

// MaxPower is a flawless build record. Yes, it is 9000.
const MaxPower = 9000

// power is the build health of the default branch: the share of its recent
// finished commits that passed, scaled to 0..MaxPower. Running and cancelled
// commits say nothing yet, so they don't count. Nil when there is no record.
func power(runs []workflowRun, defaultBranch string) *int {
	order, groups := group(runs)
	var passed, total int
	for _, sha := range order {
		if total == powerCommits {
			break
		}
		run := combine(groups[sha])
		if run.Branch != defaultBranch || (run.Status != state.CISuccess && run.Status != state.CIFailure) {
			continue
		}
		total++
		if run.Status == state.CISuccess {
			passed++
		}
	}
	if total == 0 {
		return nil
	}
	p := int(math.Round(float64(MaxPower*passed) / float64(total)))
	return &p
}

func combine(rs []workflowRun) *state.Run {
	first := rs[0]
	run := &state.Run{Branch: first.HeadBranch, SHA: first.HeadSHA, Title: first.DisplayTitle, Workflow: first.Name, URL: first.HTMLURL}
	var running, failed, succeeded bool
	var end time.Time
	for _, r := range rs {
		start := r.RunStartedAt
		if start.IsZero() {
			start = r.CreatedAt
		}
		if run.StartedAt.IsZero() || start.Before(run.StartedAt) {
			run.StartedAt = start
		}
		if r.UpdatedAt.After(end) {
			end = r.UpdatedAt
		}
		switch {
		case r.Status != "completed":
			running = true
		case r.Conclusion == "success":
			succeeded = true
		case r.Conclusion == "failure" || r.Conclusion == "timed_out" || r.Conclusion == "startup_failure" || r.Conclusion == "action_required":
			if !failed {
				run.Workflow, run.URL = r.Name, r.HTMLURL
			}
			failed = true
		}
	}
	// A failure is final even while sibling workflows still run: show it now.
	switch {
	case failed:
		run.Status = state.CIFailure
	case running:
		run.Status = state.CIRunning
	case succeeded:
		run.Status = state.CISuccess
	default:
		run.Status = state.CICancelled
	}
	if !running {
		run.DurationS = int(end.Sub(run.StartedAt).Seconds())
	}
	return run
}
