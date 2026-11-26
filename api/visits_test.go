package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36"

func TestVisits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "visits.json")
	v, err := NewVisits(file, "Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC) // 10:00 in Bangkok

	v.Hit("1.2.3.4", chromeUA, "/", "x.com", "", "", false, now)
	v.Hit("1.2.3.4", chromeUA, "/signup", "x.com", "", "", false, now.Add(time.Minute)) // same visitor: one more view only
	v.Hit("1.2.3.4", chromeUA+" Mobile", "/", "", "", "", true, now.Add(2*time.Minute)) // another browser: another visitor
	v.Hit("5.6.7.8", "Googlebot/2.1 (+http://www.google.com/bot.html)", "/", "", "", "", false, now)
	v.Hit("5.6.7.8", chromeUA, "/../etc/passwd", "", "", "", false, now)
	v.Hit("5.6.7.8", "", "/", "", "", "", false, now)
	v.Hit("9.9.9.9", chromeUA, "/", "WWW.Reddit.com", "", "", false, now.Add(3*time.Minute))

	s := v.Summary(now.Add(4 * time.Minute))
	today := s.Days[len(s.Days)-1]
	if len(s.Days) != 30 || today.Day != "2026-11-23" || today.Visitors != 3 || today.Views != 4 || today.Members != 1 {
		t.Fatalf("today: %+v (%d days)", today, len(s.Days))
	}
	if s.Online != 3 || s.OnlineMembers != 1 { // the mobile browser was signed in
		t.Fatalf("online: %d (%d signed in)", s.Online, s.OnlineMembers)
	}
	if len(s.Refs) != 2 || s.Refs[0] != (VisitTop{"reddit.com", 1}) && s.Refs[0] != (VisitTop{"x.com", 1}) {
		t.Fatalf("refs: %+v", s.Refs)
	}
	if s.Pages[0] != (VisitTop{"/", 3}) || s.Pages[1] != (VisitTop{"/signup", 1}) {
		t.Fatalf("pages: %+v", s.Pages)
	}
	if v.Summary(now.Add(10*time.Minute)).Online != 0 {
		t.Fatal("nobody is online after 5 quiet minutes")
	}

	// a restart the same day doesn't count anyone twice; no IP address is ever written
	v.flush()
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "1.2.3.4") || strings.Contains(string(raw), "9.9.9.9") {
		t.Fatal("an IP address reached the file")
	}
	v2, _ := NewVisits(file, "Asia/Bangkok")
	v2.Hit("1.2.3.4", chromeUA, "/", "", "", "", false, now.Add(time.Hour))
	if d := v2.Summary(now.Add(time.Hour)).Days[29]; d.Visitors != 3 || d.Views != 5 {
		t.Fatalf("after restart: %+v", d)
	}

	// the next day (in Bangkok) starts over with a new salt, and the old day's private part is gone
	salt := v2.f.Today.Salt
	tomorrow := time.Date(2026, 11, 23, 17, 30, 0, 0, time.UTC) // 00:30 on the 24th in Bangkok
	v2.Hit("1.2.3.4", chromeUA, "/", "", "", "", false, tomorrow)
	s2 := v2.Summary(tomorrow)
	if d := s2.Days[29]; d.Day != "2026-11-24" || d.Visitors != 1 {
		t.Fatalf("tomorrow: %+v", d)
	}
	if d := s2.Days[28]; d.Day != "2026-11-23" || d.Visitors != 3 {
		t.Fatalf("yesterday kept: %+v", d)
	}
	if v2.f.Today.Salt == salt || len(v2.f.Today.Seen) != 1 {
		t.Fatal("a new day needs a new salt and an empty list")
	}

	// one visitor can't inflate the views without end
	for i := 0; i < visitsMaxPerDay+50; i++ {
		v2.Hit("7.7.7.7", chromeUA, "/", "", "", "", false, tomorrow.Add(time.Second))
	}
	if d := v2.Summary(tomorrow).Days[29]; d.Views != 1+visitsMaxPerDay {
		t.Fatalf("capped views: %+v", d)
	}
}

func TestVisitsUnique(t *testing.T) {
	v, err := NewVisits(filepath.Join(t.TempDir(), "visits.json"), "Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	ago := func(days int) time.Time { return time.Date(2026, 11, 24-days, 5, 0, 0, 0, time.UTC) } // noon in Bangkok

	// A: first visit 40 days ago, back today (below). Its browser keeps the day the server hands back.
	a := v.Hit("1.1.1.1", chromeUA, "/", "", "", "", false, ago(40))
	if a != "2026-10-15" {
		t.Fatalf("Hit returns the day for the browser to keep: %q", a)
	}

	// B: new 9 days ago, back 3 days ago and today
	b := v.Hit("2.2.2.2", chromeUA, "/", "", "", "", false, ago(9))
	b = v.Hit("2.2.2.2", chromeUA, "/", "", b, "", false, ago(3))
	v.Hit("1.1.1.1", chromeUA, "/", "", a, "", false, ago(0)) // A is back
	v.Hit("2.2.2.2", chromeUA, "/", "", b, "", false, ago(0))

	// C: new today, a second page, then the phone switches network (a new daily code, but the browser says "seen today")
	c := v.Hit("3.3.3.3", chromeUA, "/", "", "", "", false, ago(0))
	v.Hit("3.3.3.3", chromeUA, "/signup", "", c, "", false, ago(0).Add(time.Minute))
	v.Hit("4.4.4.4", chromeUA, "/", "", c, "", false, ago(0).Add(2*time.Minute))

	// a made-up day after today isn't counted as unique; a garbled one counts as a new visitor
	v.Hit("5.5.5.5", chromeUA, "/", "", "2027-01-01", "", false, ago(0))
	v.Hit("6.6.6.6", chromeUA, "/", "", "garbage", "", false, ago(0))

	s := v.Summary(ago(0))
	if d := s.Days[29]; d.Visitors != 6 || d.New != 2 {
		t.Fatalf("today: %+v", d)
	}
	want := []VisitUnique{
		{Days: 7, Unique: 4, New: 2, Visitors: 7},  // A, B, C, the garbled one; B was new before this week
		{Days: 30, Unique: 4, New: 3, Visitors: 8}, // A's first visit is older than 30 days
	}
	if len(s.Unique) != 2 || s.Unique[0] != want[0] || s.Unique[1] != want[1] {
		t.Fatalf("unique: %+v", s.Unique)
	}
}

// All-time totals: made from the days on file the first time, added to on every counted view, and kept when the
// daily rows are dropped after visitsKeepDays.
func TestVisitsAllTime(t *testing.T) {
	file := filepath.Join(t.TempDir(), "visits.json")
	// a file from before totals existed: two old days, one of them with the new-visitor tally
	old := visitsFile{Days: []VisitDay{
		{Day: "2026-11-20", Visitors: 4, Views: 10},
		{Day: "2026-11-21", Visitors: 3, Views: 5, Back: append([]int{2}, make([]int, visitsBackLen-1)...)},
	}}
	if err := saveJSON(file, old); err != nil {
		t.Fatal(err)
	}
	v, err := NewVisits(file, "Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	v.Hit("1.2.3.4", chromeUA, "/", "", "", "", false, now)                        // new browser
	v.Hit("1.2.3.4", chromeUA, "/signup", "", "", "", false, now.Add(time.Minute)) // same visitor, one more view
	v.Hit("5.6.7.8", chromeUA, "/", "", "2026-11-21", "", false, now)              // came back
	a := v.Summary(now.Add(2 * time.Minute)).AllTime
	if a.Since != "2026-11-20" || a.Visitors != 9 || a.Views != 18 || a.Unique != 3 || a.UniqueSince != "2026-11-21" {
		t.Fatalf("all time: %+v", a)
	}
	// 100 days later the old rows are gone, the totals stay and keep counting
	later := now.AddDate(0, 0, 100)
	v.Hit("9.9.9.9", chromeUA, "/", "", "", "", false, later)
	s := v.Summary(later)
	if a := s.AllTime; a.Visitors != 10 || a.Views != 19 || a.Unique != 4 || a.Since != "2026-11-20" {
		t.Fatalf("after the daily rows are dropped: %+v", a)
	}
}

// Unique visitors split by signed in, and unique accounts: exact per account, whatever browser it uses.
func TestVisitsAccounts(t *testing.T) {
	v, err := NewVisits(filepath.Join(t.TempDir(), "visits.json"), "Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	day := func(n int) time.Time { return time.Date(2026, 11, 20+n, 3, 0, 0, 0, time.UTC) } // 10:00 Bangkok
	// day 0: lily signs in on her phone; a guest browses
	v.Hit("1.1.1.1", chromeUA+" Mobile", "/", "", "", "lily", true, day(0))
	v.Hit("2.2.2.2", chromeUA, "/", "", "", "", false, day(0))
	// day 2: lily again from a laptop (a new browser), bae for the first time
	v.Hit("1.1.1.9", chromeUA, "/", "", "", "lily", true, day(2))
	v.Hit("3.3.3.3", chromeUA, "/", "", "", "bae", true, day(2))
	v.Hit("3.3.3.3", chromeUA, "/signup", "", "", "bae", true, day(2).Add(time.Minute)) // same day: counted once
	s := v.Summary(day(2))
	u7 := s.Unique[0]
	if u7.Days != 7 || u7.Unique != 4 || u7.Members != 3 || u7.Accounts != 2 {
		t.Fatalf("7 days: want 4 browsers (3 signed in), 2 accounts; got %+v", u7)
	}
	if a := s.AllTime; a.Unique != 4 || a.UniqueMembers != 3 || a.Accounts != 2 || a.AccountsSince != "2026-11-20" {
		t.Fatalf("all time: %+v", a)
	}
	// 10 days later lily comes back: in the 7-day window she is one account again, all time still 2
	s = v.Summary(day(12))
	v.Hit("1.1.1.1", chromeUA+" Mobile", "/", "", "2026-11-20", "lily", true, day(12))
	s = v.Summary(day(12))
	if u7 := s.Unique[0]; u7.Accounts != 1 || u7.Unique != 1 || u7.Members != 1 || s.AllTime.Accounts != 2 {
		t.Fatalf("later: 7 days %+v, all time %+v", u7, s.AllTime)
	}
}
