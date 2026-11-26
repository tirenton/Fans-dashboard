package main

// Probe control through the VPS (the VPS side; SYNC_ENABLED=1). The probe runs on the home PC. The PC's API
// (push.go) keeps a snapshot of its Admin state here (PUT /api/sync/admin) and collects the VPS Admin page's
// orders (GET /api/sync/commands, held open until one arrives, then POST /api/sync/commands/ack). It carries them
// out exactly as the PC's own Admin page would, by writing the probe's request files, so the probe's own safety
// rules (request budget, STOP) still decide everything. The PC only takes orders when VPS_REMOTE_CONTROL=1 there.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Command is one order from the VPS Admin page.
type Command struct {
	ID     string        `json:"id"`
	Action string        `json:"action"` // control | start | login | resume | pull | loop
	Patch  *ControlPatch `json:"patch,omitempty"`
	Stop   string        `json:"stop,omitempty"` // resume: the halt reason the admin saw and confirmed
	Loop   string        `json:"loop,omitempty"` // loop: quit | restart | transam | transam-cancel
	At     time.Time     `json:"at"`
}

// CommandResult is how the PC carried an order out ("" Error = done).
type CommandResult struct {
	ID     string    `json:"id"`
	Action string    `json:"action"`
	Error  string    `json:"error,omitempty"`
	At     time.Time `json:"at"`
}

// Snapshot is what the PC reports about the probe.
type Snapshot struct {
	Admin         Admin      `json:"admin"`
	Pull          PullStatus `json:"pull"`
	RemoteControl bool       `json:"remoteControl"` // the PC takes orders from here (VPS_REMOTE_CONTROL=1 there)
}

// RemoteInfo is shown on the VPS Admin page.
type RemoteInfo struct {
	Online  bool           `json:"online"`           // the PC reported in the last remoteOnline
	SeenAt  *time.Time     `json:"seenAt,omitempty"` // when (this server's clock)
	Accepts bool           `json:"accepts"`          // the PC takes orders from here
	Queued  int            `json:"queued"`           // orders not picked up yet
	Last    *CommandResult `json:"last,omitempty"`   // the last order the PC carried out or refused
}

const (
	remoteOnline   = 90 * time.Second
	remoteMaxQueue = 20
)

var ErrPCOffline = errors.New("your PC is offline, or doesn't take orders from this server (VPS_REMOTE_CONTROL on the PC)")

type Remote struct {
	mu    sync.Mutex
	snap  *Snapshot
	seen  time.Time
	queue []Command
	last  *CommandResult
	wake  chan struct{} // closed (and replaced) when an order arrives
	seq   int
}

func NewRemote() *Remote { return &Remote{wake: make(chan struct{})} }

func (r *Remote) SetSnapshot(s Snapshot, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap, r.seen = &s, now
}

func (r *Remote) onlineLocked(now time.Time) bool {
	return r.snap != nil && now.Sub(r.seen) < remoteOnline
}

// Admin is the PC's last Admin state, with the orders still waiting here shown as pending.
func (r *Remote) Admin(now time.Time) Admin {
	r.mu.Lock()
	defer r.mu.Unlock()
	var a Admin
	if r.snap != nil {
		a = r.snap.Admin
	}
	info := RemoteInfo{Online: r.onlineLocked(now), Accepts: r.snap != nil && r.snap.RemoteControl, Queued: len(r.queue), Last: r.last}
	if r.snap != nil {
		t := r.seen
		info.SeenAt = &t
	}
	a.Remote = &info
	a.Enabled = info.Online && info.Accepts
	if !info.Online { // an old snapshot can't vouch for the probe or the launcher
		a.Alive, a.LauncherAlive = false, false
	}
	for _, c := range r.queue {
		switch c.Action {
		case "start":
			a.StartPending = true
		case "login":
			a.LoginPending = true
		case "resume":
			a.ResumePending = true
		case "control":
			applyPatch(&a.Control, c.Patch)
		case "loop":
			a.LoopPending = addLoopPending(a.LoopPending, c.Loop)
		}
	}
	return a
}

// addLoopPending marks a loop command as pending; a Trans-Am start and cancel replace each other, as their file does.
func addLoopPending(p []string, cmd string) []string {
	out := []string{}
	for _, x := range p {
		if x != cmd && !(strings.HasPrefix(x, "transam") && strings.HasPrefix(cmd, "transam")) {
			out = append(out, x)
		}
	}
	return append(out, cmd)
}

func applyPatch(c *Control, p *ControlPatch) {
	if p == nil {
		return
	}
	if p.Paused != nil {
		c.Paused = *p.Paused
	}
	if p.SaveImages != nil {
		c.SaveImages = p.SaveImages
	}
	if p.ShowWindow != nil {
		c.ShowWindow = p.ShowWindow
	}
}

// Pull is the PC's Pull now state, pending also while an order for one waits here.
func (r *Remote) Pull(now time.Time) PullStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	var st PullStatus
	if r.snap != nil {
		st = r.snap.Pull
	}
	st.Enabled = r.onlineLocked(now) && r.snap.RemoteControl
	for _, c := range r.queue {
		if c.Action == "pull" && !st.Pending {
			t := c.At
			st.Pending, st.RequestedAt = true, &t
		}
	}
	return st
}

// Enqueue adds an order for the PC. A second start, login, resume or pull while one waits is the same order.
func (r *Remote) Enqueue(c Command, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.onlineLocked(now) || !r.snap.RemoteControl {
		return ErrPCOffline
	}
	switch c.Action {
	case "control":
		if c.Patch == nil {
			return errors.New("empty settings change")
		}
	case "resume":
		if r.snap.Admin.Stop == "" || strings.TrimSpace(c.Stop) != r.snap.Admin.Stop {
			return ErrStopChanged
		}
	case "loop":
		if _, ok := loopCommands[c.Loop]; !ok {
			return ErrUnknownLoopCommand
		}
	case "start", "login", "pull":
	default:
		return fmt.Errorf("unknown order %q", c.Action)
	}
	if c.Action != "control" {
		for _, q := range r.queue {
			if q.Action == c.Action && q.Loop == c.Loop {
				return nil
			}
		}
	}
	if len(r.queue) >= remoteMaxQueue {
		return errors.New("too many orders waiting for your PC")
	}
	r.seq++
	c.ID, c.At = fmt.Sprintf("%d-%d", now.UnixNano(), r.seq), now
	r.queue = append(r.queue, c)
	close(r.wake)
	r.wake = make(chan struct{})
	return nil
}

// Wait returns the waiting orders, holding on up to d for one when there are none.
func (r *Remote) Wait(ctx context.Context, d time.Duration) []Command {
	r.mu.Lock()
	if len(r.queue) == 0 {
		w := r.wake
		r.mu.Unlock()
		select {
		case <-w:
		case <-time.After(d):
		case <-ctx.Done():
		}
		r.mu.Lock()
	}
	defer r.mu.Unlock()
	return append([]Command{}, r.queue...)
}

// Ack removes the orders the PC carried out and remembers the last result for the Admin page.
func (r *Remote) Ack(results []CommandResult, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, res := range results {
		for i, c := range r.queue {
			if c.ID == res.ID {
				r.queue = append(r.queue[:i], r.queue[i+1:]...)
				res.Action, res.At = c.Action, now
				last := res
				r.last = &last
				break
			}
		}
	}
}
