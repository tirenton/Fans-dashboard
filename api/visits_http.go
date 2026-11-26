package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// registerVisits: the web app reports page views (POST /api/visits/hit) and the Admin page reads the summary.
func registerVisits(mux *http.ServeMux, v *Visits) {
	mux.HandleFunc("POST /api/visits/hit", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			IP      string `json:"ip"`
			UA      string `json:"ua"`
			Path    string `json:"path"`
			Ref     string `json:"ref"`
			Last    string `json:"last"`
			Member  bool   `json:"member"`
			Account string `json:"account"` // the signed-in account, for unique accounts
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"day": v.Hit(b.IP, b.UA, b.Path, b.Ref, b.Last, b.Account, b.Member, time.Now())})
	})
	mux.HandleFunc("GET /api/visits", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, v.Summary(time.Now()))
	})
}
