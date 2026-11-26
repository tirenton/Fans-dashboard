package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func adminDirs(t *testing.T) (data, req string) {
	t.Helper()
	data = t.TempDir()
	req = filepath.Join(data, "requests")
	if err := os.Mkdir(req, 0o755); err != nil {
		t.Fatal(err)
	}
	return data, req
}

func TestUpdateControlMergesFields(t *testing.T) {
	_, req := adminDirs(t)
	yes, no := true, false
	if _, err := UpdateControl(req, ControlPatch{Paused: &yes, SaveImages: &yes}, time.Now()); err != nil {
		t.Fatal(err)
	}
	c, err := UpdateControl(req, ControlPatch{SaveImages: &no}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !c.Paused || c.SaveImages == nil || *c.SaveImages || c.ShowWindow != nil {
		t.Fatalf("bad merge %+v", c)
	}
}

func TestResumeOnlyForTheHaltTheUserSaw(t *testing.T) {
	data, req := adminDirs(t)
	if err := RequestResume(data, req, "anything"); err != ErrStopChanged {
		t.Fatalf("no STOP: want ErrStopChanged, got %v", err)
	}
	os.WriteFile(filepath.Join(data, "STOP"), []byte("2026-11-13T06:28:48Z 401 from /account/auth/refresh\n"), 0o644)
	if err := RequestResume(data, req, "2026-11-13T05:00:00Z an older halt"); err != ErrStopChanged {
		t.Fatalf("stale reason: want ErrStopChanged, got %v", err)
	}
	if err := RequestResume(data, req, "2026-11-13T06:28:48Z 401 from /account/auth/refresh"); err != nil {
		t.Fatal(err)
	}
	if a := ReadAdmin(data, req, time.Now()); !a.ResumePending || a.Stop == "" {
		t.Fatalf("want resume pending, got %+v", a)
	}
}

func TestAliveFromHeartbeat(t *testing.T) {
	data, req := adminDirs(t)
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	write := func(state string, age time.Duration) {
		os.WriteFile(filepath.Join(data, "status.json"),
			[]byte(`{"state":"`+state+`","updatedAt":"`+now.Add(-age).Format(time.RFC3339)+`"}`), 0o644)
	}
	write("waiting", time.Minute)
	if !ReadAdmin(data, req, now).Alive {
		t.Fatal("recent heartbeat: want alive")
	}
	write("waiting", 5*time.Minute)
	if ReadAdmin(data, req, now).Alive {
		t.Fatal("old heartbeat while waiting: want not alive")
	}
	write("passing", 5*time.Minute)
	if !ReadAdmin(data, req, now).Alive {
		t.Fatal("a pass may run minutes without a heartbeat: want alive")
	}
}

func TestStartAndLauncherHeartbeat(t *testing.T) {
	data, req := adminDirs(t)
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	if a := ReadAdmin(data, req, now); a.LauncherAlive || a.StartPending {
		t.Fatalf("nothing yet: %+v", a)
	}
	os.WriteFile(filepath.Join(data, "launcher.json"), []byte(`{"updatedAt":"`+now.Add(-time.Minute).Format(time.RFC3339)+`"}`), 0o644)
	if err := RequestStart(req, now); err != nil {
		t.Fatal(err)
	}
	if a := ReadAdmin(data, req, now); !a.LauncherAlive || !a.StartPending {
		t.Fatalf("want launcher alive and start pending: %+v", a)
	}
	os.WriteFile(filepath.Join(data, "status.json"), []byte(`{"state":"exited","updatedAt":"`+now.Format(time.RFC3339)+`"}`), 0o644)
	if ReadAdmin(data, req, now).Alive {
		t.Fatal("an exited loop is not alive")
	}
}

func TestLastPassAtFromLogTail(t *testing.T) {
	data, req := adminDirs(t)
	log := `{"at":"2026-11-13T20:49:51Z","kind":"pass"}
{"at":"2026-11-13T21:00:55Z","kind":"skip"}
{"at":"2026-11-13T21:01:00Z","kind":"resume"}
`
	os.WriteFile(filepath.Join(data, "log.jsonl"), []byte(log), 0o644)
	a := ReadAdmin(data, req, time.Now())
	if a.LastPassAt == nil || !a.LastPassAt.Equal(time.Date(2026, 11, 13, 21, 0, 55, 0, time.UTC)) {
		t.Fatalf("want the skip at 21:00:55, got %v", a.LastPassAt)
	}
}

func TestLoginRequestResultAndExpiry(t *testing.T) {
	data, req := adminDirs(t)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if err := RequestLogin(req, now); err != nil {
		t.Fatal(err)
	}
	log := `{"at":"2026-11-15T09:34:20Z","kind":"pass","type":"light","cookiesAfter":[{"name":"refreshToken:web","expires":"2026-11-16T09:23:00Z"}]}
{"at":"2026-11-16T11:30:00Z","kind":"login","by":"dashboard","cookiesAfter":[{"name":"refreshToken:web","expires":"2026-11-19T11:30:00Z"}]}
`
	os.WriteFile(filepath.Join(data, "log.jsonl"), []byte(log), 0o644)
	os.WriteFile(filepath.Join(data, "login.json"), []byte(`{"state":"ok","by":"dashboard","startedAt":"2026-11-16T11:29:00Z","endedAt":"2026-11-16T11:30:00Z"}`), 0o644)
	a := ReadAdmin(data, req, now)
	if !a.LoginPending || a.Login == nil || a.Login.State != "ok" {
		t.Fatalf("want login pending and last result ok: %+v", a)
	}
	if a.LoginExpiresAt == nil || !a.LoginExpiresAt.Equal(time.Date(2026, 11, 19, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("want the expiry from the login entry, got %v", a.LoginExpiresAt)
	}
	if a.LastPassAt == nil || !a.LastPassAt.Equal(time.Date(2026, 11, 15, 9, 34, 20, 0, time.UTC)) {
		t.Fatalf("a login is not a pass: got %v", a.LastPassAt)
	}
}

func TestLoopCommands(t *testing.T) {
	data, req := adminDirs(t)
	if err := RequestLoop(req, "explode"); err != ErrUnknownLoopCommand {
		t.Fatalf("want ErrUnknownLoopCommand, got %v", err)
	}
	for _, c := range []string{"restart", "transam"} {
		if err := RequestLoop(req, c); err != nil {
			t.Fatal(err)
		}
	}
	if got := ReadAdmin(data, req, time.Now()).LoopPending; len(got) != 2 || got[0] != "restart" || got[1] != "transam" {
		t.Fatalf("pending: %v", got)
	}
	// start and cancel share requests/TRANSAM: the newest wins, and its text is what the probe reads
	RequestLoop(req, "transam-cancel")
	if b, _ := os.ReadFile(filepath.Join(req, "TRANSAM")); string(b) != "cancel\n" {
		t.Fatalf("TRANSAM holds %q", b)
	}
	if got := ReadAdmin(data, req, time.Now()).LoopPending; len(got) != 2 || got[1] != "transam-cancel" {
		t.Fatalf("pending after cancel: %v", got)
	}
}

func TestStatusAndLauncherDetails(t *testing.T) {
	data, req := adminDirs(t)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	os.WriteFile(filepath.Join(data, "status.json"), []byte(`{"state":"waiting","updatedAt":"2026-11-27T11:59:50Z",
		"code":{"loadedAt":"2026-11-27T10:00:00Z","changed":true},
		"transAm":{"active":true,"startedAt":"2026-11-27T11:58:00Z","until":"2026-11-27T12:03:00Z","passes":3}}`), 0o644)
	os.WriteFile(filepath.Join(data, "launcher.json"), []byte(`{"pid":1,"updatedAt":"2026-11-27T11:59:40Z","crashes":2,
		"retryAt":"2026-11-27T12:01:00Z","lastExit":{"at":"2026-11-27T11:59:00Z","reason":"crashed"}}`), 0o644)
	a := ReadAdmin(data, req, now)
	if a.Probe == nil || a.Probe.Code == nil || !a.Probe.Code.Changed || a.Probe.TransAm == nil || !a.Probe.TransAm.Active || a.Probe.TransAm.Passes != 3 {
		t.Fatalf("status details: %+v", a.Probe)
	}
	if !a.LauncherAlive || a.Launcher == nil || a.Launcher.Crashes != 2 || a.Launcher.RetryAt == nil || a.Launcher.LastExit.Reason != "crashed" {
		t.Fatalf("launcher details: %+v", a.Launcher)
	}
	if a.Budget.HourCap == 0 {
		t.Fatalf("budget caps missing: %+v", a.Budget)
	}
}
