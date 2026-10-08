package state

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func project(name string, pushedMinAgo int, ci CIStatus) Project {
	p := Project{Name: name, FullName: "me/" + name, DefaultBranch: "master", PushedAt: t0.Add(-time.Duration(pushedMinAgo) * time.Minute)}
	if ci != "" {
		p.CI = &Run{Status: ci, Branch: "master", SHA: "sha-" + name, Title: "commit", Workflow: "CI", StartedAt: p.PushedAt}
	}
	return p
}

func newTestStore(t *testing.T, path string) *Store {
	t.Helper()
	s := NewStore(path)
	s.now = func() time.Time { return t0 }
	// NewStore published with the real clock; republish on the test clock so
	// time-based rules (alert window, expiry) don't depend on today's date.
	s.Update(func(*Inputs) {})
	return s
}

func TestDeriveSortsByActivityAndFocusesNewest(t *testing.T) {
	s := newTestStore(t, "")
	s.Update(func(in *Inputs) {
		for _, p := range []Project{project("old", 300, CISuccess), project("new", 1, CISuccess), project("mid", 60, "")} {
			in.Projects[p.FullName] = p
		}
	})
	st := s.Get()
	var got []string
	for _, p := range st.Projects {
		got = append(got, p.Name)
	}
	if fmt.Sprint(got) != "[new mid old]" {
		t.Fatalf("order = %v", got)
	}
	if st.Focus != "me/new" {
		t.Fatalf("focus = %q", st.Focus)
	}
}

func TestDeriveKeepsOldFailingProjectBeyondCap(t *testing.T) {
	s := newTestStore(t, "")
	s.Update(func(in *Inputs) {
		for i := range MaxProjects {
			p := project(fmt.Sprintf("p%02d", i), i, CISuccess)
			in.Projects[p.FullName] = p
		}
		old := project("ancient-broken", 10_000, CIFailure)
		in.Projects[old.FullName] = old
		quiet := project("ancient-green", 10_001, CISuccess)
		in.Projects[quiet.FullName] = quiet
	})
	st := s.Get()
	if len(st.Projects) != MaxProjects+1 {
		t.Fatalf("got %d projects, want %d", len(st.Projects), MaxProjects+1)
	}
	if last := st.Projects[len(st.Projects)-1].Name; last != "ancient-broken" {
		t.Fatalf("last = %q, want the failing project kept", last)
	}
}

func TestAlertsAppearOnFailureAndVanishOnRecovery(t *testing.T) {
	s := newTestStore(t, "")
	set := func(ci CIStatus) {
		s.Update(func(in *Inputs) { p := project("app", 5, ci); in.Projects[p.FullName] = p })
	}

	set(CIFailure)
	st := s.Get()
	// project() starts the run 5 min before t0 with no duration recorded.
	if len(st.Alerts) != 1 || st.Alerts[0].ID != "ci:me/app:sha-app" || !st.Alerts[0].At.Equal(t0.Add(-5*time.Minute)) {
		t.Fatalf("alerts = %+v", st.Alerts)
	}

	// Noticed again later (e.g. after a restart): the alert keeps the run's time.
	s.now = func() time.Time { return t0.Add(time.Hour) }
	set(CIFailure)
	if at := s.Get().Alerts[0].At; !at.Equal(t0.Add(-5 * time.Minute)) {
		t.Fatalf("alert time moved to %v", at)
	}

	set(CISuccess)
	if n := len(s.Get().Alerts); n != 0 {
		t.Fatalf("alerts after recovery = %d", n)
	}
}

func TestVersionOnlyBumpsOnChangeAndSubscribersGetLatest(t *testing.T) {
	s := newTestStore(t, "")
	ch, cancel := s.Subscribe()
	defer cancel()
	v0 := s.Get().Version

	p := project("app", 5, CISuccess)
	s.Update(func(in *Inputs) { in.Projects[p.FullName] = p })
	s.Update(func(in *Inputs) { in.Projects[p.FullName] = p }) // no-op
	if v := s.Get().Version; v != v0+1 {
		t.Fatalf("version = %d, want %d", v, v0+1)
	}

	q := project("other", 1, CIRunning)
	s.Update(func(in *Inputs) { in.Projects[q.FullName] = q })
	got := <-ch
	if got.Version != v0+2 {
		t.Fatalf("subscriber got version %d, want latest %d", got.Version, v0+2)
	}
}

func TestSnapshotRestoresProjectsAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := newTestStore(t, path)
	p := project("app", 5, CIFailure)
	s.Update(func(in *Inputs) { in.Projects[p.FullName] = p })
	before := s.Get()

	r := newTestStore(t, path)
	after := r.Get()
	if len(after.Projects) != 1 || after.Projects[0].FullName != "me/app" {
		t.Fatalf("restored projects = %+v", after.Projects)
	}
	if after.Version <= before.Version {
		t.Fatalf("version went from %d to %d; must keep increasing across restarts", before.Version, after.Version)
	}
	if len(after.Alerts) != 1 || !after.Alerts[0].At.Equal(before.Alerts[0].At) {
		t.Fatalf("alerts after restore = %+v, want %+v", after.Alerts, before.Alerts)
	}
}

func TestOldFailuresDoNotAlert(t *testing.T) {
	s := newTestStore(t, "")
	stale := project("stale", int(DefaultSettings().AlertWindow()/time.Minute)+1, CIFailure)
	s.Update(func(in *Inputs) { in.Projects[stale.FullName] = stale })
	st := s.Get()
	if len(st.Alerts) != 0 {
		t.Fatalf("alerts = %+v, want none for a failure older than the window", st.Alerts)
	}
	if st.Projects[0].CI.Status != CIFailure {
		t.Fatal("the project itself must still show as failing")
	}
}

func TestDeployMismatchAndAlerts(t *testing.T) {
	s := newTestStore(t, "")
	red := project("app", 5, CIFailure) // CI sha "sha-app" failed 5 min ago
	green := project("ok", 5, CISuccess)
	s.Update(func(in *Inputs) {
		in.Projects[red.FullName] = red
		in.Projects[green.FullName] = green
		in.Deploys[red.FullName] = Deploy{Status: DeploySuccess, Commit: "sha-app", At: t0.Add(-time.Minute)}
		in.Deploys[green.FullName] = Deploy{Status: DeployFailure, Commit: "sha-okk", At: t0.Add(-2 * time.Minute)}
	})
	st := s.Get()
	kinds := map[string]bool{}
	for _, a := range st.Alerts {
		kinds[a.Kind] = true
	}
	if !kinds["ci_failed"] || !kinds["deployed_red"] || !kinds["deploy_failed"] {
		t.Fatalf("alerts = %+v", st.Alerts)
	}
	for _, p := range st.Projects {
		if p.Name == "app" && (!p.Mismatch || p.Deploy == nil) {
			t.Fatalf("app should be flagged deployed-while-red: %+v", p)
		}
		if p.Name == "ok" && p.Mismatch {
			t.Fatal("a failed deploy is not a mismatch")
		}
	}

	if !sameCommit("ABCDEF1", "abcdef1234") || sameCommit("abc", "abcdef") || sameCommit("abcdef1", "abcdef2") {
		t.Fatal("sameCommit")
	}
}

func TestPowerHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	s := newTestStore(t, "")
	s.UseHistoryFile(path)
	pw := func(v int) *int { return &v }
	set := func(v int) {
		s.Update(func(in *Inputs) { p := project("app", 5, CISuccess); p.Power = pw(v); in.Projects[p.FullName] = p })
	}

	set(9000)
	s.now = func() time.Time { return t0.AddDate(0, 0, 1) }
	set(6750)
	set(7200) // same day again: last value wins

	h := s.Get().History["me/app"]
	if len(h) != historyDays || h[historyDays-1] != 7200 || h[historyDays-2] != 9000 || h[0] != -1 {
		t.Fatalf("history = %v", h)
	}

	r := newTestStore(t, "")
	r.now = s.now
	r.UseHistoryFile(path)
	r.Update(func(in *Inputs) { p := project("app", 5, CISuccess); p.Power = pw(7200); in.Projects[p.FullName] = p })
	if got := r.Get().History["me/app"]; got[historyDays-2] != 9000 {
		t.Fatalf("history not restored: %v", got)
	}

	s.now = func() time.Time { return t0.AddDate(0, 0, historyKeep+2) }
	set(9000)
	if days := s.history["me/app"]; len(days) != 1 {
		t.Fatalf("old days not pruned: %v", days)
	}
}

func TestEventsRecordTransitions(t *testing.T) {
	s := newTestStore(t, "")
	set := func(ci CIStatus, d *Deploy) {
		s.Update(func(in *Inputs) {
			p := project("app", 5, ci)
			in.Projects[p.FullName] = p
			if d != nil {
				in.Deploys[p.FullName] = *d
			}
		})
	}
	set(CISuccess, nil)
	set(CIFailure, nil)
	set(CIFailure, nil) // no change: no event
	set(CISuccess, &Deploy{Status: DeploySuccess, Commit: "sha-app", At: t0})
	var kinds []string
	for _, e := range s.Get().Events {
		kinds = append(kinds, e.Kind)
	}
	if fmt.Sprint(kinds) != "[ci_failed ci_recovered deploy_ok]" {
		t.Fatalf("events = %v", kinds)
	}
	s.now = func() time.Time { return t0.Add(25 * time.Hour) }
	set(CISuccess, &Deploy{Status: DeploySuccess, Commit: "sha-app", At: t0, Health: "x"})
	if n := len(s.Get().Events); n != 0 {
		t.Fatalf("events older than 24 h still published: %d", n)
	}
}

func TestAgents(t *testing.T) {
	s := newTestStore(t, "")
	s.AgentEvent("a", "/home/me/Code/igris", "UserPromptSubmit")
	s.AgentEvent("b", "/home/me/Code/scouter", "UserPromptSubmit")
	s.AgentEvent("b", "", "Notification")
	ag := s.Get().Agents
	if len(ag) != 2 || ag[0].ID != "b" || ag[0].State != AgentWaiting || ag[0].Project != "scouter" || ag[1].Project != "igris" {
		t.Fatalf("agents = %+v: waiting first, project from cwd", ag)
	}
	s.AgentEvent("a", "", "SessionEnd")
	s.AgentEvent("b", "", "Stop")
	if ag := s.Get().Agents; len(ag) != 1 || ag[0].State != AgentDone {
		t.Fatalf("agents = %+v", ag)
	}
	s.now = func() time.Time { return t0.Add(agentDoneFor + time.Minute) }
	s.Update(func(*Inputs) {})
	if ag := s.Get().Agents; len(ag) != 0 {
		t.Fatalf("finished session should expire: %+v", ag)
	}
}

func TestReturningAgentStartsFresh(t *testing.T) {
	s := newTestStore(t, "")
	s.AgentEvent("a", "/x/igris", "Notification")
	later := t0.Add(agentIdleFor + time.Hour)
	s.now = func() time.Time { return later }
	s.AgentEvent("a", "/x/igris", "Notification") // same session, same state, after expiry
	if ag := s.Get().Agents; len(ag) != 1 || !ag[0].Since.Equal(later) {
		t.Fatalf("agents = %+v: a session back from expiry must not keep its old since", ag)
	}
}
