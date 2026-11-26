// Command api serves the NMIXX FANS dashboard data. It only reads the files the
// prototype writes (captures/*.json, log.jsonl, STOP); it never contacts FANS.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the runtime image has no zoneinfo
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	usersFile := env("USERS_FILE", "/config/users.json")
	if len(os.Args) > 1 && os.Args[1] == "user" {
		// a PC that mirrors the VPS (VPS_URL) would lose local changes at the next copy: change accounts there
		if os.Getenv("VPS_URL") != "" && len(os.Args) > 2 && os.Args[2] != "list" {
			fmt.Fprintln(os.Stderr, "This PC copies its accounts from the VPS every 5 minutes, so a change here would be undone.")
			fmt.Fprintln(os.Stderr, "Change accounts on the VPS instead (docker compose -f docker-compose.vps.yml -p nmixx-stats run --rm -it api user ...).")
			os.Exit(1)
		}
		if err := runUserCmd(usersFile, os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	dataDir := env("DATA_DIR", "/data")
	addr := env("ADDR", ":8080")
	store := NewStore()
	imgDir := filepath.Join(dataDir, "images")
	store.imageDir = imgDir
	capDir := filepath.Join(dataDir, "captures")
	reqDir := env("REQUESTS_DIR", "") // the one writable folder: "pull now" requests for the prototype loop

	scan := func() {
		if n, err := store.IngestDir(capDir); err != nil {
			log.Printf("ingest: %v", err)
		} else if n > 0 {
			log.Printf("ingested %d capture(s)", n)
		}
	}
	scan()
	go func() {
		for range time.Tick(30 * time.Second) {
			scan()
		}
	}()

	// on the home PC: send what the probe writes to the VPS dashboard (see push.go); off unless both are set
	var pusher *Pusher
	if u, t := os.Getenv("VPS_URL"), os.Getenv("VPS_SYNC_TOKEN"); u != "" && t != "" {
		if err := PushURLOK(u); err != nil {
			log.Fatal(err)
		}
		pusher = NewPusher(u, t, dataDir, usersFile)
		log.Printf("uploading to %s", pusher.Status().Host)
	}
	// on the VPS: the home PC uploads what the probe writes (sync.go), and the Admin page's probe controls become
	// orders the PC collects (remote.go); off on the PC itself
	var remote *Remote
	if os.Getenv("SYNC_ENABLED") == "1" {
		remote = NewRemote()
	}
	readAdmin := func() Admin {
		if remote != nil {
			return remote.Admin(time.Now())
		}
		a := ReadAdmin(dataDir, reqDir, time.Now())
		if pusher != nil {
			st := pusher.Status()
			a.Upload = &st
		}
		return a
	}
	if pusher != nil {
		// VPS_REMOTE_CONTROL=1: also carry out the orders given on the VPS Admin page (the same request files)
		pusher.Remote(reqDir, readAdmin, os.Getenv("VPS_REMOTE_CONTROL") == "1")
		go pusher.Run()
	}

	mux := http.NewServeMux()
	if remote != nil {
		registerSync(mux, dataDir, usersFile, store, remote, scan)
		log.Printf("sync uploads on, into %s", dataDir)
	}
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		// when the artist acted (the Fan feed's artist strip): ?from=&to= (RFC3339), or ?hour=0-23[&minute=0-59]&tz=
		when := When{Hour: -1, Minute: -1}
		for _, p := range []struct {
			key string
			dst **time.Time
		}{{"from", &when.From}, {"to", &when.To}} {
			if v := q.Get(p.key); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					http.Error(w, "bad "+p.key, http.StatusBadRequest)
					return
				}
				*p.dst = &t
			}
		}
		if v := q.Get("hour"); v != "" {
			h, err := strconv.Atoi(v)
			loc, lerr := time.LoadLocation(env2(q.Get("tz"), "Asia/Bangkok"))
			if err != nil || h < 0 || h > 23 || lerr != nil {
				http.Error(w, "bad hour or tz", http.StatusBadRequest)
				return
			}
			when.Hour, when.Loc = h, loc
			if v := q.Get("minute"); v != "" {
				m, err := strconv.Atoi(v)
				if err != nil || m < 0 || m > 59 {
					http.Error(w, "bad minute", http.StatusBadRequest)
					return
				}
				when.Minute = m
			}
		}
		writeJSON(w, store.EventsWhen(strings.ToUpper(q.Get("member")), q.Get("kind"), q.Get("q"), limit, when))
	})
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		loc, err := time.LoadLocation(env2(r.URL.Query().Get("tz"), "Asia/Bangkok"))
		if err != nil {
			http.Error(w, "bad tz", http.StatusBadRequest)
			return
		}
		member := strings.ToUpper(r.URL.Query().Get("member"))
		// ?month=YYYY-MM: the activity timeline and weekday heatmap cover that month instead of the last 7 days / all time
		if month := r.URL.Query().Get("month"); month != "" {
			st, err := store.StatsMonth(loc, member, time.Now(), month)
			if err != nil {
				http.Error(w, "bad month", http.StatusBadRequest)
				return
			}
			writeJSON(w, st)
			return
		}
		writeJSON(w, store.Stats(loc, member))
	})
	// one hour of fan posts minute by minute: ?at=<RFC3339 hour start> for a real hour, or ?hour=0-23 for the
	// minute-by-minute average over the hours that hour of day's average uses
	mux.HandleFunc("GET /api/fan-minutes", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		loc, err := time.LoadLocation(env2(q.Get("tz"), "Asia/Bangkok"))
		if err != nil {
			http.Error(w, "bad tz", http.StatusBadRequest)
			return
		}
		if at := q.Get("at"); at != "" {
			t, err := time.Parse(time.RFC3339, at)
			if err != nil {
				http.Error(w, "bad at", http.StatusBadRequest)
				return
			}
			writeJSON(w, store.FanMinutesAt(loc, t))
			return
		}
		h, err := strconv.Atoi(q.Get("hour"))
		if err != nil || h < 0 || h > 23 {
			http.Error(w, "need at or hour", http.StatusBadRequest)
			return
		}
		writeJSON(w, store.FanMinutesAvg(loc, h, time.Now()))
	})
	// every local day captures saw (the Fan feed's calendar), or with ?date=YYYY-MM-DD that one day hour by hour
	mux.HandleFunc("GET /api/fan-days", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		loc, err := time.LoadLocation(env2(q.Get("tz"), "Asia/Bangkok"))
		if err != nil {
			http.Error(w, "bad tz", http.StatusBadRequest)
			return
		}
		if date := q.Get("date"); date != "" {
			day, err := store.FanDayAt(loc, date, time.Now())
			if err != nil {
				http.Error(w, "bad date", http.StatusBadRequest)
				return
			}
			writeJSON(w, day)
			return
		}
		writeJSON(w, store.FanDays(loc, time.Now()))
	})
	// fans whose nickname matches ?q, for the search popup
	mux.HandleFunc("GET /api/fans", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		fans, total := store.Fans(q.Get("q"), limit)
		writeJSON(w, map[string]any{"fans": fans, "total": total})
	})
	mux.HandleFunc("GET /api/feed", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		fq := FeedQuery{Search: q.Get("q"), ArtistOnly: q.Get("artist") == "1", BeforeID: q.Get("beforeId"),
			Member: strings.ToUpper(q.Get("member"))}
		// an hour (and minute) of day in tz, on any day: the fan chart's artist strip
		if h, err := strconv.Atoi(q.Get("hour")); err == nil && h >= 0 && h <= 23 {
			loc, err := time.LoadLocation(env2(q.Get("tz"), "Asia/Bangkok"))
			if err != nil {
				http.Error(w, "bad tz", http.StatusBadRequest)
				return
			}
			fq.Hour, fq.Loc = &h, loc
			if m, err := strconv.Atoi(q.Get("minute")); err == nil && m >= 0 && m <= 59 {
				fq.Minute = &m
			}
		}
		fq.Limit, _ = strconv.Atoi(q.Get("limit"))
		if fq.Limit <= 0 || fq.Limit > 200 {
			fq.Limit = 50
		}
		if b := q.Get("before"); b != "" {
			t, err := time.Parse(time.RFC3339Nano, b)
			if err != nil {
				http.Error(w, "bad before", http.StatusBadRequest)
				return
			}
			fq.BeforeTime = t
		}
		for _, f := range []struct {
			name string
			dst  *time.Time
		}{{"from", &fq.From}, {"to", &fq.To}} {
			if v := q.Get(f.name); v != "" {
				t, err := time.Parse(time.RFC3339Nano, v)
				if err != nil {
					http.Error(w, "bad "+f.name, http.StatusBadRequest)
					return
				}
				*f.dst = t
			}
		}
		writeJSON(w, store.Feed(fq))
	})
	mux.HandleFunc("GET /api/images/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" || name != imageFileName(name) || strings.HasPrefix(name, ".") ||
			strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".missing") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // the name is a content hash
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, filepath.Join(imgDir, name))
	})
	// order sends a probe control to the home PC (VPS only) and answers like the PC would; false = answered with an error
	order := func(w http.ResponseWriter, c Command) bool {
		err := remote.Enqueue(c, time.Now())
		switch {
		case err == nil:
			log.Printf("order for the PC: %s", c.Action)
			return true
		case errors.Is(err, ErrStopChanged):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, ErrPCOffline):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return false
	}
	mux.HandleFunc("GET /api/pull", func(w http.ResponseWriter, r *http.Request) {
		if remote != nil {
			writeJSON(w, remote.Pull(time.Now()))
			return
		}
		writeJSON(w, ReadPull(reqDir))
	})
	mux.HandleFunc("POST /api/pull", func(w http.ResponseWriter, r *http.Request) {
		if remote != nil {
			if order(w, Command{Action: "pull"}) {
				writeJSON(w, remote.Pull(time.Now()))
			}
			return
		}
		st, err := RequestPull(reqDir, time.Now())
		if err != nil {
			log.Printf("pull: %v", err)
			http.Error(w, "pull not available", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, st)
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ReadHealth(dataDir, time.Now()))
	})
	// Admin page: controls for the prototype loop, written as files into the requests folder (see admin.go)
	mux.HandleFunc("GET /api/admin", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, readAdmin())
	})
	mux.HandleFunc("POST /api/admin/control", func(w http.ResponseWriter, r *http.Request) {
		var p ControlPatch
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if remote != nil {
			if order(w, Command{Action: "control", Patch: &p}) {
				writeJSON(w, readAdmin())
			}
			return
		}
		if _, err := UpdateControl(reqDir, p, time.Now()); err != nil {
			log.Printf("admin control: %v", err)
			http.Error(w, "controls not available", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, readAdmin())
	})
	mux.HandleFunc("POST /api/admin/start", func(w http.ResponseWriter, r *http.Request) {
		if remote != nil {
			if order(w, Command{Action: "start"}) {
				writeJSON(w, readAdmin())
			}
			return
		}
		if err := RequestStart(reqDir, time.Now()); err != nil {
			log.Printf("admin start: %v", err)
			http.Error(w, "start not available", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, readAdmin())
	})
	// Stop / Restart the loop, start or cancel Trans-Am: {command: quit | restart | transam | transam-cancel}
	mux.HandleFunc("POST /api/admin/loop", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if _, ok := loopCommands[body.Command]; !ok {
			http.Error(w, ErrUnknownLoopCommand.Error(), http.StatusBadRequest)
			return
		}
		if remote != nil {
			if order(w, Command{Action: "loop", Loop: body.Command}) {
				writeJSON(w, readAdmin())
			}
			return
		}
		if err := RequestLoop(reqDir, body.Command); err != nil {
			log.Printf("admin loop: %v", err)
			http.Error(w, "loop commands not available", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, readAdmin())
	})
	mux.HandleFunc("POST /api/admin/login", func(w http.ResponseWriter, r *http.Request) {
		if remote != nil {
			if order(w, Command{Action: "login"}) {
				writeJSON(w, readAdmin())
			}
			return
		}
		if err := RequestLogin(reqDir, time.Now()); err != nil {
			log.Printf("admin login: %v", err)
			http.Error(w, "login not available", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, readAdmin())
	})
	mux.HandleFunc("POST /api/admin/resume", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stop string `json:"stop"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if remote != nil {
			if order(w, Command{Action: "resume", Stop: body.Stop}) {
				writeJSON(w, readAdmin())
			}
			return
		}
		if err := RequestResume(dataDir, reqDir, body.Stop); err != nil {
			code := http.StatusServiceUnavailable
			if err == ErrStopChanged {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		writeJSON(w, readAdmin())
	})
	// the web app's login form: {name, password, ip} -> {name, role}; the web app makes the session
	auth := NewAuth(usersFile)
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Password string `json:"password"`
			IP       string `json:"ip"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		u, err := auth.Login(body.Name, body.Password, body.IP, time.Now())
		switch {
		case errors.Is(err, ErrLocked):
			http.Error(w, err.Error(), http.StatusTooManyRequests)
		case errors.Is(err, ErrBadLogin):
			log.Printf("login failed for %q", body.Name)
			http.Error(w, err.Error(), http.StatusUnauthorized)
		case err != nil:
			log.Printf("login: %v", err)
			http.Error(w, "login not available", http.StatusServiceUnavailable)
		default:
			log.Printf("login: %s (%s)", u.Name, u.Role)
			writeJSON(w, map[string]string{"name": u.Name, "role": u.Role})
		}
	})
	registerAccounts(mux, accountsFromEnv(usersFile, auth))
	profiles, err := NewProfiles(env("PROFILES_FILE", "/config/profiles.json"), env("AVATARS_DIR", "/config/avatars"))
	if err != nil {
		log.Fatalf("profiles: %v", err)
	}
	registerProfiles(mux, profiles, auth, os.Getenv("VPS_URL") == "")
	registerChat(mux, env("CHAT_FILE", "/config/chat.json"), env("FEEDBACK_FILE", "/config/feedback.json"), profiles)
	visits, err := NewVisits(env("VISITS_FILE", "/config/visits.json"), env("VISITS_TZ", "Asia/Bangkok"))
	if err != nil {
		log.Fatalf("visits: %v", err)
	}
	registerVisits(mux, visits)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// only the web app may call this API: it sends API_TOKEN as a bearer token (the API is never public)
	token := os.Getenv("API_TOKEN")
	if token == "" {
		log.Printf("API_TOKEN not set: every caller is trusted (fine only on 127.0.0.1)")
	}
	log.Printf("listening on %s, data %s", addr, dataDir)
	log.Fatal(http.ListenAndServe(addr, requireToken(token, mux)))
}

// requireToken rejects requests without "Authorization: Bearer <token>" (except /healthz) when token is set.
func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.URL.Path != "/healthz" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func env2(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
