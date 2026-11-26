package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A PC folder pushed to a fake VPS (the real sync routes behind a token check), counting the requests.
func TestPusher(t *testing.T) {
	pc, vps := t.TempDir(), t.TempDir()
	store := NewStore()
	mux := http.NewServeMux()
	registerSync(mux, vps, filepath.Join(vps, "users.json"), store, NewRemote(), func() { store.IngestDir(filepath.Join(vps, "captures")) })
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer right-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()

	old := time.Now().Add(-time.Hour)
	put := func(rel, body string, mtime time.Time) {
		f := filepath.Join(pc, rel)
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte(body), 0o644)
		os.Chtimes(f, mtime, mtime)
	}
	capture := `{"capturedAt":"2026-11-20T10:00:00Z","feed":{"posts":[]}}`
	put("captures/2026-11-20T10-00-00-000Z-light.json", capture, old)
	put("captures/2026-11-20T10-10-00-000Z-light.json", capture, old)
	put("images/a1.jpg", "IMG1", old)
	put("images/b2.jpg.missing", "404", old)
	put("log.jsonl", "{\"a\":1}\n{\"b\":2}\n{\"partial", old)

	p := NewPusher(srv.URL+"/", "right-token", pc, "")
	now := time.Now()
	if more := p.Tick(now); more {
		t.Fatal("first round should finish")
	}
	st := p.Status()
	if st.Error != "" || st.Sent.Captures != 2 || st.Sent.Images != 1 || st.Sent.LogBytes != 16 || st.Captures != 2 || st.Pending != 0 {
		t.Fatalf("first round: %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(vps, "log.jsonl")); string(b) != "{\"a\":1}\n{\"b\":2}\n" {
		t.Fatalf("log on the VPS: %q (only whole lines)", b)
	}

	calls.Store(0)
	p.Tick(now.Add(30 * time.Second))
	if n := calls.Load(); n != 0 {
		t.Fatalf("nothing changed: want no requests, got %d", n)
	}
	p.Tick(now.Add(11 * time.Minute))
	if n := calls.Load(); n != 1 {
		t.Fatalf("recheck after 10 min: want just the state call, got %d", n)
	}

	calls.Store(0)
	put("captures/2026-11-20T10-20-00-000Z-light.json", capture, old)
	p.Tick(now.Add(12 * time.Minute))
	if n := calls.Load(); n != 2 || p.Status().Captures != 3 {
		t.Fatalf("one new capture: want state + 1 upload, got %d calls, %+v", n, p.Status())
	}

	put("captures/2026-11-20T10-30-00-000Z-light.json", capture, time.Now()) // still being written
	if more := p.Tick(now.Add(13 * time.Minute)); more || p.Status().Pending != 1 {
		t.Fatalf("a just-written capture waits: %+v", p.Status())
	}
	calls.Store(0)
	p.Tick(now.Add(13*time.Minute + 30*time.Second))
	if calls.Load() == 0 {
		t.Fatal("an unfinished round must be retried even though the folder looks the same")
	}

	put("STOP", "2026-11-20 halted\n", old)
	p.Tick(now.Add(14 * time.Minute))
	if !fileExists(filepath.Join(vps, "STOP")) {
		t.Fatal("STOP should be mirrored")
	}
	os.Remove(filepath.Join(pc, "STOP"))
	p.Tick(now.Add(15 * time.Minute))
	if fileExists(filepath.Join(vps, "STOP")) {
		t.Fatal("STOP should be cleared")
	}

	bad := NewPusher(srv.URL, "wrong-token", pc, "")
	bad.Tick(now)
	if e := bad.Status().Error; !strings.Contains(e, "refused the token") {
		t.Fatalf("wrong token: %q", e)
	}
	calls.Store(0)
	bad.Tick(now.Add(5 * time.Second))
	if calls.Load() != 0 {
		t.Fatal("after a failure, wait before trying again")
	}
	bad.Tick(now.Add(25 * time.Second))
	bad.Tick(now.Add(50 * time.Second)) // second failure in a row: the wait doubles to 40 s
	if n := calls.Load(); n != 1 {
		t.Fatalf("want one retry after 20 s, then a longer wait; got %d calls", n)
	}
}

func TestPushURLOK(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://203-0-113-10.sslip.io": true, "http://localhost:3100": true,
		"http://203.0.113.10": false, "ftp://x": false, "nonsense": false,
	} {
		if (PushURLOK(raw) == nil) != ok {
			t.Fatalf("%s: want ok=%v", raw, ok)
		}
	}
}

// The PC mirrors the VPS's accounts, but never takes an empty or malformed list.
func TestPullUsers(t *testing.T) {
	vps, pc := t.TempDir(), t.TempDir()
	vpsUsers, pcUsers := filepath.Join(vps, "users.json"), filepath.Join(pc, "users.json")
	mux := http.NewServeMux()
	registerSync(mux, vps, vpsUsers, NewStore(), NewRemote(), func() {})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	pw := testPassword(t)
	h, _ := HashPassword(pw)
	SaveUsers(pcUsers, []User{{Name: "local", Role: RoleAdmin, Hash: h}})

	p := NewPusher(srv.URL, "t", pc, pcUsers)
	now := time.Now()
	p.PullUsers(now)
	if st := p.Status(); !strings.Contains(st.AccountsError, "no accounts yet") {
		t.Fatalf("empty VPS: want a note, got %+v", st)
	}
	if u, _ := LoadUsers(pcUsers); len(u) != 1 || u[0].Name != "local" {
		t.Fatalf("empty VPS must not wipe this PC's accounts: %+v", u)
	}

	SaveUsers(vpsUsers, []User{
		{Name: "owner", Role: RoleAdmin, Hash: h, Email: "owner@example.com"},
		{Name: "fan", Role: RoleUser, Hash: h, Email: "fan@example.com"},
	})
	p.PullUsers(now.Add(time.Minute))
	if u, _ := LoadUsers(pcUsers); len(u) != 1 || u[0].Name != "local" {
		t.Fatal("not due yet: nothing should change")
	}
	// the PC gets only the admins, and never anyone's email
	p.PullUsers(now.Add(pullUsersEach))
	u, _ := LoadUsers(pcUsers)
	if len(u) != 1 || u[0].Name != "owner" || u[0].Email != "" || p.Status().Accounts != 1 || p.Status().AccountsError != "" {
		t.Fatalf("mirror: %+v %+v", u, p.Status())
	}
	if _, err := NewAuth(pcUsers).Login("owner", pw, "", now); err != nil {
		t.Fatalf("the copied account should log in on the PC: %v", err)
	}

	SaveUsers(vpsUsers, []User{{Name: "../x", Role: RoleAdmin, Hash: h}})
	p.PullUsers(now.Add(2 * pullUsersEach))
	if u, _ := LoadUsers(pcUsers); len(u) != 1 || !strings.Contains(p.Status().AccountsError, "doesn't accept") {
		t.Fatalf("a malformed list must be refused: %+v %+v", u, p.Status())
	}
}
