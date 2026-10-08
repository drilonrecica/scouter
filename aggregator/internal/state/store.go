package state

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// MaxProjects caps the document: the phone's Grid shows 9 tiles at a time
// and scrolls through the rest, so this bounds how far it can scroll.
const MaxProjects = 15

// Inputs is what sources write. The published State is derived from it.
type Inputs struct {
	Projects  map[string]Project // by FullName
	Sources   map[string]Source
	Available []string // every trackable repo, for the settings pickers
	Settings  Settings
	Deploys   map[string]Deploy // by project FullName, from Coolify
	App       *AppRelease       // APK offered for over-the-air update
	Agents    map[string]Agent  // Claude Code sessions, by session ID
	Icons     map[string]string // icon hash by project FullName; "" = looked, found none
}

// Store owns the inputs, derives the published State and fans changes out to subscribers.
type Store struct {
	mu       sync.Mutex
	in       Inputs
	pub      State
	pubJSON  []byte
	subs     map[chan State]struct{}
	snapshot string // file path, empty = no persistence
	settings string // settings file path, empty = no persistence
	now      func() time.Time

	heartbeat     *Heartbeat
	heartbeatFile string

	history     history
	historyFile string

	events []Event // newest last, at most maxEvents
}

// NewStore restores the last snapshot from snapshotPath if there is one.
func NewStore(snapshotPath string) *Store {
	s := &Store{
		in:       Inputs{Projects: map[string]Project{}, Sources: map[string]Source{}, Settings: DefaultSettings(), Deploys: map[string]Deploy{}, Icons: map[string]string{}},
		subs:     map[chan State]struct{}{},
		snapshot: snapshotPath,
		// UTC: older Android (java.time on API < 33) cannot parse offsets like +02:00.
		now: func() time.Time { return time.Now().UTC() },
	}
	if snapshotPath != "" {
		if b, err := os.ReadFile(snapshotPath); err == nil {
			var st State
			if json.Unmarshal(b, &st) == nil {
				for _, p := range st.Projects {
					s.in.Projects[p.FullName] = p
				}
				s.events = st.Events
				s.pub.Version = st.Version
			}
		}
	}
	s.publishLocked()
	return s
}

// UseSettingsFile loads settings from path and saves every later change there.
func (s *Store) UseSettingsFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = path
	s.in.Settings = loadSettings(path)
	s.publishLocked()
}

// Settings returns the current settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.Settings
}

// SetSettings validates, persists and publishes new settings.
func (s *Store) SetSettings(next Settings) error {
	if err := next.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings != "" {
		if err := writeAtomic(s.settings, next); err != nil {
			return err
		}
	}
	s.in.Settings = next
	s.publishLocked()
	return nil
}

// Get returns the current published state.
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pub
}

// Project returns a project's current input, e.g. as restored from the snapshot.
func (s *Store) Project(fullName string) (Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.in.Projects[fullName]
	return p, ok
}

// Update applies fn to the inputs and publishes if the derived state changed.
func (s *Store) Update(fn func(*Inputs)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.in)
	s.publishLocked()
}

// Subscribe delivers every new state. The channel holds one value and a slow
// reader only ever sees the latest state, which is all a dashboard needs.
func (s *Store) Subscribe() (<-chan State, func()) {
	ch := make(chan State, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Store) publishLocked() {
	now := s.now()
	if s.history != nil && s.history.record(s.in.Projects, now) && s.historyFile != "" {
		_ = writeAtomic(s.historyFile, s.history)
	}
	next := derive(s.in, now) // Version is zero here, so b compares content only
	if s.history != nil {
		for _, p := range next.Projects {
			if ser := s.history.series(p.FullName, now); ser != nil {
				if next.History == nil {
					next.History = map[string][]int{}
				}
				next.History[p.FullName] = ser
			}
		}
	}
	b, _ := json.Marshal(next)
	if s.pubJSON != nil && bytes.Equal(b, s.pubJSON) {
		return
	}
	// Events are derived from the change itself, so they are added after the
	// comparison: an unchanged document must not re-record anything.
	s.events = appendEvents(s.events, transitions(s.pub, next, now))
	next.Events = recentEvents(s.events, now)
	s.pubJSON = b
	next.Version = s.pub.Version + 1
	s.pub = next
	for ch := range s.subs {
		select {
		case <-ch: // drop the stale value
		default:
		}
		ch <- next
	}
	if s.snapshot != "" {
		_ = writeAtomic(s.snapshot, next)
	}
}

func derive(in Inputs, now time.Time) State {
	all := make([]Project, 0, len(in.Projects))
	for _, p := range in.Projects {
		if d, ok := in.Deploys[p.FullName]; ok {
			p.Deploy = &d
			p.Mismatch = deployedRed(p)
		}
		// Unknown until the icon source has looked: keep what the snapshot had.
		if h, ok := in.Icons[p.FullName]; ok {
			p.Icon = h
		}
		all = append(all, p)
	}
	slices.SortFunc(all, func(a, b Project) int {
		if c := b.LastActivity().Compare(a.LastActivity()); c != 0 {
			return c
		}
		return cmp.Compare(a.FullName, b.FullName)
	})

	// Keep the most active projects, plus favorites and anything failing
	// further down: a broken build must never fall off the dashboard for
	// being old. Hidden repos never show, even from an old snapshot.
	var projects []Project
	kept := 0
	for _, p := range all {
		if in.Settings.IsHidden(p.FullName) {
			continue
		}
		if kept < MaxProjects || in.Settings.IsFavorite(p.FullName) || (p.CI != nil && p.CI.Status == CIFailure) {
			projects = append(projects, p)
		}
		kept++
	}

	available := slices.DeleteFunc(slices.Clone(in.Available), in.Settings.IsHidden)
	st := State{Projects: projects, Alerts: []Alert{}, Sources: in.Sources, Settings: in.Settings, Available: available, App: in.App,
		Agents: liveAgents(in.Agents, now)}
	if len(projects) > 0 {
		st.Focus = projects[0].FullName
	}

	window := in.Settings.AlertWindow()
	for _, p := range projects {
		// Stamped with when things failed, not when Scouter noticed: a
		// restart must not make an old failure look new.
		if p.CI != nil && p.CI.Status == CIFailure && now.Sub(p.CI.StartedAt) <= window {
			st.Alerts = append(st.Alerts, Alert{
				ID: fmt.Sprintf("ci:%s:%s", p.FullName, p.CI.SHA), Kind: "ci_failed", Project: p.FullName,
				At:   p.CI.StartedAt.Add(time.Duration(p.CI.DurationS) * time.Second),
				Text: fmt.Sprintf("%s failed on %s: %s", p.CI.Workflow, p.CI.Branch, p.CI.Title),
			})
		}
		if d := p.Deploy; d != nil && now.Sub(d.At) <= window {
			switch {
			case d.Status == DeployFailure:
				st.Alerts = append(st.Alerts, Alert{
					ID: fmt.Sprintf("deploy:%s:%s", p.FullName, d.Commit), Kind: "deploy_failed", Project: p.FullName, At: d.At,
					Text: fmt.Sprintf("Deploy of %s failed on Coolify", short(d.Commit)),
				})
			case p.Mismatch:
				st.Alerts = append(st.Alerts, Alert{
					ID: fmt.Sprintf("red:%s:%s", p.FullName, d.Commit), Kind: "deployed_red", Project: p.FullName, At: d.At,
					Text: fmt.Sprintf("Deployed %s although its CI failed", short(d.Commit)),
				})
			}
		}
	}
	slices.SortFunc(st.Alerts, func(a, b Alert) int { return b.At.Compare(a.At) })
	return st
}

// deployedRed reports a successful deploy of a commit whose CI failed.
func deployedRed(p Project) bool {
	d := p.Deploy
	if d == nil || d.Status != DeploySuccess || p.CI == nil || p.CI.Status != CIFailure || d.Commit == "" {
		return false
	}
	return sameCommit(d.Commit, p.CI.SHA)
}

// sameCommit compares full or abbreviated SHAs.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if len(a) > len(b) {
		a, b = b, a
	}
	return len(a) >= 7 && strings.HasPrefix(b, a)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func writeAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
