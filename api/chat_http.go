package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"
)

// registerChat serves the chat and the feedback box. The web app checks who may do what (any account may read
// and post, admins may delete and read feedback) and passes the signed-in name.
func registerChat(mux *http.ServeMux, chatFile, feedbackFile string, profiles *Profiles) {
	chat, err := NewChat(chatFile)
	if err != nil {
		log.Fatalf("chat %s: %v", chatFile, err) // never start empty and overwrite a file we couldn't read
	}
	profiles.OnChange = func() { chat.Touch(time.Now()) }
	// the messages plus the display name and picture of everyone in them
	type withProfiles struct {
		ChatView
		Profiles map[string]Profile `json:"profiles"`
	}
	show := func(v ChatView) withProfiles {
		names := make([]string, 0, len(v.Messages))
		for _, m := range v.Messages {
			names = append(names, m.Name)
		}
		return withProfiles{v, profiles.Of(names)}
	}
	box, err := NewFeedbackBox(feedbackFile)
	if err != nil {
		log.Fatalf("feedback %s: %v", feedbackFile, err)
	}

	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, ErrEmpty), errors.Is(err, ErrTooLong):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrTooFast):
			http.Error(w, err.Error(), http.StatusTooManyRequests)
		case errors.Is(err, ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			log.Printf("chat: %v", err)
			http.Error(w, "not available", http.StatusServiceUnavailable)
		}
	}
	type post struct {
		Name  string `json:"name"`
		Admin bool   `json:"admin"`
		Text  string `json:"text"`
	}
	readPost := func(w http.ResponseWriter, r *http.Request) (post, bool) {
		var p post
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&p); err != nil || !userNameRe.MatchString(p.Name) {
			http.Error(w, "bad body", http.StatusBadRequest)
			return p, false
		}
		return p, true
	}
	id := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
		}
		return n, err == nil
	}

	// ?v=<version>&wait=<seconds>: hold on (up to 25 s) until the chat differs from that version
	mux.HandleFunc("GET /api/chat", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		v, verr := strconv.ParseInt(q.Get("v"), 10, 64)
		wait, _ := strconv.Atoi(q.Get("wait"))
		if verr != nil || wait <= 0 {
			writeJSON(w, show(chat.View()))
			return
		}
		writeJSON(w, show(chat.Wait(r.Context(), v, time.Duration(min(wait, 25))*time.Second)))
	})
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		p, ok := readPost(w, r)
		if !ok {
			return
		}
		m, err := chat.Post(p.Name, p.Admin, p.Text, time.Now())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, m)
	})
	mux.HandleFunc("DELETE /api/chat/{id}", func(w http.ResponseWriter, r *http.Request) {
		if n, ok := id(w, r); ok {
			if err := chat.Delete(n, time.Now()); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})

	mux.HandleFunc("GET /api/feedback", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, box.List())
	})
	mux.HandleFunc("POST /api/feedback", func(w http.ResponseWriter, r *http.Request) {
		p, ok := readPost(w, r)
		if !ok {
			return
		}
		it, err := box.Add(p.Name, p.Text, time.Now())
		if err != nil {
			fail(w, err)
			return
		}
		log.Printf("feedback from %s", p.Name)
		writeJSON(w, it)
	})
	mux.HandleFunc("DELETE /api/feedback/{id}", func(w http.ResponseWriter, r *http.Request) {
		if n, ok := id(w, r); ok {
			if err := box.Delete(n); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
}
