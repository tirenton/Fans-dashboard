package main

// Self-service accounts: anyone can sign up with an email address, confirmed by a link, and reset a forgotten
// password by email. New accounts get the user role; admins are still made only with `api user`.
// Mail goes out through Brevo's API (BREVO_API_KEY), or into a folder of files for testing (MAIL_OUTBOX).
// It is off unless mail and SITE_URL are set, and always off on a PC that copies its accounts from the VPS.
//
// Links carry a random 32-byte token; only its SHA-256 is stored (TOKENS_FILE), it works once, and it expires:
// confirm links after 24 hours, reset links after 1 hour. Answers never reveal whether an email has an account.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	siteName   = "AxisMixx"
	confirmFor = 24 * time.Hour
	resetFor   = time.Hour
	maxPending = 500 // unconfirmed sign-ups kept at once
)

var (
	ErrBadName     = errors.New("name")       // not 3–32 of a-z 0-9 . _ -, or reserved
	ErrBadEmail    = errors.New("email")      // not an email address
	ErrBadPassword = errors.New("password")   // shorter than MinPasswordLen or over 1024
	ErrNameTaken   = errors.New("taken")      // the name belongs to an account or a pending sign-up
	ErrEmailTaken  = errors.New("emailtaken") // confirming, but the email got an account meanwhile
	ErrBadToken    = errors.New("token")      // link unknown, used or expired
)

// names people can't pick for themselves (they would look official in the chat)
var reservedNames = map[string]bool{
	"admin": true, "administrator": true, "root": true, "system": true, "moderator": true, "mod": true,
	"staff": true, "support": true, "official": true, "nmixx": true, "jyp": true, "fans": true,
}

func normName(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 || !userNameRe.MatchString(s) || reservedNames[s] {
		return "", ErrBadName
	}
	return s, nil
}

var emailRe = regexp.MustCompile(`^[a-z0-9.!#$%&'*+/=?^_{|}~-]+@[a-z0-9-]+(\.[a-z0-9-]+)+$`)

// normEmail lowercases and checks an address: a plain local@domain.tld, no display name or comments.
func normEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 254 || !emailRe.MatchString(s) {
		return "", ErrBadEmail
	}
	return s, nil
}

func checkPassword(pw string) error {
	if len(pw) < MinPasswordLen || len(pw) > 1024 {
		return ErrBadPassword
	}
	return nil
}

func newToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// --- mail ---

type Mail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// BrevoMailer sends through Brevo's transactional email API.
type BrevoMailer struct {
	URL    string // https://api.brevo.com/v3/smtp/email
	Key    string
	From   mail.Address
	Client *http.Client
}

func (b *BrevoMailer) Send(ctx context.Context, m Mail) error {
	sender := map[string]string{"email": b.From.Address}
	if b.From.Name != "" {
		sender["name"] = b.From.Name
	}
	body, err := json.Marshal(map[string]any{
		"sender": sender, "to": []map[string]string{{"email": m.To}},
		"subject": m.Subject, "textContent": m.Text, "htmlContent": m.HTML,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", b.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("brevo: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// OutboxMailer writes each mail as a JSON file instead of sending it (local testing).
type OutboxMailer struct{ Dir string }

var unsafeFileChars = regexp.MustCompile(`[^a-z0-9]+`)

func (o OutboxMailer) Send(_ context.Context, m Mail) error {
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), unsafeFileChars.ReplaceAllString(m.To, "_"))
	return saveJSON(filepath.Join(o.Dir, name), m)
}

// SiteURLOK checks SITE_URL, the base of the links in mails: https, or http only for this machine.
func SiteURLOK(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" {
		return fmt.Errorf("SITE_URL %q must be like https://example.com", raw)
	}
	h := u.Hostname()
	if u.Scheme == "https" || (u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1")) {
		return nil
	}
	return fmt.Errorf("SITE_URL must start with https://")
}

func mailBody(intro, button, link, outro string) (text, htm string) {
	text = intro + "\n\n" + link + "\n\n" + outro + "\n\n— " + siteName + "\n"
	e := html.EscapeString
	htm = `<div style="font-family:system-ui,sans-serif;font-size:15px;line-height:1.5;color:#222;max-width:520px">` +
		`<p>` + e(intro) + `</p>` +
		`<p><a href="` + e(link) + `" style="display:inline-block;padding:10px 18px;border-radius:8px;background:#1f5fd1;color:#fff;text-decoration:none;font-weight:600">` + e(button) + `</a></p>` +
		`<p style="color:#666;font-size:13px">Or open this link: ` + e(link) + `</p>` +
		`<p style="color:#666;font-size:13px">` + e(outro) + `</p>` +
		`<p style="color:#666;font-size:13px">— ` + e(siteName) + `</p></div>`
	return text, htm
}

// --- accounts ---

type pendingSignup struct {
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Hash    string    `json:"hash"`  // argon2id of the chosen password
	Token   string    `json:"token"` // SHA-256 of the link's token
	Expires time.Time `json:"expires"`
}

type resetLink struct {
	Name    string    `json:"name"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

type tokenFile struct {
	Signups []pendingSignup `json:"signups"`
	Resets  []resetLink     `json:"resets"`
}

type Accounts struct {
	mu         sync.Mutex
	usersFile  string
	tokensFile string
	site       string // https://example.com, the base of the links
	mailer     Mailer
	auth       *Auth

	signupIP limiter // sign-ups per address
	forgotIP limiter // reset requests per address
	toEmail  limiter // mails per recipient (any kind), so nobody can flood an inbox
	sending  sync.WaitGroup
}

func NewAccounts(usersFile, tokensFile, site string, m Mailer, auth *Auth) *Accounts {
	return &Accounts{
		usersFile: usersFile, tokensFile: tokensFile, site: strings.TrimRight(site, "/"), mailer: m, auth: auth,
		signupIP: limiter{n: 5, window: time.Hour},
		forgotIP: limiter{n: 10, window: time.Hour},
		toEmail:  limiter{n: 3, window: time.Hour},
	}
}

// saveTokens writes the link file readable only by its owner (it holds the pending sign-ups' password hashes).
func (a *Accounts) saveTokens(tf tokenFile) error {
	b, err := json.Marshal(tf)
	if err != nil {
		return err
	}
	tmp := a.tokensFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.tokensFile)
}

// tokens loads the link file without the expired entries.
func (a *Accounts) tokens(now time.Time) (tokenFile, error) {
	var tf tokenFile
	if err := loadJSON(a.tokensFile, &tf); err != nil {
		return tf, err
	}
	s := tf.Signups[:0]
	for _, p := range tf.Signups {
		if now.Before(p.Expires) {
			s = append(s, p)
		}
	}
	r := tf.Resets[:0]
	for _, x := range tf.Resets {
		if now.Before(x.Expires) {
			r = append(r, x)
		}
	}
	tf.Signups, tf.Resets = s, r
	return tf, nil
}

// send mails in the background, so the answer takes as long whether or not a mail went out.
func (a *Accounts) send(to, kind string, now time.Time, m Mail) {
	if !a.toEmail.allow(to, now) {
		log.Printf("mail (%s) skipped: this address got 3 mails in the last hour", kind)
		return
	}
	m.To = to
	a.sending.Add(1)
	go func() {
		defer a.sending.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.mailer.Send(ctx, m); err != nil {
			log.Printf("mail (%s) failed: %v", kind, err) // never the address or the link
		}
	}()
}

// Signup checks the details, keeps them as a pending sign-up and mails a confirm link. When the email already has
// an account it mails that person a reminder instead and answers the same, so the form can't be used to find out
// who has an account.
func (a *Accounts) Signup(name, email, pw, ip string, now time.Time) error {
	name, err := normName(name)
	if err != nil {
		return err
	}
	if email, err = normEmail(email); err != nil {
		return err
	}
	if err := checkPassword(pw); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ip != "" && !a.signupIP.allow(ip, now) {
		return ErrTooFast
	}
	users, err := LoadUsers(a.usersFile)
	if err != nil {
		return err
	}
	tf, err := a.tokens(now)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Name == name {
			return ErrNameTaken
		}
	}
	for _, p := range tf.Signups {
		if p.Name == name && p.Email != email {
			return ErrNameTaken
		}
	}
	for _, u := range users {
		if u.Email == email {
			text, htm := mailBody(
				"Someone (hopefully you) tried to sign up for "+siteName+" with this email address, but it already has an account: "+u.Name+". If you forgot the password, you can reset it here:",
				"Reset password", a.site+"/forgot", "If it wasn't you, you can ignore this email.")
			a.send(email, "already", now, Mail{Subject: "You already have an account on " + siteName, Text: text, HTML: htm})
			return nil
		}
	}
	if len(tf.Signups) >= maxPending {
		return ErrTooFast
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	raw, th, err := newToken()
	if err != nil {
		return err
	}
	keep := tf.Signups[:0]
	for _, p := range tf.Signups {
		if p.Email != email { // signing up again with the same email replaces the earlier try
			keep = append(keep, p)
		}
	}
	tf.Signups = append(keep, pendingSignup{Name: name, Email: email, Hash: hash, Token: th, Expires: now.Add(confirmFor)})
	if err := a.saveTokens(tf); err != nil {
		return err
	}
	text, htm := mailBody(
		"Hi "+name+"! Confirm your email address to finish creating your "+siteName+" account:",
		"Confirm email", a.site+"/confirm?t="+raw,
		"The link works for 24 hours. If you didn't sign up, you can ignore this email and no account will be made.")
	a.send(email, "confirm", now, Mail{Subject: "Confirm your email for " + siteName, Text: text, HTML: htm})
	return nil
}

// Confirm turns a pending sign-up into a user account.
func (a *Accounts) Confirm(raw string, now time.Time) (User, error) {
	th := hashToken(raw)
	a.mu.Lock()
	defer a.mu.Unlock()
	usersMu.Lock()
	defer usersMu.Unlock()
	tf, err := a.tokens(now)
	if err != nil {
		return User{}, err
	}
	i := -1
	for j, p := range tf.Signups {
		if p.Token == th {
			i = j
		}
	}
	if i < 0 {
		return User{}, ErrBadToken
	}
	p := tf.Signups[i]
	tf.Signups = append(tf.Signups[:i], tf.Signups[i+1:]...)
	users, err := LoadUsers(a.usersFile)
	if err != nil {
		return User{}, err
	}
	for _, u := range users {
		if u.Name == p.Name || u.Email == p.Email {
			if err := a.saveTokens(tf); err != nil {
				return User{}, err
			}
			if u.Name == p.Name {
				return User{}, ErrNameTaken
			}
			return User{}, ErrEmailTaken
		}
	}
	u := User{Name: p.Name, Role: RoleUser, Hash: p.Hash, Email: p.Email}
	if err := SaveUsers(a.usersFile, append(users, u)); err != nil {
		return User{}, err
	}
	if err := a.saveTokens(tf); err != nil {
		return User{}, err
	}
	log.Printf("new account: %s", u.Name)
	return User{Name: u.Name, Role: u.Role}, nil
}

// Forgot mails a reset link when the email belongs to an account, and answers the same either way.
func (a *Accounts) Forgot(email, ip string, now time.Time) error {
	email, err := normEmail(email)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ip != "" && !a.forgotIP.allow(ip, now) {
		return ErrTooFast
	}
	users, err := LoadUsers(a.usersFile)
	if err != nil {
		return err
	}
	var u *User
	for i := range users {
		if users[i].Email == email {
			u = &users[i]
		}
	}
	if u == nil {
		return nil
	}
	tf, err := a.tokens(now)
	if err != nil {
		return err
	}
	raw, th, err := newToken()
	if err != nil {
		return err
	}
	keep := tf.Resets[:0]
	for _, r := range tf.Resets {
		if r.Name != u.Name { // a new link replaces the older ones
			keep = append(keep, r)
		}
	}
	tf.Resets = append(keep, resetLink{Name: u.Name, Token: th, Expires: now.Add(resetFor)})
	if err := a.saveTokens(tf); err != nil {
		return err
	}
	text, htm := mailBody(
		"Someone (hopefully you) asked to reset the password of your "+siteName+" account, "+u.Name+". Choose a new password here:",
		"Choose a new password", a.site+"/reset?t="+raw,
		"The link works for 1 hour. If you didn't ask for this, you can ignore this email; your password stays the same.")
	a.send(email, "reset", now, Mail{Subject: "Reset your " + siteName + " password", Text: text, HTML: htm})
	return nil
}

// Reset sets a new password with a reset link, and lifts a login lockout on that account.
func (a *Accounts) Reset(raw, pw string, now time.Time) (string, error) {
	if err := checkPassword(pw); err != nil {
		return "", err
	}
	th := hashToken(raw)
	a.mu.Lock()
	defer a.mu.Unlock()
	usersMu.Lock()
	defer usersMu.Unlock()
	tf, err := a.tokens(now)
	if err != nil {
		return "", err
	}
	name := ""
	for _, r := range tf.Resets {
		if r.Token == th {
			name = r.Name
		}
	}
	if name == "" {
		return "", ErrBadToken
	}
	keep := tf.Resets[:0]
	for _, r := range tf.Resets {
		if r.Name != name {
			keep = append(keep, r)
		}
	}
	tf.Resets = keep
	users, err := LoadUsers(a.usersFile)
	if err != nil {
		return "", err
	}
	i := -1
	for j, u := range users {
		if u.Name == name {
			i = j
		}
	}
	if i < 0 {
		a.saveTokens(tf)
		return "", ErrBadToken
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return "", err
	}
	users[i].Hash = hash
	if err := SaveUsers(a.usersFile, users); err != nil {
		return "", err
	}
	if err := a.saveTokens(tf); err != nil {
		return "", err
	}
	if a.auth != nil {
		a.auth.Unlock(name)
	}
	log.Printf("password reset: %s", name)
	return name, nil
}
