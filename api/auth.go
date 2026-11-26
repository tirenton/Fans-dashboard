package main

// Dashboard accounts. Passwords are stored only as argon2id hashes (PHC string format) in a JSON file
// (USERS_FILE) that the operator edits with the `api user ...` command. The web app asks POST /api/auth/login
// and keeps its own signed session cookie; this API never sees the session.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	RoleAdmin = "admin" // everything, including the Admin and Health pages and Pull now
	RoleUser  = "user"  // the Main dashboard's chat and feedback box; every other page is for admins
)

// argon2id parameters (OWASP's 64 MiB / 3 passes / 2 lanes class): about 0.1 s per check.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// MinPasswordLen is the shortest password `api user` accepts.
const MinPasswordLen = 10

type User struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Hash  string `json:"hash"`            // argon2id PHC string
	Email string `json:"email,omitempty"` // confirmed address (sign-up, password reset, logging in by email)
}

type usersFile struct {
	Users []User `json:"users"`
}

var userNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)

func validRole(r string) bool { return r == RoleAdmin || r == RoleUser }

// HashPassword returns an argon2id PHC string: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a PHC string from HashPassword (its own parameters, so they can be raised later).
func VerifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || m > 1<<20 || t == 0 || t > 10 || p == 0 {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 || len(want) > 128 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func LoadUsers(path string) ([]User, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f usersFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Users, nil
}

// SaveUsers writes the file atomically, readable only by its owner.
func SaveUsers(path string, users []User) error {
	if users == nil {
		users = []User{}
	}
	b, err := json.MarshalIndent(usersFile{Users: users}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var (
	ErrBadLogin = errors.New("wrong name or password")
	ErrLocked   = errors.New("too many failed logins, try again later")
)

// Login lockout: this many failures within lockWindow locks the account (or the client address) until the
// window has passed since the first of them.
const (
	maxFailsPerUser = 5
	maxFailsPerIP   = 20
	lockWindow      = 15 * time.Minute
)

type failCount struct {
	n     int
	first time.Time
}

// Auth checks logins against the users file, which it re-reads every time (so `api user` changes apply at once).
type Auth struct {
	file  string
	mu    sync.Mutex
	fails map[string]*failCount // "u:<name>" or "ip:<addr>"
	sem   chan struct{}         // at most 2 hashes at once: each takes 64 MiB
	dummy string                // checked for unknown names, so they take as long as known ones
}

func NewAuth(file string) *Auth {
	d, _ := HashPassword("not a real password, only for timing")
	return &Auth{file: file, fails: map[string]*failCount{}, sem: make(chan struct{}, 2), dummy: d}
}

func (a *Auth) locked(key string, max int, now time.Time) bool {
	f := a.fails[key]
	if f == nil {
		return false
	}
	if now.Sub(f.first) > lockWindow {
		delete(a.fails, key)
		return false
	}
	return f.n >= max
}

func (a *Auth) fail(key string, now time.Time) {
	f := a.fails[key]
	if f == nil || now.Sub(f.first) > lockWindow {
		f = &failCount{first: now}
		a.fails[key] = f
	}
	f.n++
}

// usersMu serialises read-modify-write of the users file inside this process (sign-up confirm, reset, password change).
var usersMu sync.Mutex

var ErrNoUser = errors.New("no such account")

// ChangePassword sets a new password after checking the current one (with the same lockout as logging in).
func (a *Auth) ChangePassword(name, current, next string, now time.Time) error {
	if err := checkPassword(next); err != nil {
		return err
	}
	u, err := a.Login(name, current, "", now)
	if err != nil {
		return err
	}
	if u.Name != name {
		return ErrBadLogin // matched someone else's email: never change another account
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	usersMu.Lock()
	defer usersMu.Unlock()
	users, err := LoadUsers(a.file)
	if err != nil {
		return err
	}
	for i := range users {
		if users[i].Name == name {
			users[i].Hash = hash
			return SaveUsers(a.file, users)
		}
	}
	return ErrNoUser
}

// Email returns the account's confirmed email address ("" if none).
func (a *Auth) Email(name string) string {
	users, _ := LoadUsers(a.file)
	for _, u := range users {
		if u.Name == name {
			return u.Email
		}
	}
	return ""
}

// Unlock lifts the lockout on an account (after its password was reset by email).
func (a *Auth) Unlock(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.fails, "u:"+name)
}

// Login returns the user when the name (or the account's email) and password match. ip is the client address as
// the web app saw it ("" if unknown).
func (a *Auth) Login(name, pw, ip string, now time.Time) (User, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	users, err := LoadUsers(a.file)
	if err != nil {
		return User{}, err
	}
	var u *User
	for i := range users {
		if (users[i].Name == name || (users[i].Email != "" && users[i].Email == name)) && validRole(users[i].Role) {
			u = &users[i]
		}
	}
	// one lockout per account, whether it is tried by name or by email
	uKey, ipKey := "u:"+name, "ip:"+ip
	if u != nil {
		uKey = "u:" + u.Name
	}
	a.mu.Lock()
	if a.locked(uKey, maxFailsPerUser, now) || (ip != "" && a.locked(ipKey, maxFailsPerIP, now)) {
		a.mu.Unlock()
		return User{}, ErrLocked
	}
	if len(pw) > 1024 || name == "" {
		a.fail(uKey, now)
		a.mu.Unlock()
		return User{}, ErrBadLogin
	}
	a.mu.Unlock()

	a.sem <- struct{}{}
	ok := false
	if u != nil {
		ok = VerifyPassword(u.Hash, pw)
	} else {
		VerifyPassword(a.dummy, pw)
	}
	<-a.sem

	a.mu.Lock()
	defer a.mu.Unlock()
	if !ok {
		a.fail(uKey, now)
		if ip != "" {
			a.fail(ipKey, now)
		}
		return User{}, ErrBadLogin
	}
	delete(a.fails, uKey)
	return User{Name: u.Name, Role: u.Role}, nil
}
