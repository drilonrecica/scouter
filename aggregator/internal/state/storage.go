package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// loadJSON reads path into v. A missing file is not a problem. A file that
// cannot be parsed is moved aside to path.bad-<time> instead of being
// overwritten by the next save, and the returned message says where it went.
func loadJSON(path string, v any, now time.Time) (found bool, problem string) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, ""
	}
	if err == nil {
		if err = json.Unmarshal(b, v); err == nil {
			return true, ""
		}
	}
	return false, quarantine(path, err, now)
}

func quarantine(path string, cause error, now time.Time) string {
	bad := path + ".bad-" + now.Format("20060102T150405Z")
	if err := os.Rename(path, bad); err != nil {
		return fmt.Sprintf("%s unreadable (%v) and could not be moved aside: %v", path, cause, err)
	}
	return fmt.Sprintf("%s unreadable (%v): kept as %s, starting from defaults", path, cause, filepath.Base(bad))
}

// saveLocked writes v to path and remembers whether that file is currently
// failing to save, for StorageProblems.
func (s *Store) saveLocked(path string, v any) error {
	err := writeAtomic(path, v)
	if err != nil {
		s.saveErrs[path] = err.Error()
	} else {
		delete(s.saveErrs, path)
	}
	return err
}

// StorageProblems lists files that were unreadable at startup (load) and
// files whose last save failed (save), so neither goes unnoticed.
func (s *Store) StorageProblems() (load, save []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for path, err := range s.saveErrs {
		save = append(save, fmt.Sprintf("saving %s: %s", path, err))
	}
	sort.Strings(save)
	return append([]string(nil), s.loadProblems...), save
}

// writeAtomic replaces path with v's JSON so that a crash or power loss
// leaves either the old file or the new one, never a torn or empty one.
func writeAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	// The rename itself lives in the directory: sync it too.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
