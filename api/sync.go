package main

// The VPS side of the PC -> VPS upload (SYNC_ENABLED=1). The probe runs on the home PC, and the PC's own API
// (push.go) sends what the probe writes (captures, saved images, log.jsonl, STOP) here, so the dashboard stays up
// while the PC is off. Only the web app can reach these routes (API_TOKEN), and it only forwards uploads that carry
// SYNC_TOKEN.
// Everything is written atomically into DATA_DIR, the same layout the prototype uses on the PC.

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxCaptureBytes = 20 << 20 // a capture is ~30-200 KB
	maxImageBytes   = 25 << 20
	maxLogChunk     = 8 << 20
	maxStopBytes    = 4 << 10
)

var captureNameRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-\d{3}Z[A-Za-z0-9_-]*\.json$`)

// SyncState is what the VPS already holds: a few dozen bytes however much it holds. Captures are only ever sent
// in name order, so the newest name says which are missing; images are checked by name (missing-images).
type SyncState struct {
	NewestCapture string `json:"newestCapture"`
	Captures      int    `json:"captures"`
	Images        int    `json:"images"`
	LogSize       int64  `json:"logSize"`
	Stop          bool   `json:"stop"`
}

func listNames(dir string, keep func(string) bool) []string {
	out := []string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && keep(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func readSyncState(dataDir string) SyncState {
	caps := listNames(filepath.Join(dataDir, "captures"), captureNameRe.MatchString)
	st := SyncState{Captures: len(caps), Images: len(listNames(filepath.Join(dataDir, "images"), validImageName))}
	if len(caps) > 0 {
		st.NewestCapture = caps[len(caps)-1]
	}
	if fi, err := os.Stat(filepath.Join(dataDir, "log.jsonl")); err == nil {
		st.LogSize = fi.Size()
	}
	_, err := os.Stat(filepath.Join(dataDir, "STOP"))
	st.Stop = err == nil
	return st
}

func validImageName(n string) bool {
	return n != "" && len(n) <= 200 && n == imageFileName(n) && !strings.HasPrefix(n, ".") &&
		!strings.HasSuffix(n, ".part") && !strings.HasSuffix(n, ".missing") && !strings.HasSuffix(n, ".tmp")
}

// syncBody reads an upload (gzip when Content-Encoding says so), refusing more than max bytes after decoding.
func syncBody(r *http.Request, max int64) ([]byte, error) {
	var rd io.Reader = http.MaxBytesReader(nil, r.Body, max)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		rd = gz
	}
	b, err := io.ReadAll(io.LimitReader(rd, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("too large")
	}
	return b, nil
}

// saveUpload creates the folder if needed and writes via a hidden .part file and a rename, so readers never see half a file.
func saveUpload(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".part")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) LastFile() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastFile
}

// adminsOnly is what the home PC gets of the accounts: admins, without their emails.
func adminsOnly(users []User) []User {
	out := []User{}
	for _, u := range users {
		if u.Role == RoleAdmin {
			out = append(out, User{Name: u.Name, Role: u.Role, Hash: u.Hash})
		}
	}
	return out
}

// registerSync adds the upload routes. ingest re-scans the captures folder after a new capture arrives. accountsFile:
// the accounts the home PC copies down (users), so both dashboards accept the same logins.
func registerSync(mux *http.ServeMux, dataDir, accountsFile string, store *Store, remote *Remote, ingest func()) {
	capDir, imgDir := filepath.Join(dataDir, "captures"), filepath.Join(dataDir, "images")
	logFile, stopFile := filepath.Join(dataDir, "log.jsonl"), filepath.Join(dataDir, "STOP")
	fail := func(w http.ResponseWriter, code int, msg string) { http.Error(w, msg, code) }

	mux.HandleFunc("GET /api/sync/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, readSyncState(dataDir))
	})
	// the PC's Admin state, so the VPS Admin page shows the probe (remote.go)
	mux.HandleFunc("PUT /api/sync/admin", func(w http.ResponseWriter, r *http.Request) {
		var s Snapshot
		b, err := syncBody(r, 256<<10)
		if err == nil {
			err = json.Unmarshal(b, &s)
		}
		if err != nil {
			fail(w, http.StatusBadRequest, "bad snapshot")
			return
		}
		remote.SetSnapshot(s, time.Now())
		w.WriteHeader(http.StatusNoContent)
	})
	// the VPS Admin page's orders for the PC: held open up to ?wait= seconds (at most 30) until there is one
	mux.HandleFunc("GET /api/sync/commands", func(w http.ResponseWriter, r *http.Request) {
		wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
		wait = min(max(wait, 0), 30)
		writeJSON(w, map[string][]Command{"commands": remote.Wait(r.Context(), time.Duration(wait)*time.Second)})
	})
	mux.HandleFunc("POST /api/sync/commands/ack", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Results []CommandResult `json:"results"`
		}
		b, err := syncBody(r, 64<<10)
		if err == nil {
			err = json.Unmarshal(b, &body)
		}
		if err != nil {
			fail(w, http.StatusBadRequest, "bad results")
			return
		}
		remote.Ack(body.Results, time.Now())
		w.WriteHeader(http.StatusNoContent)
	})
	// the admin accounts (names, roles, argon2id hashes; never passwords or emails), for the home PC's dashboard.
	// Users who signed up only use this site; their accounts and emails stay here.
	mux.HandleFunc("GET /api/sync/users", func(w http.ResponseWriter, r *http.Request) {
		users, err := LoadUsers(accountsFile)
		if err != nil {
			fail(w, http.StatusInternalServerError, "accounts not readable")
			return
		}
		writeJSON(w, usersFile{Users: adminsOnly(users)})
	})
	mux.HandleFunc("PUT /api/sync/captures/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !captureNameRe.MatchString(name) {
			fail(w, http.StatusBadRequest, "bad capture name")
			return
		}
		// captures are read in name order and never re-read: an older one arriving late would be skipped
		if last := store.LastFile(); name <= last && !fileExists(filepath.Join(capDir, name)) {
			fail(w, http.StatusConflict, "out of order: newest capture here is "+last)
			return
		}
		b, err := syncBody(r, maxCaptureBytes)
		if err != nil || !json.Valid(b) {
			fail(w, http.StatusBadRequest, "not a complete JSON capture")
			return
		}
		if err := saveUpload(filepath.Join(capDir, name), b); err != nil {
			fail(w, http.StatusInternalServerError, "write failed")
			return
		}
		ingest()
		w.WriteHeader(http.StatusNoContent)
	})
	// which of these image names are not here yet: {"names": [...]} -> {"missing": [...]}
	mux.HandleFunc("POST /api/sync/missing-images", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Names []string `json:"names"`
		}
		b, err := syncBody(r, 1<<20)
		if err == nil {
			err = json.Unmarshal(b, &body)
		}
		if err != nil || len(body.Names) > 5000 {
			fail(w, http.StatusBadRequest, "bad names")
			return
		}
		missing := []string{}
		for _, n := range body.Names {
			if validImageName(n) && !fileExists(filepath.Join(imgDir, n)) {
				missing = append(missing, n)
			}
		}
		writeJSON(w, map[string][]string{"missing": missing})
	})
	mux.HandleFunc("PUT /api/sync/images/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !validImageName(name) {
			fail(w, http.StatusBadRequest, "bad image name")
			return
		}
		b, err := syncBody(r, maxImageBytes)
		if err != nil || len(b) == 0 {
			fail(w, http.StatusBadRequest, "bad image")
			return
		}
		if err := saveUpload(filepath.Join(imgDir, name), b); err != nil {
			fail(w, http.StatusInternalServerError, "write failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// log.jsonl only grows: ?offset= must be the size here (append), or 0 to replace it all
	mux.HandleFunc("PUT /api/sync/log", func(w http.ResponseWriter, r *http.Request) {
		off, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		if err != nil || off < 0 {
			fail(w, http.StatusBadRequest, "bad offset")
			return
		}
		var size int64
		if fi, err := os.Stat(logFile); err == nil {
			size = fi.Size()
		}
		if off != 0 && off != size {
			w.Header().Set("X-Log-Size", strconv.FormatInt(size, 10))
			fail(w, http.StatusConflict, "offset is not the current size")
			return
		}
		b, err := syncBody(r, maxLogChunk)
		if err != nil {
			fail(w, http.StatusBadRequest, "bad log chunk")
			return
		}
		if off == 0 {
			err = saveUpload(logFile, b)
		} else {
			var f *os.File
			if f, err = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
				_, err = f.Write(b)
				if cerr := f.Close(); err == nil {
					err = cerr
				}
			}
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "write failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// the probe's safety halt, mirrored so Health shows it
	mux.HandleFunc("PUT /api/sync/stop", func(w http.ResponseWriter, r *http.Request) {
		b, err := syncBody(r, maxStopBytes)
		if err != nil {
			fail(w, http.StatusBadRequest, "bad STOP")
			return
		}
		if err := saveUpload(stopFile, b); err != nil {
			fail(w, http.StatusInternalServerError, "write failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/sync/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := os.Remove(stopFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			fail(w, http.StatusInternalServerError, "remove failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
