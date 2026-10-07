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
