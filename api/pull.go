package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PullStatus says whether a "pull now" request is waiting for the prototype's loop to pick it up.
// The dashboard never contacts FANS: it only writes requests/PULL, which the running loop answers
// with its next pass (still inside the request budget, and never sooner than 2 min after the last pass).
type PullStatus struct {
	Pending     bool       `json:"pending"`
	RequestedAt *time.Time `json:"requestedAt,omitempty"`
	Enabled     bool       `json:"enabled"` // false when no writable requests folder is mounted
}

func pullFile(dir string) string { return filepath.Join(dir, "PULL") }

func ReadPull(dir string) PullStatus {
	st := PullStatus{Enabled: dirWritable(dir)}
	b, err := os.ReadFile(pullFile(dir))
	if err != nil {
		return st
	}
	st.Pending = true
	if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b))); err == nil {
		st.RequestedAt = &t
	}
	return st
}

// RequestPull writes requests/PULL unless one is already pending. Atomic: write a temp file, then rename.
func RequestPull(dir string, now time.Time) (PullStatus, error) {
	if st := ReadPull(dir); st.Pending {
		return st, nil
	}
	if dir == "" {
		return PullStatus{}, errors.New("no requests folder configured")
	}
	tmp := pullFile(dir) + ".tmp"
	if err := os.WriteFile(tmp, []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return PullStatus{}, err
	}
	if err := os.Rename(tmp, pullFile(dir)); err != nil {
		os.Remove(tmp)
		return PullStatus{}, err
	}
	return ReadPull(dir), nil
}

func dirWritable(dir string) bool {
	if dir == "" {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
