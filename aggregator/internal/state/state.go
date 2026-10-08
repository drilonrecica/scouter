// Package state holds the single document the phone renders. Sources write
// into it through Store.Update; the server streams it out on every change.
package state

import "time"

// CIStatus is the combined result of all workflow runs for one commit.
type CIStatus string

const (
	CIRunning   CIStatus = "running"
	CISuccess   CIStatus = "success"
	CIFailure   CIStatus = "failure"
	CICancelled CIStatus = "cancelled"
)

// Run is the CI result for one commit, combined across its workflows.
type Run struct {
	Status    CIStatus  `json:"status"`
	Branch    string    `json:"branch"`
	SHA       string    `json:"sha"`
	Title     string    `json:"title"`
	Workflow  string    `json:"workflow"` // the failing workflow if any, else the first one
	StartedAt time.Time `json:"started_at"`
	// DurationS is set once the run has finished; while running, the phone
	// counts up from StartedAt itself so the aggregator need not tick.
	DurationS int    `json:"duration_s,omitempty"`
	URL       string `json:"url"`
}

// Project is one repository, as shown in Focus and Grid.
type Project struct {
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	PushedAt      time.Time `json:"pushed_at"`
	// CI is the newest commit on the default branch: the one that matters for deploys.
	CI *Run `json:"ci,omitempty"`
	// Latest is the newest commit on any other branch, when it is newer than CI.
	Latest *Run `json:"latest,omitempty"`
	// Power is build health, 0..9000: the share of recent default-branch
	// commits that passed. Nil until there is a finished build to judge.
	Power   *int `json:"power,omitempty"`
	OpenPRs int  `json:"open_prs"`
	// Deploy is the latest Coolify deployment, when the repo has a Coolify app.
	Deploy *Deploy `json:"deploy,omitempty"`
	// Mismatch: the deployed commit is one whose CI failed (Coolify deploys
	// on push without waiting for CI).
	Mismatch bool `json:"mismatch,omitempty"`
	// Homepage is the repo's website as set on GitHub; a fallback for the icon.
	Homepage string `json:"homepage,omitempty"`
	// Icon is the hash of the project's icon, served at /v1/icons/{hash}.
	Icon string `json:"icon,omitempty"`
}

// DeployStatus mirrors Coolify's deployment queue states.
type DeployStatus string

const (
	DeployQueued    DeployStatus = "queued"
	DeployRunning   DeployStatus = "running"
	DeploySuccess   DeployStatus = "success"
	DeployFailure   DeployStatus = "failure"
	DeployCancelled DeployStatus = "cancelled"
)

// Deploy is one application's latest deployment.
type Deploy struct {
	Status DeployStatus `json:"status"`
	Commit string       `json:"commit"`
	Branch string       `json:"branch"`
	At     time.Time    `json:"at"`
	// Health is the app's container state as Coolify reports it, e.g. "running:healthy".
	Health string `json:"health"`
	// Apps counts the Coolify apps built from this repo (staging, production, ...).
	Apps int `json:"apps"`
	// URL is where the deployed app is served, when Coolify knows it.
	URL string `json:"url,omitempty"`
}

// LastActivity is what Grid sorts by and what auto-Focus follows.
func (p Project) LastActivity() time.Time {
	t := p.PushedAt
	for _, r := range []*Run{p.CI, p.Latest} {
		if r != nil && r.StartedAt.After(t) {
			t = r.StartedAt
		}
	}
	return t
}

// Alert is something worth taking over the screen for. IDs are stable so the
// phone can remember which ones were dismissed.
type Alert struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Project string    `json:"project"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// Source reports whether an upstream is healthy, so the phone can show stale data as stale.
type Source struct {
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Schema is the document format. It goes up only for changes an older phone
// app would misread; new optional fields do not need it.
const Schema = 1

// State is the whole document. Version increases on every change and doubles as ETag.
type State struct {
	Schema   int               `json:"schema"`
	Version  int64             `json:"version"`
	Focus    string            `json:"focus"`
	Projects []Project         `json:"projects"`
	Alerts   []Alert           `json:"alerts"`
	Sources  map[string]Source `json:"sources"`
	Settings Settings          `json:"settings"`
	// Available lists every trackable repo (hidden ones excluded): the
	// pickers in the admin UI and on the phone choose from it.
	Available []string `json:"available"`
	// App is the APK the phone should be running; it updates itself when the hash differs.
	App *AppRelease `json:"app,omitempty"`
	// History is each project's power over the last 14 days, oldest first, -1 = unknown.
	History map[string][]int `json:"history,omitempty"`
	// Agents are live Claude Code sessions, waiting ones first.
	Agents []Agent `json:"agents"`
	// Events from the last 24 h, oldest first: the phone's morning briefing.
	Events []Event `json:"events"`
}
