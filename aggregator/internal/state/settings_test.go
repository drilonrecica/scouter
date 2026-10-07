package state

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := DefaultSettings()
	if err := ok.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Settings)
		errHas string
	}{
		{"bad repo name", func(s *Settings) { s.Favorites = []string{"../etc/passwd"} }, "not owner/repo"},
		{"html in repo", func(s *Settings) { s.Hidden = []string{"me/<script>"} }, "not owner/repo"},
		{"too many", func(s *Settings) {
			for i := range maxListed + 1 {
				s.Hidden = append(s.Hidden, fmt.Sprintf("me/r%d", i))
			}
		}, "at most"},
		{"hidden favorite", func(s *Settings) { s.Hidden = []string{"me/a"}; s.Favorites = []string{"me/a"} }, "both hidden"},
		{"unknown focus", func(s *Settings) { s.FocusMode = "chaos" }, "focus_mode"},
		{"pinned without repo", func(s *Settings) { s.FocusMode = FocusPinned }, "needs a pinned"},
		{"rotate range", func(s *Settings) { s.RotateMinutes = 0 }, "rotate_minutes"},
		{"days", func(s *Settings) { s.Schedule.Days = "1-9" }, "schedule.days"},
		{"clock", func(s *Settings) { s.Schedule.On = "9:00" }, "HH:MM"},
		{"on after off", func(s *Settings) { s.Schedule.On, s.Schedule.Off = "19:00", "09:00" }, "before"},
		{"alert hours", func(s *Settings) { s.AlertHours = 100 }, "alert_hours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			tt.mutate(&s)
			err := s.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.errHas) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.errHas)
			}
		})
	}

	d := DefaultSettings()
	d.Favorites = []string{"me/a", "me/b", "me/a"}
	if err := d.Validate(); err != nil || len(d.Favorites) != 2 {
		t.Fatalf("dedupe: %v %v", err, d.Favorites)
	}
}

func TestSettingsPersistAndPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newTestStore(t, "")
	s.UseSettingsFile(path)
	v := s.Get().Version

	next := DefaultSettings()
	next.Kiosk = true
	next.Favorites = []string{"me/fav"}
	if err := s.SetSettings(next); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.Version != v+1 || !st.Settings.Kiosk {
		t.Fatalf("settings not published: version %d->%d, %+v", v, st.Version, st.Settings)
	}

	bad := next
	bad.AlertHours = 0
	if err := s.SetSettings(bad); err == nil {
		t.Fatal("invalid settings accepted")
	}
	if !s.Get().Settings.Kiosk || s.Get().Settings.AlertHours != 12 {
		t.Fatal("a rejected update must leave the old settings in place")
	}

	r := newTestStore(t, "")
	r.UseSettingsFile(path)
	if got := r.Settings(); !got.Kiosk || got.Favorites[0] != "me/fav" {
		t.Fatalf("settings not restored: %+v", got)
	}
}

func TestDeriveHonoursHiddenAndFavorites(t *testing.T) {
	s := newTestStore(t, "")
	s.Update(func(in *Inputs) {
		for i := range MaxProjects + 3 {
			p := project(fmt.Sprintf("p%02d", i), i, CISuccess)
			in.Projects[p.FullName] = p
		}
		in.Available = []string{"me/p00", "me/p01", "me/secret"}
	})
	set := DefaultSettings()
	set.Hidden = []string{"me/p00", "me/secret"}
	set.Favorites = []string{"me/p17"} // oldest: would fall off without being a favorite
	if err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	names := map[string]bool{}
	for _, p := range st.Projects {
		names[p.FullName] = true
	}
	if names["me/p00"] {
		t.Fatal("hidden project published")
	}
	if !names["me/p17"] {
		t.Fatal("favorite dropped past the cap")
	}
	if st.Focus != "me/p01" {
		t.Fatalf("focus = %s, want the newest visible project", st.Focus)
	}
	if fmt.Sprint(st.Available) != "[me/p01]" {
		t.Fatalf("available = %v, hidden repos must not be offered", st.Available)
	}
}

func TestAlertWindowFollowsSettings(t *testing.T) {
	s := newTestStore(t, "")
	p := project("app", 3*60, CIFailure) // failed 3 h ago
	s.Update(func(in *Inputs) { in.Projects[p.FullName] = p })
	if len(s.Get().Alerts) != 1 {
		t.Fatal("3 h old failure should alert with the 12 h default")
	}
	set := DefaultSettings()
	set.AlertHours = 2
	if err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	if len(s.Get().Alerts) != 0 {
		t.Fatal("alert should end when the window shrinks below its age")
	}
}
