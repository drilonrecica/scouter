package github

import (
	"fmt"
	"testing"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func wr(name, branch, sha, status, conclusion string, startMin, endMin int) workflowRun {
	return workflowRun{
		Name: name, HeadBranch: branch, HeadSHA: sha, DisplayTitle: "msg " + sha, Event: "push",
		Status: status, Conclusion: conclusion, HTMLURL: "https://gh/" + name + "/" + sha,
		RunStartedAt: t0.Add(time.Duration(startMin) * time.Minute),
		UpdatedAt:    t0.Add(time.Duration(endMin) * time.Minute),
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name       string
		runs       []workflowRun
		ci         state.CIStatus
		ciSHA      string
		ciWorkflow string
		ciDuration int
		latestSHA  string // "" = no latest
	}{
		{
			name:       "all workflows green",
			runs:       []workflowRun{wr("CI", "master", "a", "completed", "success", 0, 3), wr("Lint", "master", "a", "completed", "success", 1, 2)},
			ci:         state.CISuccess,
			ciSHA:      "a",
			ciWorkflow: "CI",
			ciDuration: 180,
		},
		{
			name:       "one failure wins even while a sibling still runs",
			runs:       []workflowRun{wr("CI", "master", "a", "in_progress", "", 0, 1), wr("Lint", "master", "a", "completed", "failure", 0, 1)},
			ci:         state.CIFailure,
			ciSHA:      "a",
			ciWorkflow: "Lint",
		},
		{
			name: "running on newest commit",
			runs: []workflowRun{wr("CI", "master", "b", "queued", "", 5, 5), wr("CI", "master", "a", "completed", "failure", 0, 2)},
			ci:   state.CIRunning, ciSHA: "b", ciWorkflow: "CI",
		},
		{
			name:       "skipped and bot runs are ignored",
			runs:       []workflowRun{wr("Close PRs", "master", "c", "completed", "skipped", 9, 9), {Name: "Bot", HeadBranch: "master", HeadSHA: "d", Event: "pull_request_target", Status: "completed", Conclusion: "failure"}, wr("CI", "master", "a", "completed", "success", 0, 1)},
			ci:         state.CISuccess,
			ciSHA:      "a",
			ciWorkflow: "CI",
			ciDuration: 60,
		},
		{
			name:       "feature branch newer than master shows as latest",
			runs:       []workflowRun{wr("CI", "feat", "f", "in_progress", "", 10, 11), wr("CI", "master", "a", "completed", "success", 0, 1)},
			ci:         state.CISuccess,
			ciSHA:      "a",
			ciWorkflow: "CI",
			ciDuration: 60,
			latestSHA:  "f",
		},
		{
			name:       "feature branch older than master is dropped",
			runs:       []workflowRun{wr("CI", "master", "a", "completed", "success", 10, 11), wr("CI", "feat", "f", "completed", "failure", 0, 1)},
			ci:         state.CISuccess,
			ciSHA:      "a",
			ciWorkflow: "CI",
			ciDuration: 60,
		},
		{
			name: "dependency bot runs are ignored",
			runs: func() []workflowRun {
				bot := wr("CI", "master", "b", "completed", "failure", 9, 9)
				bot.Actor.Type = "Bot"
				return []workflowRun{wr("CI", "dependabot/npm/x", "d", "completed", "failure", 10, 10), bot, wr("CI", "master", "a", "completed", "success", 0, 1)}
			}(),
			ci:         state.CISuccess,
			ciSHA:      "a",
			ciWorkflow: "CI",
			ciDuration: 60,
		},
		{
			name:       "all cancelled",
			runs:       []workflowRun{wr("CI", "master", "a", "completed", "cancelled", 0, 1)},
			ci:         state.CICancelled,
			ciSHA:      "a",
			ciWorkflow: "CI",
			ciDuration: 60,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci, latest := summarize(tt.runs, "master")
			if ci == nil {
				t.Fatal("ci = nil")
			}
			if ci.Status != tt.ci || ci.SHA != tt.ciSHA || ci.Workflow != tt.ciWorkflow || ci.DurationS != tt.ciDuration {
				t.Fatalf("ci = %+v", ci)
			}
			switch {
			case tt.latestSHA == "" && latest != nil:
				t.Fatalf("latest = %+v, want nil", latest)
			case tt.latestSHA != "" && (latest == nil || latest.SHA != tt.latestSHA):
				t.Fatalf("latest = %+v, want sha %s", latest, tt.latestSHA)
			}
		})
	}
}

func TestSummarizeNoRuns(t *testing.T) {
	if ci, latest := summarize(nil, "master"); ci != nil || latest != nil {
		t.Fatalf("got %v %v", ci, latest)
	}
}

func TestPower(t *testing.T) {
	bot := wr("CI", "master", "z", "completed", "failure", 0, 1)
	bot.Actor.Type = "Bot"
	tests := []struct {
		name string
		runs []workflowRun
		want int // -1 = nil
	}{
		{"no runs", nil, -1},
		{"only running", []workflowRun{wr("CI", "master", "a", "in_progress", "", 0, 1)}, -1},
		{"all green", []workflowRun{wr("CI", "master", "a", "completed", "success", 3, 4), wr("CI", "master", "b", "completed", "success", 0, 1)}, 9000},
		{"one of four failed", []workflowRun{
			wr("CI", "master", "a", "completed", "failure", 9, 10), wr("CI", "master", "b", "completed", "success", 6, 7),
			wr("CI", "master", "c", "completed", "success", 3, 4), wr("CI", "master", "d", "completed", "success", 0, 1),
		}, 6750},
		{"a failing workflow fails its whole commit", []workflowRun{
			wr("CI", "master", "a", "completed", "success", 3, 4), wr("Lint", "master", "a", "completed", "failure", 3, 4),
			wr("CI", "master", "b", "completed", "success", 0, 1),
		}, 4500},
		{"cancelled, running, other branches and bots don't count", []workflowRun{
			wr("CI", "master", "r", "in_progress", "", 12, 13), wr("CI", "master", "x", "completed", "cancelled", 10, 11),
			wr("CI", "feat", "f", "completed", "failure", 8, 9), bot, wr("CI", "master", "a", "completed", "success", 0, 1),
		}, 9000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := power(tt.runs, "master")
			switch {
			case tt.want == -1 && got != nil:
				t.Fatalf("power = %d, want nil", *got)
			case tt.want != -1 && (got == nil || *got != tt.want):
				t.Fatalf("power = %v, want %d", got, tt.want)
			}
		})
	}
}

func TestPowerCoversOnlyRecentCommits(t *testing.T) {
	var runs []workflowRun
	for i := range powerCommits { // newest: all green
		runs = append(runs, wr("CI", "master", fmt.Sprint("new", i), "completed", "success", 100-i, 100-i))
	}
	runs = append(runs, wr("CI", "master", "ancient", "completed", "failure", 0, 1))
	if got := power(runs, "master"); got == nil || *got != MaxPower {
		t.Fatalf("power = %v, want %d: commits beyond the window must not count", got, MaxPower)
	}
}
