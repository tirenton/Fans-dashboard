package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// registerProfiles serves the profile page's calls and the pictures. The web app checks the session and passes the
// signed-in name in the path (an admin may also remove someone else's picture); errors are {"error": code}.
// canChangePassword is false on the home PC, whose accounts are copies of the VPS's (a change here would be undone).
func registerProfiles(mux *http.ServeMux, p *Profiles, auth *Auth, canChangePassword bool) {
	fail := func(w http.ResponseWriter, err error) {
		code, status := "down", http.StatusServiceUnavailable
		switch {
		case errors.Is(err, ErrTooLong):
			code, status = "toolong", http.StatusBadRequest
		case errors.Is(err, ErrBadImage):
			code, status = "image", http.StatusBadRequest
		case errors.Is(err, ErrBadPassword):
			code, status = "password", http.StatusBadRequest
		case errors.Is(err, ErrBadLogin):
			code, status = "current", http.StatusBadRequest
		case errors.Is(err, ErrTooFast), errors.Is(err, ErrLocked):
			code, status = "fast", http.StatusTooManyRequests
		case errors.Is(err, ErrNoUser):
			code, status = "nouser", http.StatusNotFound
		default:
			log.Printf("profile: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": code})
	}
	name := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		n := r.PathValue("name")
		if !userNameRe.MatchString(n) {
			http.Error(w, "bad name", http.StatusBadRequest)
			return "", false
		}
		return n, true
	}
	type me struct {
		Name              string `json:"name"`
		Email             string `json:"email,omitempty"`
		CanChangePassword bool   `json:"canChangePassword"`
		Profile
	}

	mux.HandleFunc("GET /api/profile/{name}", func(w http.ResponseWriter, r *http.Request) {
		if n, ok := name(w, r); ok {
			writeJSON(w, me{Name: n, Email: auth.Email(n), CanChangePassword: canChangePassword, Profile: p.Get(n)})
		}
	})
	mux.HandleFunc("PUT /api/profile/{name}/display", func(w http.ResponseWriter, r *http.Request) {
		n, ok := name(w, r)
		if !ok {
			return
		}
		var b struct {
			Display string `json:"display"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		pr, err := p.SetDisplay(n, b.Display, time.Now())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, pr)
	})
	mux.HandleFunc("PUT /api/profile/{name}/avatar", func(w http.ResponseWriter, r *http.Request) {
		n, ok := name(w, r)
		if !ok {
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, avatarMaxBytes))
		if err != nil {
			fail(w, ErrBadImage)
			return
		}
		pr, err := p.SetAvatar(n, data, time.Now())
		if err != nil {
			fail(w, err)
			return
		}
		log.Printf("new profile picture: %s", n)
		writeJSON(w, pr)
	})
	mux.HandleFunc("DELETE /api/profile/{name}/avatar", func(w http.ResponseWriter, r *http.Request) {
		if n, ok := name(w, r); ok {
			pr, err := p.RemoveAvatar(n)
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, pr)
		}
	})
	mux.HandleFunc("POST /api/profile/{name}/password", func(w http.ResponseWriter, r *http.Request) {
		n, ok := name(w, r)
		if !ok {
			return
		}
		if !canChangePassword {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "pc"})
			return
		}
		var b struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := auth.ChangePassword(n, b.Current, b.New, time.Now()); err != nil {
			fail(w, err)
			return
		}
		log.Printf("password changed: %s", n)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/avatar/{name}", func(w http.ResponseWriter, r *http.Request) {
		n, ok := name(w, r)
		if !ok {
			return
		}
		f, err := os.Open(p.AvatarPath(n))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		io.Copy(w, f)
	})
}
