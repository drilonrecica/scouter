package state

import (
	"time"
)

const (
	historyDays = 14 // published per project
	historyKeep = 30 // kept on disk
)

// history is each project's power per UTC day: project -> "2006-01-02" -> power.
type history map[string]map[string]int

// UseHistoryFile loads past power levels and saves later ones to path.
func (s *Store) UseHistoryFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyFile = path
	if _, problem := loadJSON(path, &s.history, s.now()); problem != "" {
		s.loadProblems = append(s.loadProblems, problem)
		s.history = nil
	}
	if s.history == nil {
		s.history = history{}
	}
	s.publishLocked()
}

// record notes today's power of every project; the last value of a day wins.
// It reports whether anything changed (so the caller saves the file).
func (h history) record(projects map[string]Project, now time.Time) bool {
	day := now.Format(time.DateOnly)
	changed := false
	for name, p := range projects {
		if p.Power == nil {
			continue
		}
		days := h[name]
		if days == nil {
			days = map[string]int{}
			h[name] = days
		}
		if v, ok := days[day]; !ok || v != *p.Power {
			days[day] = *p.Power
			changed = true
		}
	}
	cut := now.AddDate(0, 0, -historyKeep).Format(time.DateOnly)
	for name, days := range h {
		for d := range days {
			if d < cut {
				delete(days, d)
				changed = true
			}
		}
		if len(days) == 0 {
			delete(h, name)
		}
	}
	return changed
}

// series is the last historyDays days, oldest first, -1 where unknown.
func (h history) series(name string, now time.Time) []int {
	days, ok := h[name]
	if !ok {
		return nil
	}
	out := make([]int, historyDays)
	for i := range historyDays {
		d := now.AddDate(0, 0, i-historyDays+1).Format(time.DateOnly)
		if v, ok := days[d]; ok {
			out[i] = v
		} else {
			out[i] = -1
		}
	}
	return out
}
