package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Admin page: the dashboard still never contacts FANS. It writes small files into the requests folder, which the
// prototype's loop reads before each pass:
//   requests/control.json  { paused, saveImages, showWindow } (null = the probe's own default)
//   requests/RESUME        the exact text of ./STOP, to clear that halt; the loop deletes both
// and it reads what the loop reports about itself in status.json (data folder, read-only here).

type Control struct {
	Paused     bool       `json:"paused"`
	SaveImages *bool      `json:"saveImages"`
	ShowWindow *bool      `json:"showWindow"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// ControlPatch is a partial update from the Admin page: only the fields it sends change.
type ControlPatch struct {
	Paused     *bool `json:"paused"`
	SaveImages *bool `json:"saveImages"`
	ShowWindow *bool `json:"showWindow"`
}

// ProbeStatus is what the loop writes to status.json.
type ProbeStatus struct {
	State      string     `json:"state"` // waiting | passing | paused | stopped | login | exited
	Pid        int        `json:"pid"`
	SaveImages bool       `json:"saveImages"`
	ShowWindow bool       `json:"showWindow"`
	NextPassAt *time.Time `json:"nextPassAt,omitempty"`
	Trigger    string     `json:"trigger,omitempty"`
	Stop       string     `json:"stop,omitempty"`
	Reason     string     `json:"reason,omitempty"` // exited: quit | restart | closed | ended | crashed
	Code       *ProbeCode `json:"code,omitempty"`
	TransAm    *TransAm   `json:"transAm,omitempty"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// ProbeCode: when the running loop's probe.mjs was last changed, and whether the file has changed since it started.
type ProbeCode struct {
	LoadedAt time.Time `json:"loadedAt"`
	Changed  bool      `json:"changed"`
}

// TransAm is the probe's fast mode (a pass about every 30 s for 5 min): the running one, or how the last one ended.
type TransAm struct {
	Active    bool       `json:"active"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	Until     *time.Time `json:"until,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Passes    int        `json:"passes"`
}

// LauncherExit is how the loop the launcher watched last ended.
type LauncherExit struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// Launcher is what launcher.mjs (the supervisor on the PC) writes to launcher.json.
type Launcher struct {
	RetryAt   *time.Time    `json:"retryAt,omitempty"` // it starts the loop again then, after a crash
	Crashes   int           `json:"crashes"`
	LastExit  *LauncherExit `json:"lastExit,omitempty"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// Budget is the probe's FANS request budget right now (from the log), for the Trans-Am button.
type Budget struct {
	HourCap  int `json:"hourCap"`
	DayCap   int `json:"dayCap"`
	LastHour int `json:"lastHour"`
	LastDay  int `json:"lastDay"`
}

type Admin struct {
	Upload        *PushStatus  `json:"upload,omitempty"` // uploads to the VPS dashboard (push.go), when set up
	Remote        *RemoteInfo  `json:"remote,omitempty"` // on the VPS: the home PC that runs the probe (remote.go)
	Enabled       bool         `json:"enabled"`          // false when no writable requests folder is mounted
	Control       Control      `json:"control"`
	Stop          string       `json:"stop,omitempty"` // contents of ./STOP; empty when not halted
	ResumePending bool         `json:"resumePending"`
	Probe         *ProbeStatus `json:"probe,omitempty"`
	// Alive: status.json was refreshed recently. The loop heartbeats every 30 s while waiting, but a pass (app boot,
	// paging, pictures) can run for minutes without one.
	Alive bool `json:"alive"`
	// LauncherAlive: launcher.mjs is running on the PC (heartbeat in launcher.json), so the Start button works.
	LauncherAlive bool `json:"launcherAlive"`
	StartPending  bool `json:"startPending"`
	// LastPassAt: when the last pass (or budget skip) was logged, at its end: where the wait for NextPassAt began.
	LastPassAt *time.Time `json:"lastPassAt,omitempty"`
	// Log in to FANS: requests/LOGIN asks the loop to open FANS on-screen; login.json is how the last one went.
	LoginPending bool         `json:"loginPending"`
	Login        *LoginResult `json:"login,omitempty"`
	// LoginExpiresAt: the FANS login cookie's fixed expiry (3 days after logging in), from the log.
	LoginExpiresAt *time.Time `json:"loginExpiresAt,omitempty"`
	// Loop commands waiting for the loop to pick up: quit | restart | transam | transam-cancel
	LoopPending []string  `json:"loopPending"`
	Launcher    *Launcher `json:"launcher,omitempty"`
	Budget      Budget    `json:"budget"`
}

// Loop commands from the Admin page, each a file in requests/ that the loop acts on between passes:
// quit ends the loop (it stays off), restart ends it for launcher.mjs to start again (new code), transam starts the
// fast mode and transam-cancel ends it. The file's text is what the loop reads.
var loopCommands = map[string]struct{ file, text string }{
	"quit":           {"QUIT", "quit"},
	"restart":        {"RESTART", "restart"},
	"transam":        {"TRANSAM", "start"},
	"transam-cancel": {"TRANSAM", "cancel"},
}

var ErrUnknownLoopCommand = errors.New("unknown loop command")

// RequestLoop leaves a loop command for the probe. A Trans-Am start and cancel share one file: the newest wins.
func RequestLoop(reqDir, cmd string) error {
	c, ok := loopCommands[cmd]
	if !ok {
		return ErrUnknownLoopCommand
	}
	if reqDir == "" {
		return errors.New("no requests folder configured")
	}
	return writeAtomic(filepath.Join(reqDir, c.file), []byte(c.text+"\n"))
}

// pendingLoop lists the loop commands whose files are still waiting in requests/.
func pendingLoop(reqDir string) []string {
	out := []string{}
	if reqDir == "" {
		return out
	}
	for _, cmd := range []string{"quit", "restart", "transam", "transam-cancel"} {
		c := loopCommands[cmd]
		if b, err := os.ReadFile(filepath.Join(reqDir, c.file)); err == nil && strings.TrimSpace(string(b)) == c.text {
			out = append(out, cmd)
		}
	}
	return out
}

type LoginResult struct {
	State     string     `json:"state"` // waiting | ok | gave up
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

func loginFile(reqDir string) string { return filepath.Join(reqDir, "LOGIN") }

// RequestLogin asks the probe loop to open FANS on-screen for a login, the next time it is waiting (within seconds
// while halted, paused or between passes; never during a pass).
func RequestLogin(reqDir string, now time.Time) error {
	if reqDir == "" {
		return errors.New("no requests folder configured")
	}
	return writeAtomic(loginFile(reqDir), []byte(now.UTC().Format(time.RFC3339Nano)+"\n"))
}

// lastPassAt finds the newest "pass" or "skip" entry near the end of log.jsonl (entries are a few KB at most).
func lastPassAt(dataDir string) *time.Time {
	f, err := os.Open(filepath.Join(dataDir, "log.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	const tail = 256 << 10
	if st, err := f.Stat(); err == nil && st.Size() > tail {
		f.Seek(st.Size()-tail, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			At   time.Time `json:"at"`
			Kind string    `json:"kind"`
		}
		if json.Unmarshal([]byte(lines[i]), &e) == nil && (e.Kind == "pass" || e.Kind == "skip") && !e.At.IsZero() {
			return &e.At
		}
	}
	return nil
}

const (
	heartbeatMaxAge = 2 * time.Minute
	passMaxAge      = 15 * time.Minute
)

func controlFile(reqDir string) string { return filepath.Join(reqDir, "control.json") }
func resumeFile(reqDir string) string  { return filepath.Join(reqDir, "RESUME") }

func readStop(dataDir string) string {
	b, err := os.ReadFile(filepath.Join(dataDir, "STOP"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func ReadControl(reqDir string) Control {
	var c Control
	if reqDir == "" {
		return c
	}
	if b, err := os.ReadFile(controlFile(reqDir)); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func ReadAdmin(dataDir, reqDir string, now time.Time) Admin {
	a := Admin{Enabled: dirWritable(reqDir), Control: ReadControl(reqDir), Stop: readStop(dataDir)}
	if reqDir != "" {
		_, err := os.Stat(resumeFile(reqDir))
		a.ResumePending = err == nil && a.Stop != ""
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, "status.json")); err == nil {
		var p ProbeStatus
		if json.Unmarshal(b, &p) == nil && !p.UpdatedAt.IsZero() {
			a.Probe = &p
			max := heartbeatMaxAge
			if p.State == "passing" {
				max = passMaxAge
			}
			a.Alive = p.State != "exited" && now.Sub(p.UpdatedAt) < max
		}
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, "launcher.json")); err == nil {
		var l Launcher
		if json.Unmarshal(b, &l) == nil {
			a.LauncherAlive = now.Sub(l.UpdatedAt) < heartbeatMaxAge
			a.Launcher = &l
		}
	}
	a.LoopPending = pendingLoop(reqDir)
	if reqDir != "" {
		_, err := os.Stat(startFile(reqDir))
		a.StartPending = err == nil
	}
	a.LastPassAt = lastPassAt(dataDir)
	if reqDir != "" {
		_, err := os.Stat(loginFile(reqDir))
		a.LoginPending = err == nil
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, "login.json")); err == nil {
		var l LoginResult
		if json.Unmarshal(b, &l) == nil && l.State != "" {
			a.Login = &l
		}
	}
	h := ReadHealth(dataDir, now)
	a.LoginExpiresAt = h.RefreshCookieExp
	a.Budget = Budget{HourCap: h.HourCap, DayCap: h.DayCap, LastHour: h.RequestsLastHour, LastDay: h.RequestsLast24h}
	return a
}

func startFile(reqDir string) string { return filepath.Join(reqDir, "START") }

// RequestStart asks launcher.mjs to start the probe loop. It does nothing if a loop is already running.
func RequestStart(reqDir string, now time.Time) error {
	if reqDir == "" {
		return errors.New("no requests folder configured")
	}
	return writeAtomic(startFile(reqDir), []byte(now.UTC().Format(time.RFC3339Nano)+"\n"))
}

// writeAtomic writes a file in one step (temp file, then rename), so the loop never reads half of it.
func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func UpdateControl(reqDir string, p ControlPatch, now time.Time) (Control, error) {
	if reqDir == "" {
		return Control{}, errors.New("no requests folder configured")
	}
	c := ReadControl(reqDir)
	if p.Paused != nil {
		c.Paused = *p.Paused
	}
	if p.SaveImages != nil {
		c.SaveImages = p.SaveImages
	}
	if p.ShowWindow != nil {
		c.ShowWindow = p.ShowWindow
	}
	t := now.UTC()
	c.UpdatedAt = &t
	b, _ := json.MarshalIndent(c, "", " ")
	return c, writeAtomic(controlFile(reqDir), b)
}

var ErrStopChanged = errors.New("STOP changed or is gone: reload and check the reason again")

// RequestResume asks the loop to clear ./STOP. stop must be the halt reason the user saw and confirmed; if STOP
// now says something else (a newer halt), nothing is written.
func RequestResume(dataDir, reqDir, stop string) error {
	if reqDir == "" {
		return errors.New("no requests folder configured")
	}
	cur := readStop(dataDir)
	if cur == "" || cur != strings.TrimSpace(stop) {
		return ErrStopChanged
	}
	return writeAtomic(resumeFile(reqDir), []byte(cur+"\n"))
}
