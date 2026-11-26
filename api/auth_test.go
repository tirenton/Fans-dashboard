package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testPassword makes a random password for a test, so no password-like text sits in the code.
func testPassword(t *testing.T) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func TestPasswordHash(t *testing.T) {
	pw := testPassword(t)
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("want an argon2id PHC string, got %q", h)
	}
	if strings.Contains(h, pw) {
		t.Fatal("hash contains the password")
	}
	if !VerifyPassword(h, pw) || VerifyPassword(h, pw+"x") {
		t.Fatal("verify: right password must pass, wrong must fail")
	}
	h2, _ := HashPassword(pw)
	if h2 == h {
		t.Fatal("two hashes of one password must differ (random salt)")
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=65536,t=3,p=2$AAAA$AAAA", "$argon2id$v=19$m=99999999,t=3,p=2$AAAA$AAAA"} {
		if VerifyPassword(bad, "x") {
			t.Fatalf("%q must not verify", bad)
		}
	}
}

func TestUserCommandAndLogin(t *testing.T) {
	adminPW, userPW := testPassword(t), testPassword(t)
	file := filepath.Join(t.TempDir(), "users.json")
	run := func(stdin string, args ...string) (string, error) {
		var out bytes.Buffer
		err := runUserCmd(file, args, strings.NewReader(stdin), &out)
		return out.String(), err
	}
	if _, err := run("short\nshort\n", "add", "boss", "admin"); err == nil {
		t.Fatal("want a too-short password refused")
	}
	if _, err := run(adminPW+"\n"+adminPW+"x\n", "add", "boss", "admin"); err == nil {
		t.Fatal("want differing passwords refused")
	}
	if _, err := run(adminPW+"\n"+adminPW+"\n", "add", "Boss", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(userPW+"\n"+userPW+"\n", "add", "fan", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("x\n", "add", "fan", "user"); err == nil {
		t.Fatal("want a duplicate name refused")
	}
	if _, err := run("", "add", "../x", "user"); err == nil {
		t.Fatal("want a bad name refused")
	}
	if _, err := run("", "add", "x", "root"); err == nil {
		t.Fatal("want a bad role refused")
	}
	out, _ := run("", "list")
	if !strings.Contains(out, "boss") || !strings.Contains(out, "fan") || strings.Contains(out, "argon2") {
		t.Fatalf("list: want names and roles only, got %q", out)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), adminPW) {
		t.Fatal("users file contains a password")
	}

	a := NewAuth(file)
	now := time.Now()
	if u, err := a.Login("BOSS", adminPW, "1.2.3.4", now); err != nil || u.Role != RoleAdmin || u.Hash != "" {
		t.Fatalf("admin login: %+v %v", u, err)
	}
	if u, err := a.Login("fan", userPW, "1.2.3.4", now); err != nil || u.Role != RoleUser {
		t.Fatalf("user login: %+v %v", u, err)
	}
	if _, err := a.Login("nobody", testPassword(t), "", now); err != ErrBadLogin {
		t.Fatalf("unknown name: want ErrBadLogin, got %v", err)
	}
	for i := 0; i < maxFailsPerUser; i++ {
		if _, err := a.Login("fan", "wrong", "5.6.7.8", now); err != ErrBadLogin {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	if _, err := a.Login("fan", userPW, "5.6.7.8", now); err != ErrLocked {
		t.Fatalf("after %d failures even the right password must wait, got %v", maxFailsPerUser, err)
	}
	if _, err := a.Login("fan", userPW, "5.6.7.8", now.Add(lockWindow+time.Second)); err != nil {
		t.Fatalf("after the lock window: %v", err)
	}

	if _, err := run("", "role", "fan", "admin"); err != nil {
		t.Fatal(err)
	}
	if u, _ := a.Login("fan", userPW, "", now.Add(time.Hour)); u.Role != RoleAdmin {
		t.Fatalf("role change applies at the next login, got %+v", u)
	}
	if _, err := run("", "rm", "fan"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login("fan", userPW, "", now.Add(time.Hour)); err != ErrBadLogin {
		t.Fatalf("removed account: want ErrBadLogin, got %v", err)
	}
}

func TestRequireToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	h := requireToken("s3cret", ok)
	for _, c := range []struct {
		path, auth string
		want       int
	}{
		{"/api/stats", "", 401},
		{"/api/stats", "Bearer wrong", 401},
		{"/api/stats", "Bearer s3cret", 200},
		{"/healthz", "", 200},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Fatalf("%s %q: want %d, got %d", c.path, c.auth, c.want, w.Code)
		}
	}
}
