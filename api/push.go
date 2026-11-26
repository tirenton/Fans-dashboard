package main

// The home PC side of the PC -> VPS upload (VPS_URL + VPS_SYNC_TOKEN set). The probe only writes its folder; this
// API, which already reads that folder, sends what is new to the VPS dashboard (sync.go there): captures in name
// order, saved images, new lines of log.jsonl, and whether STOP exists. It never touches the probe or FANS.
//
// Cheap by design: every 20 s it compares a small signature of the folder (newest capture, file counts, log size,
// STOP) with the last one it finished; when nothing changed it sends nothing, except one state check every 10 min
// (in case the VPS was reset). The VPS's state is a few dozen bytes, however much it holds.

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pushEvery     = 20 * time.Second
	pushRecheck   = 10 * time.Minute // ask the VPS even when nothing changed here
	pushSettle    = 15 * time.Second // the probe writes a capture in one go; leave a just-written file a moment
	pushPerRound  = 200
	pushLogChunk  = 4 << 20
	pushNamesEach = 1000
	pullUsersEach = 5 * time.Minute // copy the VPS's accounts down this often (and at start)
)

// PushStatus is shown on the PC's Admin page.
type PushStatus struct {
	Enabled  bool       `json:"enabled"`
	Host     string     `json:"host,omitempty"` // the VPS, without the token
	LastOK   *time.Time `json:"lastOk,omitempty"`
	LastTry  *time.Time `json:"lastTry,omitempty"`
	Error    string     `json:"error,omitempty"`
	Captures int        `json:"captures"` // captures on the VPS after the last round
	Pending  int        `json:"pending"`  // captures here not sent yet
	Sent     struct {
		Captures int   `json:"captures"`
		Images   int   `json:"images"`
		LogBytes int64 `json:"logBytes"`
	} `json:"sent"` // since this API started
	// accounts copied down from the VPS, so this PC's dashboard accepts the same logins
	Accounts      int        `json:"accounts"`
	AccountsAt    *time.Time `json:"accountsAt,omitempty"`
	AccountsError string     `json:"accountsError,omitempty"`
	// Orders: this PC carries out the VPS Admin page's orders (VPS_REMOTE_CONTROL=1); see push_remote.go
	Orders    bool           `json:"orders"`
	LastOrder *CommandResult `json:"lastOrder,omitempty"`
}

type Pusher struct {
	base, token, dataDir string
	usersFile            string // this PC's accounts file, replaced by the VPS's copy
	client               *http.Client

	mu         sync.Mutex
	status     PushStatus
	sentImages map[string]bool // confirmed on the VPS
	doneSig    string          // the folder signature of the last complete round
	lastCheck  time.Time
	usersAt    time.Time     // last accounts pull
	retryAt    time.Time     // after a failure, wait before trying again
	backoff    time.Duration // doubles on each failure in a row, up to pushRecheck

	remote remoteState // probe control through the VPS (push_remote.go)
}

func NewPusher(base, token, dataDir, usersFile string) *Pusher {
	base = strings.TrimRight(base, "/")
	p := &Pusher{base: base, token: token, dataDir: dataDir, usersFile: usersFile, sentImages: map[string]bool{},
		client: &http.Client{Timeout: 2 * time.Minute}}
	p.status.Enabled = true
	if u, err := url.Parse(base); err == nil {
		p.status.Host = u.Host
	}
	return p
}

func (p *Pusher) Status() PushStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Run pushes forever (and, after Remote, reports the Admin state and takes orders).
func (p *Pusher) Run() {
	p.startRemote()
	for {
		p.PullUsers(time.Now())
		more := p.Tick(time.Now())
		if !more {
			time.Sleep(pushEvery)
		}
	}
}

// signature summarises the folder cheaply: a change in any of these means there may be something to send.
func (p *Pusher) signature() string {
	caps := listNames(filepath.Join(p.dataDir, "captures"), captureNameRe.MatchString)
	newest := ""
	if len(caps) > 0 {
		newest = caps[len(caps)-1]
	}
	imgs := listNames(filepath.Join(p.dataDir, "images"), validImageName)
	var logSize int64
	if fi, err := os.Stat(filepath.Join(p.dataDir, "log.jsonl")); err == nil {
		logSize = fi.Size()
	}
	return fmt.Sprintf("%s|%d|%d|%d|%v", newest, len(caps), len(imgs), logSize, fileExists(filepath.Join(p.dataDir, "STOP")))
}

// Tick runs one round if anything changed (or a recheck is due). It returns true when more is ready to send now
// (a backlog bigger than one round), so Run goes again without waiting.
func (p *Pusher) Tick(now time.Time) bool {
	sig := p.signature()
	p.mu.Lock()
	idle := (sig == p.doneSig && now.Sub(p.lastCheck) < pushRecheck) || now.Before(p.retryAt)
	p.mu.Unlock()
	if idle {
		return false
	}
	complete, more, err := p.round()
	p.mu.Lock()
	defer p.mu.Unlock()
	t := now
	p.status.LastTry = &t
	p.lastCheck = now
	if err != nil {
		p.status.Error = err.Error()
		p.backoff = min(max(2*p.backoff, pushEvery), pushRecheck)
		p.retryAt = now.Add(p.backoff)
		return false
	}
	p.status.Error = ""
	p.status.LastOK = &t
	p.backoff, p.retryAt = 0, time.Time{}
	if complete {
		p.doneSig = sig
	}
	return more
}

func (p *Pusher) call(method, path string, body []byte, contentType string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write(body)
		gz.Close()
		rd = &buf
	}
	req, err := http.NewRequest(method, p.base+"/api/sync/"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Content-Type", contentType)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %v", method, strings.SplitN(path, "?", 2)[0], err)
	}
	return res, nil
}

func (p *Pusher) ok(method, path string, body []byte, contentType string) error {
	res, err := p.call(method, path, body, contentType)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		if res.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("the VPS refused the token (VPS_SYNC_TOKEN here must equal SYNC_TOKEN there)")
		}
		return fmt.Errorf("%s %s: HTTP %d %s", method, strings.SplitN(path, "?", 2)[0], res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// round sends what the VPS is missing. complete is false when something had to wait, so the next tick tries again
// even if the folder signature hasn't changed: a file still settling (next tick), or more than one round's worth
// (more: go again at once).
func (p *Pusher) round() (complete, more bool, err error) {
	res, err := p.call("GET", "state", nil, "")
	if err != nil {
		return false, false, err
	}
	var st SyncState
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		if res.StatusCode == http.StatusUnauthorized {
			return false, false, fmt.Errorf("the VPS refused the token (VPS_SYNC_TOKEN here must equal SYNC_TOKEN there)")
		}
		return false, false, fmt.Errorf("state: HTTP %d", res.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&st)
	res.Body.Close()
	if err != nil {
		return false, false, fmt.Errorf("state: %v", err)
	}
	complete = true
	now := time.Now()
	settled := func(file string) bool {
		fi, err := os.Stat(file)
		return err == nil && now.Sub(fi.ModTime()) > pushSettle
	}

	// captures: only names after the VPS's newest, oldest first, stopping at the first that can't go yet
	capDir := filepath.Join(p.dataDir, "captures")
	var todo []string
	for _, n := range listNames(capDir, captureNameRe.MatchString) {
		if n > st.NewestCapture {
			todo = append(todo, n)
		}
	}
	sent := 0
	for _, n := range todo {
		f := filepath.Join(capDir, n)
		if sent >= pushPerRound {
			complete, more = false, true
			break
		}
		if !settled(f) {
			complete = false
			break
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return false, false, err
		}
		if err := p.ok("PUT", "captures/"+url.PathEscape(n), b, "application/json"); err != nil {
			p.setPending(st.Captures+sent, len(todo)-sent)
			return false, false, err
		}
		sent++
		p.mu.Lock()
		p.status.Sent.Captures++
		p.mu.Unlock()
	}
	p.setPending(st.Captures+sent, len(todo)-sent)

	// images: ask the VPS about names it hasn't confirmed yet, then send the missing ones
	imgDir := filepath.Join(p.dataDir, "images")
	var unknown []string
	p.mu.Lock()
	for _, n := range listNames(imgDir, validImageName) {
		if !p.sentImages[n] && settled(filepath.Join(imgDir, n)) {
			unknown = append(unknown, n)
		}
	}
	p.mu.Unlock()
	for len(unknown) > 0 {
		chunk := unknown[:min(len(unknown), pushNamesEach)]
		unknown = unknown[len(chunk):]
		missing, err := p.missingImages(chunk)
		if err != nil {
			return false, false, err
		}
		isMissing := map[string]bool{}
		for _, n := range missing {
			isMissing[n] = true
		}
		for _, n := range chunk {
			if isMissing[n] {
				b, err := os.ReadFile(filepath.Join(imgDir, n))
				if err != nil {
					return false, false, err
				}
				if err := p.ok("PUT", "images/"+url.PathEscape(n), b, "application/octet-stream"); err != nil {
					return false, false, err
				}
				p.mu.Lock()
				p.status.Sent.Images++
				p.mu.Unlock()
			}
			p.mu.Lock()
			p.sentImages[n] = true
			p.mu.Unlock()
		}
	}

	// log.jsonl: whole new lines from the VPS's size on; a shorter log here (a new one) replaces it
	if rest, off, partial, err := readLogFrom(filepath.Join(p.dataDir, "log.jsonl"), st.LogSize); err == nil && len(rest) > 0 {
		if partial {
			complete, more = false, true
		}
		if end := bytes.LastIndexByte(rest, '\n') + 1; end > 0 {
			if err := p.ok("PUT", "log?offset="+strconv.FormatInt(off, 10), rest[:end], "application/x-ndjson"); err != nil {
				return false, false, err
			}
			p.mu.Lock()
			p.status.Sent.LogBytes += int64(end)
			p.mu.Unlock()
		}
	}

	// STOP: mirror whether the probe has halted, so Health on the VPS shows it
	if b, err := os.ReadFile(filepath.Join(p.dataDir, "STOP")); err == nil {
		if !st.Stop {
			if err := p.ok("PUT", "stop", b, "text/plain"); err != nil {
				return false, false, err
			}
		}
	} else if st.Stop {
		if err := p.ok("DELETE", "stop", nil, ""); err != nil {
			return false, false, err
		}
	}
	return complete, more, nil
}

// readLogFrom reads up to pushLogChunk bytes of the log from off (from 0 if the log is now shorter: a new log).
func readLogFrom(path string, off int64) (rest []byte, from int64, more bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	if fi.Size() < off {
		off = 0
	}
	n := fi.Size() - off
	if n > pushLogChunk {
		n, more = pushLogChunk, true
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil, 0, false, err
	}
	return buf, off, more, nil
}

func (p *Pusher) setPending(onVPS, pending int) {
	p.mu.Lock()
	p.status.Captures, p.status.Pending = onVPS, pending
	p.mu.Unlock()
}

func (p *Pusher) missingImages(names []string) ([]string, error) {
	body, _ := json.Marshal(map[string][]string{"names": names})
	res, err := p.call("POST", "missing-images", body, "application/json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("missing-images: HTTP %d", res.StatusCode)
	}
	var out struct {
		Missing []string `json:"missing"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out.Missing, nil
}

// PullUsers copies the VPS's accounts into this PC's accounts file when due (at start, then every pullUsersEach).
// The VPS is where accounts are managed; this PC only mirrors them. If the VPS has none yet, or sends anything
// that doesn't look like valid accounts, this PC keeps its own, so a mistake there can't lock anyone out here.
func (p *Pusher) PullUsers(now time.Time) {
	p.mu.Lock()
	due := p.usersFile != "" && (p.usersAt.IsZero() || now.Sub(p.usersAt) >= pullUsersEach)
	if due {
		p.usersAt = now
	}
	p.mu.Unlock()
	if !due {
		return
	}
	n, err := p.pullUsers()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.status.AccountsError = err.Error()
		return
	}
	t := now
	p.status.Accounts, p.status.AccountsAt, p.status.AccountsError = n, &t, ""
}

func (p *Pusher) pullUsers() (int, error) {
	res, err := p.call("GET", "users", nil, "")
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return 0, fmt.Errorf("the VPS refused the token (VPS_SYNC_TOKEN here must equal SYNC_TOKEN there)")
	}
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("accounts: HTTP %d", res.StatusCode)
	}
	var f usersFile
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&f); err != nil {
		return 0, fmt.Errorf("accounts: %v", err)
	}
	if len(f.Users) == 0 {
		return 0, errors.New("the VPS has no accounts yet; keeping this PC's")
	}
	seen := map[string]bool{}
	for _, u := range f.Users {
		if !userNameRe.MatchString(u.Name) || !validRole(u.Role) || !strings.HasPrefix(u.Hash, "$argon2id$") || seen[u.Name] {
			return 0, errors.New("the VPS sent accounts this PC doesn't accept; keeping this PC's")
		}
		seen[u.Name] = true
	}
	if cur, err := LoadUsers(p.usersFile); err == nil && reflect.DeepEqual(cur, f.Users) {
		return len(f.Users), nil
	}
	if err := SaveUsers(p.usersFile, f.Users); err != nil {
		return 0, fmt.Errorf("saving accounts: %v", err)
	}
	return len(f.Users), nil
}

// PushURLOK refuses a VPS address the token would travel to in clear text (http:// is fine only for this machine).
func PushURLOK(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("VPS_URL %q is not a URL", raw)
	}
	h := u.Hostname()
	if u.Scheme == "https" || (u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "host.docker.internal")) {
		return nil
	}
	return fmt.Errorf("VPS_URL must start with https:// (the token must not travel in clear text)")
}
