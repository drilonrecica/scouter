package state

import (
	"fmt"
	"slices"
	"time"
)

// Event is something that happened, for the phone's morning briefing.
type Event struct {
	At      time.Time `json:"at"`
	Project string    `json:"project"`
	Kind    string    `json:"kind"` // ci_failed, ci_recovered, deploy_ok, deploy_failed
	Text    string    `json:"text"`
}

const (
	maxEvents     = 50
	eventsPublish = 24 * time.Hour
)

// transitions compares two published documents and lists what changed.
// The first document after a start has nothing to compare with and records nothing.
func transitions(prev, next State, now time.Time) []Event {
	if prev.Version == 0 {
		return nil
	}
	before := map[string]Project{}
	for _, p := range prev.Projects {
		before[p.FullName] = p
	}
	var out []Event
	for _, p := range next.Projects {
		old, ok := before[p.FullName]
		if !ok {
			continue
		}
		if p.CI != nil && old.CI != nil && p.CI.SHA != "" {
			switch {
			case p.CI.Status == CIFailure && old.CI.Status != CIFailure:
				out = append(out, Event{now, p.FullName, "ci_failed", fmt.Sprintf("%s failed: %s", p.CI.Workflow, p.CI.Title)})
			case p.CI.Status == CISuccess && old.CI.Status == CIFailure:
				out = append(out, Event{now, p.FullName, "ci_recovered", fmt.Sprintf("back to green: %s", p.CI.Title)})
			}
		}
		if d := p.Deploy; d != nil {
			o := old.Deploy
			changed := o == nil || o.Status != d.Status || o.Commit != d.Commit
			switch {
			case changed && d.Status == DeploySuccess:
				out = append(out, Event{now, p.FullName, "deploy_ok", "deployed " + short(d.Commit)})
			case changed && d.Status == DeployFailure:
				out = append(out, Event{now, p.FullName, "deploy_failed", "deploy of " + short(d.Commit) + " failed"})
			}
		}
	}
	return out
}

// appendEvents keeps the newest maxEvents.
func appendEvents(log []Event, add []Event) []Event {
	log = append(log, add...)
	if len(log) > maxEvents {
		log = slices.Clone(log[len(log)-maxEvents:])
	}
	return log
}

func recentEvents(log []Event, now time.Time) []Event {
	out := []Event{}
	for _, e := range log {
		if now.Sub(e.At) <= eventsPublish {
			out = append(out, e)
		}
	}
	return out
}
