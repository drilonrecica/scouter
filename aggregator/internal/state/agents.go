package state

import (
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Agent is one Claude Code session as reported by its hooks.
type Agent struct {
	ID      string    `json:"id"`
	Project string    `json:"project"` // basename of the session's working directory
	State   string    `json:"state"`   // AgentWorking, AgentWaiting or AgentDone
	Since   time.Time `json:"since"`
	Seen    time.Time `json:"-"` // last event, for expiry
}

const (
	AgentWorking = "working"
	AgentWaiting = "waiting"
	AgentDone    = "done"

	agentDoneFor = 10 * time.Minute // a finished session lingers this long
	agentIdleFor = 30 * time.Minute // any silent session is dropped after this
	maxAgents    = 20
)

// AgentEvent applies one hook event. Event names are Claude Code's hook events.
func (s *Store) AgentEvent(sessionID, cwd, event string) {
	if sessionID == "" {
		return
	}
	s.Update(func(in *Inputs) {
		if in.Agents == nil {
			in.Agents = map[string]Agent{}
		}
		a, ok := in.Agents[sessionID]
		if !ok {
			a = Agent{ID: sessionID}
		}
		if cwd != "" {
			a.Project = filepath.Base(filepath.Clean(cwd))
		}
		var next string
		switch event {
		case "UserPromptSubmit", "PreToolUse", "PostToolUse":
			next = AgentWorking
		case "Notification":
			next = AgentWaiting
		case "Stop":
			next = AgentDone
		case "SessionEnd":
			delete(in.Agents, sessionID)
			return
		default:
			return
		}
		if next != a.State {
			a.State, a.Since = next, s.now()
		}
		a.Seen = s.now()
		in.Agents[sessionID] = a
	})
}

// liveAgents drops finished and silent sessions and orders the rest:
// waiting first (they need the owner), then working, then done.
func liveAgents(all map[string]Agent, now time.Time) []Agent {
	rank := map[string]int{AgentWaiting: 0, AgentWorking: 1, AgentDone: 2}
	var out []Agent
	for _, a := range all {
		if now.Sub(a.Seen) > agentIdleFor || (a.State == AgentDone && now.Sub(a.Since) > agentDoneFor) {
			continue
		}
		a.Seen = time.Time{} // internal: keep it out of the document
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b Agent) int {
		if d := rank[a.State] - rank[b.State]; d != 0 {
			return d
		}
		if c := a.Since.Compare(b.Since); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(out) > maxAgents {
		out = out[:maxAgents]
	}
	return out
}
