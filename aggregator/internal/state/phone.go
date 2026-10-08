package state

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// MaxAPK bounds uploads; the Scouter APK is well under 1 MB.
const MaxAPK = 30 << 20

// ErrNotAPK means the upload was not a ZIP file (an APK is one) or too large.
var ErrNotAPK = errors.New("not an APK (expected a ZIP file under 30 MB)")

// SaveAPK stores an uploaded APK atomically at path and describes it. Only the
// shape is checked here; the phone's package installer verifies signature
// and version.
func SaveAPK(path string, src io.Reader) (*AppRelease, error) {
	br := bufio.NewReader(io.LimitReader(src, MaxAPK+1))
	if magic, err := br.Peek(4); err != nil || string(magic) != "PK\x03\x04" {
		return nil, ErrNotAPK
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(tmp, br)
	if err == nil && n > MaxAPK {
		err = ErrNotAPK
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return DescribeAPK(path)
}

// Heartbeat is what the phone reports about itself every minute. It is kept
// out of the state document so it does not bump the version each time.
type Heartbeat struct {
	AppVersion  string    `json:"app_version"`
	VersionCode int       `json:"version_code"`
	Battery     int       `json:"battery"`
	Charging    bool      `json:"charging"`
	TempC       float64   `json:"temp_c"`
	UptimeS     int64     `json:"uptime_s"`
	PssKB       int       `json:"pss_kb"`
	WifiRSSI    int       `json:"wifi_rssi"`
	DeviceOwner bool      `json:"device_owner"`
	LockTask    bool      `json:"lock_task"`
	LastOTA     string    `json:"last_ota"`
	LastCrash   string    `json:"last_crash,omitempty"` // the app's most recent uncaught exception
	At          time.Time `json:"at"`                   // set by the server on receipt
}

// AppRelease describes the APK offered to the phone for over-the-air update.
type AppRelease struct {
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// UsePhoneFiles sets where the latest heartbeat is kept and restores it.
func (s *Store) UsePhoneFiles(heartbeatPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatFile = heartbeatPath
	if b, err := os.ReadFile(heartbeatPath); err == nil {
		var h Heartbeat
		if json.Unmarshal(b, &h) == nil {
			s.heartbeat = &h
		}
	}
}

// SetHeartbeat records the phone's latest report.
func (s *Store) SetHeartbeat(h Heartbeat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.At = s.now()
	s.heartbeat = &h
	if s.heartbeatFile != "" {
		_ = s.saveLocked(s.heartbeatFile, h) // failures show in StorageProblems
	}
}

// Heartbeat returns the phone's latest report, if any.
func (s *Store) Heartbeat() *Heartbeat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heartbeat == nil {
		return nil
	}
	h := *s.heartbeat
	return &h
}

// SetApp publishes the APK on offer (nil withdraws it).
func (s *Store) SetApp(a *AppRelease) {
	s.Update(func(in *Inputs) { in.App = a })
}

// DescribeAPK hashes an APK file for publishing.
func DescribeAPK(path string) (*AppRelease, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	st, _ := f.Stat()
	return &AppRelease{SHA256: hex.EncodeToString(h.Sum(nil)), Size: n, UploadedAt: st.ModTime().UTC()}, nil
}
