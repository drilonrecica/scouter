package state

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Settings is the single source of truth for what Scouter tracks and how the
// phone behaves. The admin UI and the phone both edit it; it is published in
// the state document, so it must never hold secrets.
type Settings struct {
	Hidden        []string   `json:"hidden"`         // never tracked
	Favorites     []string   `json:"favorites"`      // always tracked, shown first, in this order
	FocusMode     string     `json:"focus_mode"`     // FocusLatest, FocusPinned or FocusRotate
	Pinned        string     `json:"pinned"`         // for FocusPinned
	RotateMinutes int        `json:"rotate_minutes"` // for FocusRotate: cycles the favorites
	Schedule      Schedule   `json:"schedule"`
	Kiosk         bool       `json:"kiosk"`
	Background    Background `json:"background"`
	AlertHours    int        `json:"alert_hours"` // failures younger than this take over the screen
	Wave          bool       `json:"wave"`        // wave over the proximity sensor to peek at night
	Briefing      bool       `json:"briefing"`    // morning report when the screen turns on
}

// Schedule is when the phone's screen is on. Days "1-5" = Monday to Friday
// (wrapping like "6-2" is allowed); On must be before Off.
type Schedule struct {
	Days string `json:"days"`
	On   string `json:"on"`
	Off  string `json:"off"`
}

// Background toggles the phone's backdrop layers.
type Background struct {
	Aura  bool `json:"aura"`
	Stars bool `json:"stars"`
	Mesh  bool `json:"mesh"`
	// Stage draws an original Dragon Ball-inspired scene behind the HUD,
	// chosen by the Focus project; Weather overlays the status on it.
	Stage   bool `json:"stage"`
	Weather bool `json:"weather"`
}

const (
	FocusLatest = "latest"
	FocusPinned = "pinned"
	FocusRotate = "rotate"

	maxListed = 50
)

// DefaultSettings is what a fresh install starts with.
func DefaultSettings() Settings {
	return Settings{
		Hidden:        []string{},
		Favorites:     []string{},
		FocusMode:     FocusLatest,
		RotateMinutes: 5,
		Schedule:      Schedule{Days: "1-5", On: "09:00", Off: "19:00"},
		Background:    Background{Aura: true, Stars: true, Stage: true, Weather: true},
		AlertHours:    12,
		Wave:          true,
		Briefing:      true,
	}
}

// AlertWindow is how recent a failure must be to raise an alert.
func (s Settings) AlertWindow() time.Duration { return time.Duration(s.AlertHours) * time.Hour }

// IsHidden reports whether a repo is excluded from tracking.
func (s Settings) IsHidden(fullName string) bool { return slices.Contains(s.Hidden, fullName) }

// IsFavorite reports whether a repo is a favorite.
func (s Settings) IsFavorite(fullName string) bool { return slices.Contains(s.Favorites, fullName) }

var (
	repoName = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)
	dayRange = regexp.MustCompile(`^[1-7]-[1-7]$`)
	clock    = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// Validate rejects anything the phone or the admin UI should never be able to
// store. Input comes from the network, so every field is checked.
func (s *Settings) Validate() error {
	for _, l := range []struct {
		name  string
		items []string
	}{{"hidden", s.Hidden}, {"favorites", s.Favorites}} {
		if len(l.items) > maxListed {
			return fmt.Errorf("%s: at most %d repos", l.name, maxListed)
		}
		for _, r := range l.items {
			if !repoName.MatchString(r) {
				return fmt.Errorf("%s: %q is not owner/repo", l.name, r)
			}
		}
	}
	s.Hidden = dedupe(s.Hidden)
	s.Favorites = dedupe(s.Favorites)
	for _, f := range s.Favorites {
		if s.IsHidden(f) {
			return fmt.Errorf("%s is both hidden and a favorite", f)
		}
	}
	switch s.FocusMode {
	case FocusLatest, FocusRotate:
	case FocusPinned:
		if s.Pinned == "" {
			return errors.New("focus_mode pinned needs a pinned repo")
		}
	default:
		return fmt.Errorf("focus_mode %q: want latest, pinned or rotate", s.FocusMode)
	}
	if s.Pinned != "" && !repoName.MatchString(s.Pinned) {
		return fmt.Errorf("pinned %q is not owner/repo", s.Pinned)
	}
	if s.RotateMinutes < 1 || s.RotateMinutes > 60 {
		return errors.New("rotate_minutes: 1..60")
	}
	if !dayRange.MatchString(s.Schedule.Days) {
		return fmt.Errorf("schedule.days %q: want like 1-5 (1 = Monday)", s.Schedule.Days)
	}
	if !clock.MatchString(s.Schedule.On) || !clock.MatchString(s.Schedule.Off) {
		return errors.New("schedule.on/off: want HH:MM")
	}
	if s.Schedule.On >= s.Schedule.Off { // zero-padded, so string order is time order
		return errors.New("schedule.on must be before schedule.off")
	}
	if s.AlertHours < 1 || s.AlertHours > 72 {
		return errors.New("alert_hours: 1..72")
	}
	return nil
}

func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// loadSettings reads settings from path, falling back to defaults for a
// missing or invalid file. An invalid file is moved aside, not overwritten
// by the next save, so favorites and hidden repos can be recovered by hand.
func loadSettings(path string, now time.Time) (Settings, string) {
	s := DefaultSettings()
	found, problem := loadJSON(path, &s, now)
	if !found {
		return DefaultSettings(), problem
	}
	if err := s.Validate(); err != nil {
		return DefaultSettings(), quarantine(path, err, now)
	}
	return s, ""
}
