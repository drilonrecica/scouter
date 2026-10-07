package state

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// MaxProjects caps the document: the phone shows at most 9 tiles, and a few
// spares let the owner pin something slightly older in Focus.
const MaxProjects = 15

// AlertWindow: only failures this recent take over the screen. Older red
// builds are known state and stay visible in Grid without shouting.
const AlertWindow = 12 * time.Hour

// Inputs is what sources write. The published State is derived from it.
type Inputs struct {
	Projects map[string]Project // by FullName
	Sources  map[string]Source
}

// Store owns the inputs, derives the published State and fans changes out to subscribers.
type Store struct {
	mu       sync.Mutex
	in       Inputs
	pub      State
	pubJSON  []byte
	subs     map[chan State]struct{}
	snapshot string // file path, empty = no persistence
	now      func() time.Time
}

// NewStore restores the last snapshot from snapshotPath if there is one.
func NewStore(snapshotPath string) *Store {
	s := &Store{
		in:       Inputs{Projects: map[string]Project{}, Sources: map[string]Source{}},
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
				s.pub.Version = st.Version
			}
		}
	}
	s.publishLocked()
	return s
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
	next := derive(s.in, s.now()) // Version is zero here, so b compares content only
	b, _ := json.Marshal(next)
	if s.pubJSON != nil && bytes.Equal(b, s.pubJSON) {
		return
	}
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
		all = append(all, p)
	}
	slices.SortFunc(all, func(a, b Project) int {
		if c := b.LastActivity().Compare(a.LastActivity()); c != 0 {
			return c
		}
		return cmp.Compare(a.FullName, b.FullName)
	})

	// Keep the most active projects, plus anything failing further down:
	// a broken build must never fall off the dashboard for being old.
	var projects []Project
	for i, p := range all {
		if i < MaxProjects || (p.CI != nil && p.CI.Status == CIFailure) {
			projects = append(projects, p)
		}
	}

	st := State{Projects: projects, Alerts: []Alert{}, Sources: in.Sources}
	if len(projects) > 0 {
		st.Focus = projects[0].FullName
	}

	for _, p := range projects {
		if p.CI == nil || p.CI.Status != CIFailure || now.Sub(p.CI.StartedAt) > AlertWindow {
			continue
		}
		// Stamped with when the run failed, not when Scouter noticed: a
		// restart must not make an old failure look new.
		st.Alerts = append(st.Alerts, Alert{
			ID: fmt.Sprintf("ci:%s:%s", p.FullName, p.CI.SHA), Kind: "ci_failed", Project: p.FullName,
			At:   p.CI.StartedAt.Add(time.Duration(p.CI.DurationS) * time.Second),
			Text: fmt.Sprintf("%s failed on %s: %s", p.CI.Workflow, p.CI.Branch, p.CI.Title),
		})
	}
	slices.SortFunc(st.Alerts, func(a, b Alert) int { return b.At.Compare(a.At) })
	return st
}

func writeAtomic(path string, st State) error {
	b, err := json.Marshal(st)
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
