package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMailer struct {
	mu   sync.Mutex
	sent []Mail
}

func (f *fakeMailer) Send(_ context.Context, m Mail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMailer) all(a *Accounts) []Mail {
	a.sending.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Mail{}, f.sent...)
}

var linkRe = regexp.MustCompile(`https://stats\.example/(confirm|reset)\?t=([A-Za-z0-9_-]{43})`)

func linkIn(t *testing.T, m Mail, kind string) string {
	t.Helper()
	got := linkRe.FindStringSubmatch(m.Text)
	if got == nil || got[1] != kind || !strings.Contains(m.HTML, got[0]) {
		t.Fatalf("no %s link in %q", kind, m.Text)
	}
	return got[2]
}

func newTestAccounts(t *testing.T) (*Accounts, *fakeMailer, string) {
	dir := t.TempDir()
	users := filepath.Join(dir, "users.json")
	fm := &fakeMailer{}
	return NewAccounts(users, filepath.Join(dir, "links.json"), "https://stats.example/", fm, NewAuth(users)), fm, users
}

func TestSignupConfirmLogin(t *testing.T) {
	a, fm, users := newTestAccounts(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	pw := testPassword(t)

	for _, c := range []struct {
		name, email, pw string
		want            error
	}{
		{"ab", "a@b.co", pw, ErrBadName},
		{"Admin", "a@b.co", pw, ErrBadName},
		{"has space", "a@b.co", pw, ErrBadName},
		{"lily_fan", "not-an-email", pw, ErrBadEmail},
		{"lily_fan", "Name <a@b.co>", pw, ErrBadEmail},
		{"lily_fan", "a@localhost", pw, ErrBadEmail},
		{"lily_fan", "a@b.co", "short", ErrBadPassword},
	} {
		if err := a.Signup(c.name, c.email, c.pw, "", now); !errors.Is(err, c.want) {
			t.Errorf("Signup(%q, %q): %v, want %v", c.name, c.email, err, c.want)
		}
	}
	if n := len(fm.all(a)); n != 0 {
		t.Fatalf("bad sign-ups sent %d mails", n)
	}

	if err := a.Signup(" Lily_Fan ", " Fan@Example.COM ", pw, "1.2.3.4", now); err != nil {
		t.Fatal(err)
	}
	mails := fm.all(a)
	if len(mails) != 1 || mails[0].To != "fan@example.com" || !strings.Contains(mails[0].Subject, "Confirm") {
		t.Fatalf("mails: %+v", mails)
	}
	tok := linkIn(t, mails[0], "confirm")
	// not an account until confirmed, and the name is held for this email only
	if _, err := a.auth.Login("lily_fan", pw, "", now); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("login before confirming: %v", err)
	}
	if err := a.Signup("lily_fan", "other@example.com", pw, "", now); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("name held by a pending sign-up: %v", err)
	}
	// the stored file has only the token's hash, and is private
	raw, _ := os.ReadFile(a.tokensFile)
	if strings.Contains(string(raw), tok) || strings.Contains(string(raw), pw) {
		t.Fatal("links file holds the token or the password")
	}
	if st, _ := os.Stat(a.tokensFile); st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("links file mode %v", st.Mode())
	}

	if _, err := a.Confirm("x"+tok[1:], now); !errors.Is(err, ErrBadToken) {
		t.Fatalf("wrong token: %v", err)
	}
	u, err := a.Confirm(tok, now.Add(time.Hour))
	if err != nil || u.Name != "lily_fan" || u.Role != RoleUser {
		t.Fatalf("confirm: %+v %v", u, err)
	}
	if _, err := a.Confirm(tok, now.Add(time.Hour)); !errors.Is(err, ErrBadToken) {
		t.Fatalf("second use: %v", err)
	}
	list, _ := LoadUsers(users)
	if len(list) != 1 || list[0].Email != "fan@example.com" {
		t.Fatalf("users: %+v", list)
	}
	for _, id := range []string{"lily_fan", "FAN@example.com"} {
		if got, err := a.auth.Login(id, pw, "", now); err != nil || got.Name != "lily_fan" {
			t.Fatalf("login as %q: %+v %v", id, got, err)
		}
	}

	// the same email again: no second account, a reminder instead, and the same answer
	if err := a.Signup("someone_else", "fan@example.com", pw, "", now); err != nil {
		t.Fatalf("existing email: %v", err)
	}
	mails = fm.all(a)
	if last := mails[len(mails)-1]; !strings.Contains(last.Subject, "already have an account") || !strings.Contains(last.Text, "lily_fan") {
		t.Fatalf("reminder: %+v", last)
	}
	if err := a.Signup("lily_fan", "new@example.com", pw, "", now); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("taken name: %v", err)
	}
}

func TestConfirmLinkExpires(t *testing.T) {
	a, fm, _ := newTestAccounts(t)
	now := time.Now()
	if err := a.Signup("late_fan", "late@example.com", testPassword(t), "", now); err != nil {
		t.Fatal(err)
	}
	tok := linkIn(t, fm.all(a)[0], "confirm")
	if _, err := a.Confirm(tok, now.Add(confirmFor+time.Minute)); !errors.Is(err, ErrBadToken) {
		t.Fatalf("expired link: %v", err)
	}
	// and the name is free again
	if err := a.Signup("late_fan", "other@example.com", testPassword(t), "", now.Add(confirmFor+time.Minute)); err != nil {
		t.Fatalf("name after expiry: %v", err)
	}
}

func TestForgotAndReset(t *testing.T) {
	a, fm, users := newTestAccounts(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	old, fresh := testPassword(t), testPassword(t)
	h, _ := HashPassword(old)
	SaveUsers(users, []User{{Name: "fan", Role: RoleUser, Hash: h, Email: "fan@example.com"}})

	if err := a.Forgot("nobody@example.com", "", now); err != nil {
		t.Fatalf("unknown email must look the same: %v", err)
	}
	if n := len(fm.all(a)); n != 0 {
		t.Fatalf("mailed an unknown address: %d", n)
	}
	// locked out by wrong passwords; a reset lifts it
	for i := 0; i < maxFailsPerUser; i++ {
		a.auth.Login("fan", "wrong password", "", now)
	}
	if _, err := a.auth.Login("fan", old, "", now); !errors.Is(err, ErrLocked) {
		t.Fatalf("want locked: %v", err)
	}
	if err := a.Forgot("FAN@example.com", "", now); err != nil {
		t.Fatal(err)
	}
	first := linkIn(t, fm.all(a)[0], "reset")
	if err := a.Forgot("fan@example.com", "", now); err != nil {
		t.Fatal(err)
	}
	tok := linkIn(t, fm.all(a)[1], "reset")
	if _, err := a.Reset(first, fresh, now); !errors.Is(err, ErrBadToken) {
		t.Fatalf("an older link must stop working: %v", err)
	}
	if _, err := a.Reset(tok, "short", now); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("short password: %v", err)
	}
	if _, err := a.Reset(tok, fresh, now.Add(resetFor+time.Second)); !errors.Is(err, ErrBadToken) {
		t.Fatalf("expired: %v", err)
	}
	if err := a.Forgot("fan@example.com", "", now); err != nil {
		t.Fatal(err)
	}
	tok = linkIn(t, fm.all(a)[2], "reset")
	if name, err := a.Reset(tok, fresh, now); err != nil || name != "fan" {
		t.Fatalf("reset: %q %v", name, err)
	}
	if _, err := a.Reset(tok, fresh, now); !errors.Is(err, ErrBadToken) {
		t.Fatalf("second use: %v", err)
	}
	if _, err := a.auth.Login("fan", fresh, "", now); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if _, err := a.auth.Login("fan@example.com", old, "", now); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("old password: %v", err)
	}
	// a 4th mail to the same inbox within the hour is not sent
	a.Forgot("fan@example.com", "", now.Add(time.Minute))
	if n := len(fm.all(a)); n != 3 {
		t.Fatalf("mails to one inbox in an hour: %d, want 3", n)
	}
}

func TestSignupLimits(t *testing.T) {
	a, _, _ := newTestAccounts(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := a.Signup("fan_"+string(rune('a'+i))+"x", string(rune('a'+i))+"@example.com", testPassword(t), "9.9.9.9", now); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Signup("fan_zzz", "z@example.com", testPassword(t), "9.9.9.9", now); !errors.Is(err, ErrTooFast) {
		t.Fatalf("6th sign-up from one address in an hour: %v", err)
	}
	if err := a.Signup("fan_yyy", "y@example.com", testPassword(t), "8.8.8.8", now); err != nil {
		t.Fatalf("another address: %v", err)
	}
}

func TestBrevoMailer(t *testing.T) {
	var got map[string]any
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("api-key")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		if r.URL.Path == "/fail" {
			http.Error(w, `{"code":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"messageId":"<x@smtp-relay.mailin.fr>"}`))
	}))
	defer srv.Close()
	m := &BrevoMailer{URL: srv.URL + "/v3/smtp/email", Key: "k1", From: mail.Address{Name: "Stats", Address: "no-reply@stats.example"}, Client: srv.Client()}
	if err := m.Send(context.Background(), Mail{To: "fan@example.com", Subject: "Hi", Text: "t", HTML: "<p>h</p>"}); err != nil {
		t.Fatal(err)
	}
	sender, _ := got["sender"].(map[string]any)
	to, _ := got["to"].([]any)
	if key != "k1" || sender["email"] != "no-reply@stats.example" || sender["name"] != "Stats" || len(to) != 1 ||
		got["subject"] != "Hi" || got["htmlContent"] != "<p>h</p>" || got["textContent"] != "t" {
		t.Fatalf("request: key %q body %+v", key, got)
	}
	m.URL = srv.URL + "/fail"
	if err := m.Send(context.Background(), Mail{To: "a@b.co"}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want a 401 error, got %v", err)
	}
}

func TestSiteURLOK(t *testing.T) {
	for _, ok := range []string{"https://stats.example", "http://localhost:3100"} {
		if err := SiteURLOK(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://stats.example", "stats.example", "https://stats.example/x", "https://"} {
		if SiteURLOK(bad) == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
