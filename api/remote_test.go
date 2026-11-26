package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteQueue(t *testing.T) {
	r := NewRemote()
	now := time.Now()
	if err := r.Enqueue(Command{Action: "start"}, now); err != ErrPCOffline {
		t.Fatalf("no PC yet: want ErrPCOffline, got %v", err)
	}
	r.SetSnapshot(Snapshot{Admin: Admin{Alive: true, Stop: "halted: 401"}, RemoteControl: false}, now)
	if err := r.Enqueue(Command{Action: "start"}, now); err != ErrPCOffline {
		t.Fatalf("PC doesn't take orders: want ErrPCOffline, got %v", err)
	}
	if a := r.Admin(now); a.Enabled || !a.Remote.Online || a.Remote.Accepts {
		t.Fatalf("read-only PC: %+v", a.Remote)
	}

	r.SetSnapshot(Snapshot{Admin: Admin{Alive: true, LauncherAlive: true, Stop: "halted: 401"}, RemoteControl: true}, now)
	paused := true
	for _, c := range []Command{{Action: "start"}, {Action: "start"}, {Action: "control", Patch: &ControlPatch{Paused: &paused}}, {Action: "pull"}} {
		if err := r.Enqueue(c, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Enqueue(Command{Action: "resume", Stop: "an older halt"}, now); err != ErrStopChanged {
		t.Fatalf("resume must name the current halt: %v", err)
	}
	if err := r.Enqueue(Command{Action: "resume", Stop: "halted: 401\n"}, now); err != nil {
		t.Fatal(err)
	}
	if err := r.Enqueue(Command{Action: "reboot"}, now); err == nil {
		t.Fatal("unknown orders must be refused")
	}
	a := r.Admin(now)
	if !a.Enabled || !a.StartPending || !a.ResumePending || !a.Control.Paused || a.Remote.Queued != 4 || !r.Pull(now).Pending {
		t.Fatalf("queued orders show as pending (a second start is the same order): %+v %+v", a, a.Remote)
	}
	if a := r.Admin(now.Add(2 * time.Minute)); a.Enabled || a.Alive || a.LauncherAlive || a.Remote.Online {
		t.Fatalf("an old snapshot means the PC is offline: %+v", a)
	}

	got := r.Wait(context.Background(), time.Second)
	r.Ack([]CommandResult{{ID: got[0].ID}, {ID: got[1].ID, Error: "boom"}}, now)
	if a := r.Admin(now); a.Remote.Queued != 2 || a.Remote.Last.Error != "boom" || a.Remote.Last.Action != "control" {
		t.Fatalf("ack: %+v", a.Remote)
	}

	empty := NewRemote()
	empty.SetSnapshot(Snapshot{RemoteControl: true}, time.Now())
	done := make(chan []Command)
	go func() { done <- empty.Wait(context.Background(), 5*time.Second) }()
	time.Sleep(50 * time.Millisecond)
	empty.Enqueue(Command{Action: "login"}, time.Now())
	select {
	case cs := <-done:
		if len(cs) != 1 || cs[0].Action != "login" {
			t.Fatalf("woke with %+v", cs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a waiting PC must get a new order at once")
	}
}

// End to end: the VPS Admin page queues orders, the PC collects them, writes the probe's request files, and the
// VPS shows how they went.
func TestOrdersReachTheProbe(t *testing.T) {
	vps, pc := t.TempDir(), t.TempDir()
	req := filepath.Join(pc, "requests")
	os.MkdirAll(req, 0o755)
	os.WriteFile(filepath.Join(pc, "STOP"), []byte("2026-11-20 401 from refresh\n"), 0o644)
	remote := NewRemote()
	mux := http.NewServeMux()
	registerSync(mux, vps, filepath.Join(vps, "users.json"), NewStore(), remote, func() {})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewPusher(srv.URL, "t", pc, "")
	p.Remote(req, func() Admin { return ReadAdmin(pc, req, time.Now()) }, true)
	if err := p.SendSnapshot(time.Now()); err != nil {
		t.Fatal(err)
	}
	if a := remote.Admin(time.Now()); !a.Enabled || a.Stop != "2026-11-20 401 from refresh" {
		t.Fatalf("the VPS should see the PC's state: %+v", a)
	}
	now := time.Now()
	paused := true
	for _, c := range []Command{
		{Action: "control", Patch: &ControlPatch{Paused: &paused}}, {Action: "start"}, {Action: "login"}, {Action: "pull"},
		{Action: "resume", Stop: "2026-11-20 401 from refresh"}, {Action: "loop", Loop: "restart"}, {Action: "loop", Loop: "transam"},
	} {
		if err := remote.Enqueue(c, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := remote.Enqueue(Command{Action: "loop", Loop: "self-destruct"}, now); err != ErrUnknownLoopCommand {
		t.Fatalf("unknown loop command: want ErrUnknownLoopCommand, got %v", err)
	}
	if a := remote.Admin(now); len(a.LoopPending) != 2 {
		t.Fatalf("queued loop commands show as pending on the VPS: %v", a.LoopPending)
	}
	if err := p.PollCommands(1); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"control.json", "START", "LOGIN", "PULL", "RESUME", "RESTART", "TRANSAM"} {
		if !fileExists(filepath.Join(req, f)) {
			t.Fatalf("requests/%s should have been written", f)
		}
	}
	if c := ReadControl(req); !c.Paused {
		t.Fatal("the pause should reach control.json")
	}
	a := remote.Admin(time.Now())
	if a.Remote.Queued != 0 || a.Remote.Last == nil || a.Remote.Last.Error != "" || p.Status().LastOrder == nil {
		t.Fatalf("all carried out and reported: %+v %+v", a.Remote, p.Status())
	}

	// a resume for a halt that has since changed is refused on the PC too
	os.WriteFile(filepath.Join(pc, "STOP"), []byte("a newer halt\n"), 0o644)
	os.Remove(filepath.Join(req, "RESUME"))
	remote.Enqueue(Command{Action: "resume", Stop: "2026-11-20 401 from refresh"}, time.Now()) // the VPS still has the old snapshot
	p.PollCommands(1)
	if fileExists(filepath.Join(req, "RESUME")) || !strings.Contains(remote.Admin(time.Now()).Remote.Last.Error, "STOP changed") {
		t.Fatalf("stale resume must not clear a newer halt: %+v", remote.Admin(time.Now()).Remote.Last)
	}
}
