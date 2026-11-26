package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"
)

// accountsFromEnv sets up sign-up and password reset when mail and SITE_URL are configured, else returns nil (off).
// Never on a PC that copies its accounts from the VPS: the next copy would undo any account made here.
func accountsFromEnv(usersFile string, auth *Auth) *Accounts {
	site := strings.TrimRight(os.Getenv("SITE_URL"), "/")
	var m Mailer
	switch {
	case os.Getenv("MAIL_OUTBOX") != "":
		m = OutboxMailer{Dir: os.Getenv("MAIL_OUTBOX")}
	case os.Getenv("BREVO_API_KEY") != "":
		from, err := mail.ParseAddress(os.Getenv("MAIL_FROM"))
		if err != nil {
			log.Fatalf("MAIL_FROM must be like: AxisMixx <no-reply@example.com> (%v)", err)
		}
		m = &BrevoMailer{URL: env("BREVO_API_URL", "https://api.brevo.com/v3/smtp/email"), Key: os.Getenv("BREVO_API_KEY"),
			From: *from, Client: &http.Client{Timeout: 30 * time.Second}}
	}
	switch {
	case m == nil:
		return nil
	case os.Getenv("VPS_URL") != "":
		log.Printf("sign-up off: this PC copies its accounts from the VPS")
		return nil
	case site == "":
		log.Printf("sign-up off: mail is set up but SITE_URL (the address in the mails' links) is not")
		return nil
	}
	if err := SiteURLOK(site); err != nil {
		log.Fatal(err)
	}
	log.Printf("sign-up on, links to %s", site)
	return NewAccounts(usersFile, env("TOKENS_FILE", "/config/account-links.json"), site, m, auth)
}

// registerAccounts serves sign-up, confirm, forgot and reset for the web app's forms. Errors are {"error": code}
// with the codes in account.go, so the pages can say what to fix.
func registerAccounts(mux *http.ServeMux, a *Accounts) {
	mux.HandleFunc("GET /api/auth/options", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"signup": a != nil})
	})
	if a == nil {
		return
	}
	answer := func(w http.ResponseWriter, err error, ok any) {
		code, status := "", http.StatusOK
		switch {
		case err == nil:
			writeJSON(w, ok)
			return
		case errors.Is(err, ErrBadName), errors.Is(err, ErrBadEmail), errors.Is(err, ErrBadPassword):
			code, status = err.Error(), http.StatusBadRequest
		case errors.Is(err, ErrNameTaken), errors.Is(err, ErrEmailTaken):
			code, status = err.Error(), http.StatusConflict
		case errors.Is(err, ErrBadToken):
			code, status = err.Error(), http.StatusGone
		case errors.Is(err, ErrTooFast):
			code, status = "fast", http.StatusTooManyRequests
		default:
			log.Printf("accounts: %v", err)
			code, status = "down", http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": code})
	}
	type body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Token    string `json:"token"`
		IP       string `json:"ip"`
	}
	read := func(w http.ResponseWriter, r *http.Request) (body, bool) {
		var b body
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return b, false
		}
		return b, true
	}
	mux.HandleFunc("POST /api/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		if b, ok := read(w, r); ok {
			answer(w, a.Signup(b.Name, b.Email, b.Password, b.IP, time.Now()), map[string]bool{"ok": true})
		}
	})
	mux.HandleFunc("POST /api/auth/confirm", func(w http.ResponseWriter, r *http.Request) {
		if b, ok := read(w, r); ok {
			u, err := a.Confirm(b.Token, time.Now())
			answer(w, err, map[string]string{"name": u.Name})
		}
	})
	mux.HandleFunc("POST /api/auth/forgot", func(w http.ResponseWriter, r *http.Request) {
		if b, ok := read(w, r); ok {
			answer(w, a.Forgot(b.Email, b.IP, time.Now()), map[string]bool{"ok": true})
		}
	})
	mux.HandleFunc("POST /api/auth/reset", func(w http.ResponseWriter, r *http.Request) {
		if b, ok := read(w, r); ok {
			name, err := a.Reset(b.Token, b.Password, time.Now())
			answer(w, err, map[string]string{"name": name})
		}
	})
}
