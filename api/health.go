package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Hard limits from the go/no-go decision (api.app.fans requests).
const (
	HourCap = 15
	DayCap  = 150
)

type PassRow struct {
	At       time.Time      `json:"at"`
	Kind     string         `json:"kind"` // pass | skip | warn | login
	Type     string         `json:"type,omitempty"`
	Requests int            `json:"requests"`
	Statuses map[string]int `json:"statuses,omitempty"`
	Posts    int            `json:"posts"`
	Note     string         `json:"note,omitempty"`
}

type HourBucket struct {
	Hour     time.Time `json:"hour"`
	Requests int       `json:"requests"`
	Passes   int       `json:"passes"`
}

type Health struct {
	Now               time.Time    `json:"now"`
	HourCap           int          `json:"hourCap"`
	DayCap            int          `json:"dayCap"`
	RequestsLastHour  int          `json:"requestsLastHour"`
	RequestsLast24h   int          `json:"requestsLast24h"`
	Passes            int          `json:"passes"`
	Skips             int          `json:"skips"`
	Warns             int          `json:"warns"`
	LastPassAt        *time.Time   `json:"lastPassAt,omitempty"`
	LastCaptureOKAt   *time.Time   `json:"lastCaptureOkAt,omitempty"`
	AccessTokenExp    *time.Time   `json:"accessTokenExp,omitempty"`
	RefreshCookieExp  *time.Time   `json:"refreshCookieExp,omitempty"`
	RefreshCookieExps []time.Time  `json:"refreshCookieExps"` // distinct values seen: does it slide?
	RefreshCalls      []PassRow    `json:"refreshCalls"`
	Webdriver         *bool        `json:"webdriver,omitempty"`
	Stop              string       `json:"stop,omitempty"`
	Hourly            []HourBucket `json:"hourly"` // last 48 h, oldest first
	Recent            []PassRow    `json:"recent"` // newest first
	LogMissing        bool         `json:"logMissing"`
}

func num(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func tm(v any) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, str(v))
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

// ReadHealth summarises the prototype's log.jsonl and STOP file. It re-reads the log on
// every call: it is small (one line per pass) and this keeps the API stateless about it.
func ReadHealth(dir string, now time.Time) Health {
	h := Health{Now: now.UTC(), HourCap: HourCap, DayCap: DayCap, RefreshCookieExps: []time.Time{}, RefreshCalls: []PassRow{}, Recent: []PassRow{}, Hourly: []HourBucket{}}
	if b, err := os.ReadFile(filepath.Join(dir, "STOP")); err == nil {
		h.Stop = strings.TrimSpace(string(b))
	}
	f, err := os.Open(filepath.Join(dir, "log.jsonl"))
	if err != nil {
		h.LogMissing = true
		return h
	}
	defer f.Close()

	start := now.UTC().Truncate(time.Hour).Add(-47 * time.Hour)
	buckets := make([]HourBucket, 48)
	for i := range buckets {
		buckets[i].Hour = start.Add(time.Duration(i) * time.Hour)
	}
	seenCookie := map[time.Time]bool{}
	readCookies := func(e map[string]any) {
		for _, c := range asSlice(e["cookiesAfter"]) {
			co := obj(c)
			if strings.HasPrefix(str(co["name"]), "refreshToken") {
				if t := tm(co["expires"]); t != nil {
					h.RefreshCookieExp = t
					if !seenCookie[*t] {
						seenCookie[*t] = true
						h.RefreshCookieExps = append(h.RefreshCookieExps, *t)
					}
				}
			}
		}
	}
	var rows []PassRow

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var e map[string]any
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		at := tm(e["at"])
		if at == nil {
			continue
		}
		row := PassRow{At: *at, Kind: str(e["kind"]), Type: str(e["type"])}
		switch row.Kind {
		case "pass":
			h.Passes++
			api := obj(e["apiRequests"])
			row.Requests = num(api["total"])
			row.Statuses = map[string]int{}
			for k, v := range obj(api["statuses"]) {
				row.Statuses[k] = num(v)
			}
			if c := obj(e["capture"]); c != nil {
				row.Posts = num(c["posts"])
				if row.Posts > 0 {
					h.LastCaptureOKAt = at
				}
			}
			if row.Type == "" {
				row.Type = "full-load" // first prototype version
			}
			row.Note = str(e["error"])
			if row.Note == "" {
				row.Note = str(e["gotoError"])
			}
			for _, r := range asSlice(api["refresh"]) {
				ro := obj(r)
				h.RefreshCalls = append(h.RefreshCalls, PassRow{At: *at, Kind: "refresh", Statuses: map[string]int{itoa(num(ro["status"])): 1}})
			}
			// token: newer log lines use tokenAfter, the first version used tokenTimes
			tok := obj(e["tokenAfter"])
			if tok == nil {
				tok = obj(e["tokenTimes"])
			}
			if t := tm(tok["exp"]); t != nil {
				h.AccessTokenExp = t
			}
			readCookies(e)
			if p := obj(e["probe"]); p != nil {
				if b, ok := p["webdriver"].(bool); ok {
					h.Webdriver = &b
				}
			}
			h.LastPassAt = at
			if now.Sub(*at) < time.Hour {
				h.RequestsLastHour += row.Requests
			}
			if now.Sub(*at) < 24*time.Hour {
				h.RequestsLast24h += row.Requests
			}
			if i := int(at.Sub(start) / time.Hour); i >= 0 && i < 48 && !at.Before(start) {
				buckets[i].Requests += row.Requests
				buckets[i].Passes++
			}
		case "skip":
			h.Skips++
			row.Note = str(e["reason"])
		case "warn":
			h.Warns++
			row.Note = str(e["msg"])
		case "login":
			row.Note = "manual login"
			if by := str(e["by"]); by != "" {
				row.Note += " (" + by + ")"
			}
			readCookies(e) // a login sets a new refresh cookie, with a new expiry
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })
	if len(rows) > 50 {
		rows = rows[:50]
	}
	h.Recent = append(h.Recent, rows...)
	h.Hourly = buckets
	return h
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
