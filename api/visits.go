package main

// Visits: a small visitor counter for the Admin page. The web app reports each page view (from a script in the
// page, so most bots, which don't run scripts, are never counted; admins aren't counted either).
//
// Privacy: no IP address is ever stored. A visitor is counted once a day by sha256(daily random salt, IP, browser),
// cut to 8 bytes. The salt and those codes exist only for the current day and are thrown away at midnight, after
// which nobody (this server included) can tell who visited. Kept for 90 days: per day the number of visitors,
// signed-in visitors and page views, the pages viewed, and which sites linked here (host names only).
//
// Unique visitors over 7 and 30 days: the daily code can't recognise anyone on another day, on purpose. Instead the
// browser keeps the day of its own last visit (localStorage, as this server told it) and sends it along. On a
// visitor's first view of a day the server only adds one to that day's Back[gap], gap being how many days ago that
// last visit was (0: none known, a new visitor). A visitor belongs in a window's unique count on the first day they
// show up in it, i.e. when the gap reaches back past the window's first day, so the totals come from these per-day
// tallies alone and nothing about any one visitor is kept. A browser that clears its storage counts as new again.
// BackMembers is the same tally for the browsers that were signed in at that first view.
//
// Signed-in accounts are known to the site anyway, so for them (only) the day of each account's last visit is kept
// (AccountLast), and AccountBack tallies their first view of a day the same way: unique accounts over 7, 30 days and
// all time are exact, whatever browser they use.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	visitsKeepDays   = 90
	visitsMaxKeys    = 40     // distinct pages or sites kept per day; the rest count as "other"
	visitsMaxPerDay  = 300    // page views counted per visitor per day (a stuck reload loop stays a blip)
	visitsMaxSeen    = 200000 // visitors remembered per day
	visitsOnlineFor  = 5 * time.Minute
	visitsSaveEvery  = 30 * time.Second
	visitsTopN       = 10
	visitsSummaryDay = 30
	visitsBackLen    = 31 // Back[0] new, Back[1..29] last visit that many days ago, Back[30] 30 days or more
)

var (
	botRe       = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|preview|headless|lighthouse|facebookexternalhit|curl|wget|python|go-http|java/|okhttp`)
	visitPathRe = regexp.MustCompile(`^/[a-z0-9/_-]{0,60}$`)
	hostRe      = regexp.MustCompile(`^[a-z0-9.-]{1,80}$`)
)

type VisitDay struct {
	Day      string         `json:"day"` // YYYY-MM-DD in the counter's time zone
	Visitors int            `json:"visitors"`
	Members  int            `json:"members"` // visitors who were signed in
	Views    int            `json:"views"`
	Pages    map[string]int `json:"pages,omitempty"`
	Refs     map[string]int `json:"refs,omitempty"`
	Back     []int          `json:"back,omitempty"` // first views of the day by how many days ago the last visit was
	// BackMembers: the part of Back whose first view of the day was signed in
	BackMembers []int `json:"backMembers,omitempty"`
	// AccountBack: each signed-in account's first view of the day, by how many days ago that account last visited
	AccountBack []int `json:"accountBack,omitempty"`
}

// today's private part: only in this file until midnight
type visitToday struct {
	Day      string          `json:"day"`
	Salt     string          `json:"salt"`
	Seen     map[string]int  `json:"seen"`               // code -> views today
	Members  map[string]bool `json:"members"`            // codes already counted as signed in
	Accounts map[string]bool `json:"accounts,omitempty"` // accounts already counted today
}

// tally adds one to back[gap], growing back to visitsBackLen first.
func tally(back *[]int, gap int) {
	if len(*back) != visitsBackLen {
		*back = append(*back, make([]int, visitsBackLen)...)[:visitsBackLen]
	}
	(*back)[gap]++
}

// VisitTotals: all-time running totals, never dropped (the daily rows go after visitsKeepDays). The first time
// they are made from the days on file, then every counted view adds to them. Nothing about any one visitor is kept.
type VisitTotals struct {
	Since    string `json:"since"`    // the first day counted
	Visitors int    `json:"visitors"` // the daily visitors added up
	Views    int    `json:"views"`
	// Unique: browsers with no earlier visit known (each person once, as in the 7/30-day unique counts), counted
	// since UniqueSince, the first day that tally existed
	Unique      int    `json:"unique"`
	UniqueSince string `json:"uniqueSince,omitempty"`
	// UniqueMembers: the part of Unique that was signed in at its first view
	UniqueMembers int `json:"uniqueMembers"`
	// Accounts: distinct signed-in accounts that visited, since AccountsSince
	Accounts      int    `json:"accounts"`
	AccountsSince string `json:"accountsSince,omitempty"`
}

type visitsFile struct {
	Days   []VisitDay   `json:"days"`
	Today  *visitToday  `json:"today,omitempty"`
	Totals *VisitTotals `json:"totals,omitempty"`
	// AccountLast: each signed-in account's last visit day (accounts only, never anonymous visitors)
	AccountLast map[string]string `json:"accountLast,omitempty"`
}

// totalsLocked returns the all-time totals, making them from the days on file the first time.
func (v *Visits) totalsLocked() *VisitTotals {
	if v.f.Totals != nil {
		return v.f.Totals
	}
	t := &VisitTotals{}
	for _, d := range v.f.Days { // oldest first
		if t.Since == "" {
			t.Since = d.Day
		}
		t.Visitors += d.Visitors
		t.Views += d.Views
		if len(d.Back) > 0 {
			if t.UniqueSince == "" {
				t.UniqueSince = d.Day
			}
			t.Unique += d.Back[0]
		}
		if len(d.BackMembers) > 0 {
			t.UniqueMembers += d.BackMembers[0]
		}
	}
	v.f.Totals = t
	v.dirty = true
	return t
}

type Visits struct {
	mu     sync.Mutex
	file   string
	loc    *time.Location
	f      visitsFile
	online map[string]onlineVisit // code -> last view (memory only)
	dirty  bool
}

func NewVisits(file, tz string) (*Visits, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, err
	}
	v := &Visits{file: file, loc: loc, online: map[string]onlineVisit{}}
	if err := loadJSON(file, &v.f); err != nil {
		return v, err
	}
	go func() {
		for range time.Tick(visitsSaveEvery) {
			v.flush()
		}
	}()
	return v, nil
}

func (v *Visits) flush() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dirty {
		v.dirty = saveJSON(v.file, v.f) != nil
	}
}

// todayLocked returns today's stats row and private part, starting a new day (and a new salt) when needed.
func (v *Visits) todayLocked(now time.Time) (*VisitDay, *visitToday) {
	day := now.In(v.loc).Format("2006-01-02")
	if v.f.Today == nil || v.f.Today.Day != day {
		salt := make([]byte, 16)
		rand.Read(salt)
		v.f.Today = &visitToday{Day: day, Salt: hex.EncodeToString(salt), Seen: map[string]int{}, Members: map[string]bool{}}
		cut := now.In(v.loc).AddDate(0, 0, -visitsKeepDays).Format("2006-01-02")
		keep := v.f.Days[:0]
		for _, d := range v.f.Days {
			if d.Day > cut {
				keep = append(keep, d)
			}
		}
		v.f.Days = keep
	}
	if n := len(v.f.Days); n == 0 || v.f.Days[n-1].Day != day {
		v.f.Days = append(v.f.Days, VisitDay{Day: day})
	}
	return &v.f.Days[len(v.f.Days)-1], v.f.Today
}

func bump(m *map[string]int, key string) {
	if *m == nil {
		*m = map[string]int{}
	}
	if _, ok := (*m)[key]; !ok && len(*m) >= visitsMaxKeys {
		key = "other"
	}
	(*m)[key]++
}

// backGap says how many days before today (YYYY-MM-DD) the browser's last visit was: 0 for none known, at most
// visitsBackLen-1. ok is false when the browser was already counted today (or names a day after today).
func backGap(last, today string) (gap int, ok bool) {
	l, err1 := time.Parse("2006-01-02", last)
	t, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return 0, true
	}
	days := int(t.Sub(l).Hours()+12) / 24 // both are UTC midnights; +12 h rounds
	if days <= 0 {
		return 0, false
	}
	return min(days, visitsBackLen-1), true
}

// Hit counts one page view. ip and ua identify the visitor for today only; path is the page, ref the host name of
// the site that linked here ("" for none or this site); account is the signed-in account ("" for none) and member
// whether they were signed in; last is the day of the browser's previous visit as this server told it ("" for none).
// Hit returns today's day for the browser to keep ("" when the view wasn't counted).
func (v *Visits) Hit(ip, ua, path, ref, last, account string, member bool, now time.Time) string {
	if ua == "" || botRe.MatchString(ua) || !visitPathRe.MatchString(path) {
		return ""
	}
	if len(account) > 64 {
		account = ""
	}
	member = member || account != ""
	v.mu.Lock()
	defer v.mu.Unlock()
	tot := v.totalsLocked() // before todayLocked adds today's row, so a first run doesn't count this view twice
	d, t := v.todayLocked(now)
	if tot.Since == "" {
		tot.Since = d.Day
	}
	sum := sha256.Sum256([]byte(t.Salt + "\x00" + ip + "\x00" + ua))
	code := hex.EncodeToString(sum[:8])
	n, seen := t.Seen[code]
	if !seen && len(t.Seen) >= visitsMaxSeen {
		return ""
	}
	if n >= visitsMaxPerDay {
		return d.Day
	}
	t.Seen[code] = n + 1
	if !seen {
		d.Visitors++
		tot.Visitors++
		if ref = strings.ToLower(ref); ref != "" && hostRe.MatchString(ref) {
			bump(&d.Refs, strings.TrimPrefix(ref, "www."))
		}
		// only along with a new daily code, so a browser without storage counts no more often than daily visitors do
		if gap, ok := backGap(last, d.Day); ok {
			tally(&d.Back, gap)
			if member {
				tally(&d.BackMembers, gap)
			}
			if gap == 0 {
				tot.Unique++
				if member {
					tot.UniqueMembers++
				}
				if tot.UniqueSince == "" {
					tot.UniqueSince = d.Day
				}
			}
		}
	}
	// a signed-in account's first view of the day, whichever browser it comes from
	if account != "" && !t.Accounts[account] {
		if t.Accounts == nil {
			t.Accounts = map[string]bool{}
		}
		t.Accounts[account] = true
		if v.f.AccountLast == nil {
			v.f.AccountLast = map[string]string{}
		}
		prev := v.f.AccountLast[account]
		if gap, ok := backGap(prev, d.Day); ok {
			tally(&d.AccountBack, gap)
		}
		if prev == "" {
			tot.Accounts++
			if tot.AccountsSince == "" {
				tot.AccountsSince = d.Day
			}
		}
		v.f.AccountLast[account] = d.Day
	}
	if member && !t.Members[code] {
		t.Members[code] = true
		d.Members++
	}
	d.Views++
	tot.Views++
	bump(&d.Pages, path)
	v.online[code] = onlineVisit{at: now, member: member}
	if len(v.online) > 5000 {
		for c, o := range v.online {
			if now.Sub(o.at) > visitsOnlineFor {
				delete(v.online, c)
			}
		}
	}
	v.dirty = true
	return d.Day
}

// onlineVisit: a visitor's latest page view, for "online now" (memory only, gone after visitsOnlineFor).
type onlineVisit struct {
	at     time.Time
	member bool // signed in at that view
}

type VisitCount struct {
	Day      string `json:"day"`
	Visitors int    `json:"visitors"`
	Members  int    `json:"members"`
	Views    int    `json:"views"`
	New      int    `json:"new"` // visitors whose browser had no earlier visit
}

// VisitUnique: distinct browsers over the last Days days (today included), and how many of them were new.
type VisitUnique struct {
	Days     int `json:"days"`
	Unique   int `json:"unique"`
	New      int `json:"new"`
	Visitors int `json:"visitors"` // the daily visitors added up, for comparison
	Members  int `json:"members"`  // the part of Unique signed in at its first view of the day
	Accounts int `json:"accounts"` // distinct signed-in accounts (exact, any browser)
}

type VisitTop struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type VisitSummary struct {
	TZ     string `json:"tz"`
	Online int    `json:"online"` // visitors with a page view in the last 5 minutes
	// OnlineMembers: the Online visitors who were signed in at their latest view (the rest weren't)
	OnlineMembers int           `json:"onlineMembers"`
	Days          []VisitCount  `json:"days"`   // the last 30 days, oldest first, today last (days without visits included)
	Pages         []VisitTop    `json:"pages"`  // the last 7 days
	Refs          []VisitTop    `json:"refs"`   // the last 7 days: visitors arriving from another site
	Unique        []VisitUnique `json:"unique"` // the last 7 and 30 days
	AllTime       VisitTotals   `json:"allTime"`
}

func top(m map[string]int) []VisitTop {
	out := make([]VisitTop, 0, len(m))
	for k, n := range m {
		out = append(out, VisitTop{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out[:min(len(out), visitsTopN)]
}

func (v *Visits) Summary(now time.Time) VisitSummary {
	v.mu.Lock()
	defer v.mu.Unlock()
	tot := *v.totalsLocked()
	v.todayLocked(now)
	s := VisitSummary{TZ: v.loc.String(), AllTime: tot}
	for code, o := range v.online {
		if now.Sub(o.at) > visitsOnlineFor {
			delete(v.online, code)
		} else {
			s.Online++
			if o.member {
				s.OnlineMembers++
			}
		}
	}
	byDay := map[string]VisitDay{}
	for _, d := range v.f.Days {
		byDay[d.Day] = d
	}
	local := now.In(v.loc)
	pages, refs := map[string]int{}, map[string]int{}
	for i := visitsSummaryDay - 1; i >= 0; i-- {
		day := local.AddDate(0, 0, -i).Format("2006-01-02")
		d := byDay[day]
		c := VisitCount{Day: day, Visitors: d.Visitors, Members: d.Members, Views: d.Views}
		if len(d.Back) > 0 {
			c.New = d.Back[0]
		}
		s.Days = append(s.Days, c)
		if i < 7 {
			for k, n := range d.Pages {
				pages[k] += n
			}
			for k, n := range d.Refs {
				refs[k] += n
			}
		}
	}
	s.Pages, s.Refs = top(pages), top(refs)
	for _, w := range []int{7, visitsSummaryDay} {
		u := VisitUnique{Days: w}
		for i := 0; i < w; i++ { // i days ago is w-1-i days after the window's first day
			d := byDay[local.AddDate(0, 0, -i).Format("2006-01-02")]
			u.Visitors += d.Visitors
			// the last visit was before the window (or never): this is their first day in it
			firsts := func(back []int) (n int) {
				for gap, c := range back {
					if gap == 0 || gap > w-1-i {
						n += c
					}
				}
				return
			}
			if len(d.Back) > 0 {
				u.New += d.Back[0]
			}
			u.Unique += firsts(d.Back)
			u.Members += firsts(d.BackMembers)
			u.Accounts += firsts(d.AccountBack)
		}
		s.Unique = append(s.Unique, u)
	}
	return s
}
