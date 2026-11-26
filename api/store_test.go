package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type tPost struct {
	id, created string
	likes       []string
	comment     *[2]string // member, at
}

func capture(t *testing.T, posts ...tPost) []byte {
	t.Helper()
	objs := []map[string]any{}
	for _, p := range posts {
		la := []any{}
		for _, l := range p.likes {
			la = append(la, map[string]any{"member": map[string]any{"nickname": l, "artist": map[string]any{"code": strings.ToLower(l)}}})
		}
		o := map[string]any{"id": p.id, "slug": "s" + p.id, "body": "demo body", "firstActivatedAt": p.created,
			"member": map[string]any{"nickname": "fan"}, "likedArtists": la, "lastArtistComment": nil}
		if p.comment != nil {
			o["lastArtistComment"] = map[string]any{"body": "hi", "firstActivatedAt": p.comment[1], "member": map[string]any{"nickname": p.comment[0]}}
		}
		objs = append(objs, o)
	}
	b, _ := json.Marshal(map[string]any{"data": map[string]any{"posts": map[string]any{"objects": objs}}})
	return b
}

func at(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

// The 00:05 check saw the post without the like, so the like found at 00:15 is Late; its window starts at 00:05.
func TestLikeSeenWithoutBeforeIsLate(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z"}), at("2026-11-10T00:05:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", likes: []string{"NMIXX BAE"}}), at("2026-11-10T00:15:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", likes: []string{"NMIXX BAE"}}), at("2026-11-10T00:25:00Z"))

	ev := s.Events("", "", "", 10)
	if len(ev) != 1 {
		t.Fatalf("want 1 event, got %d", len(ev))
	}
	e := ev[0]
	if e.Member != "BAE" || e.Kind != "like" || !e.DetectedAt.Equal(at("2026-11-10T00:15:00Z")) {
		t.Fatalf("bad event %+v", e)
	}
	if e.WindowStart == nil || !e.WindowStart.Equal(at("2026-11-10T00:05:00Z")) || !e.Late {
		t.Fatalf("bad window / not late %+v", e)
	}
	if e.At == nil || !e.At.Equal(at("2026-11-10T00:00:00Z")) {
		t.Fatalf("a late like is placed at its post's time too: %v", e.At)
	}
}

// Already liked the first time any check saw the post, even 4 hours after posting: an Instant like at post time.
func TestLikeOnFirstSightingIsInstantAtPostTime(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-09T20:00:00Z", likes: []string{"해원"}}), at("2026-11-10T00:00:00Z"))
	e := s.Events("", "", "", 10)[0]
	if e.Member != "HAEWON" || e.Late || !e.WindowStart.Equal(at("2026-11-09T20:00:00Z")) || !e.At.Equal(at("2026-11-09T20:00:00Z")) {
		t.Fatalf("bad %+v", e)
	}
}

func TestCommentRecordedOncePerDistinctComment(t *testing.T) {
	s := NewStore()
	c1 := [2]string{"LILY", "2026-11-10T00:07:00Z"}
	c2 := [2]string{"JIWOO", "2026-11-10T00:20:00Z"}
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", comment: &c1}), at("2026-11-10T00:10:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", comment: &c1}), at("2026-11-10T00:20:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", comment: &c2}), at("2026-11-10T00:30:00Z"))
	ev := s.Events("", "comment", "", 10)
	if len(ev) != 2 || ev[0].Member != "JIWOO" || !ev[0].CommentAt.Equal(at("2026-11-10T00:20:00Z")) {
		t.Fatalf("bad %+v", ev)
	}
}

func TestEventsSortByActivityTimeAndSearch(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z"}), at("2026-11-10T00:05:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", likes: []string{"NMIXX BAE"}}), at("2026-11-10T00:15:00Z"))
	// the comment was written at 00:07 but only detected at 00:20; the like (found 00:15) sits at its post's time,
	// 00:00, so the comment is the newer one
	c := [2]string{"LILY", "2026-11-10T00:07:00Z"}
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T00:00:00Z", likes: []string{"NMIXX BAE"}, comment: &c}), at("2026-11-10T00:20:00Z"))

	ev := s.Events("", "", "", 10)
	if len(ev) != 2 || ev[0].Kind != "comment" || ev[1].Kind != "like" {
		t.Fatalf("want comment then like, got %+v", ev)
	}
	if got := s.Events("", "", "HI", 10); len(got) != 1 || got[0].Kind != "comment" {
		t.Fatalf("search on comment text: %+v", got)
	}
	if got := s.Events("", "", "bae", 10); len(got) != 1 || got[0].Member != "BAE" {
		t.Fatalf("search on member: %+v", got)
	}
	if got := s.Events("", "", "demo", 1); len(got) != 1 || got[0].Kind != "comment" {
		t.Fatalf("search on post text, limited: %+v", got)
	}
}

// Every like counts in the heatmap at its post's time: an Instant one a day old, and a Late one.
func TestStatsHeatmapCountsEveryLike(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-09T00:00:00Z", likes: []string{"KYUJIN"}}), at("2026-11-10T00:00:00Z"))
	s.ingest(capture(t, tPost{id: "2", created: "2026-11-10T00:05:00Z"}), at("2026-11-10T00:10:00Z"))
	s.ingest(capture(t, tPost{id: "2", created: "2026-11-10T00:05:00Z", likes: []string{"KYUJIN"}}), at("2026-11-10T00:20:00Z"))
	kst, _ := time.LoadLocation("Asia/Seoul")
	st := s.Stats(kst, "")
	var k MemberStats
	for _, m := range st.Members {
		if m.Member == "KYUJIN" {
			k = m
		}
	}
	if k.Likes != 2 || k.Late != 1 {
		t.Fatalf("bad stats %+v", k)
	}
	// posts at Monday 09:00 KST and Tuesday 09:05 KST
	if st.Heatmap[1][9] != 1 || st.Heatmap[2][9] != 1 {
		t.Fatalf("heatmap cells missing: %+v", st.Heatmap)
	}
}

func TestIngestDirIsIncremental(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "2026-11-09T21-13-59-652Z.json"), capture(t, tPost{id: "1", created: "2026-11-09T21:00:00Z"}), 0o644)
	s := NewStore()
	if n, _ := s.IngestDir(dir); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	os.WriteFile(filepath.Join(dir, "2026-11-09T21-25-00-000Z-light.json"), capture(t, tPost{id: "1", created: "2026-11-09T21:00:00Z", likes: []string{"SULLYOON"}}), 0o644)
	if n, _ := s.IngestDir(dir); n != 1 {
		t.Fatalf("want 1 new, got %d", n)
	}
	e := s.Events("SULLYOON", "", "", 10)
	if len(e) != 1 || !e[0].WindowStart.Equal(at("2026-11-09T21:13:59.652Z")) {
		t.Fatalf("bad %+v", e)
	}
}

func TestReadHealth(t *testing.T) {
	dir := t.TempDir()
	lines := `{"at":"2026-11-09T21:13:59.773Z","kind":"pass","headed":true,"tokenTimes":{"exp":"2026-11-10T21:12:08.000Z"},"probe":{"webdriver":true},"capture":{"posts":10},"apiRequests":{"total":18,"statuses":{"200":18},"refresh":[]}}
{"at":"2026-11-09T21:20:40.063Z","kind":"skip","type":"refresh-load","reason":"request budget"}
{"at":"2026-11-09T22:20:00.000Z","kind":"pass","type":"light","tokenAfter":{"exp":"2026-11-10T21:12:08.000Z"},"capture":{"posts":10},"apiRequests":{"total":1,"statuses":{"200":1},"refresh":[{"status":200}]},"cookiesAfter":[{"name":"refreshToken:web","expires":"2026-11-12T21:12:08.398Z"}]}
`
	os.WriteFile(filepath.Join(dir, "log.jsonl"), []byte(lines), 0o644)
	os.WriteFile(filepath.Join(dir, "STOP"), []byte("2026-11-09T22:30:00Z 429 from /graphql\n"), 0o644)
	h := ReadHealth(dir, at("2026-11-09T22:30:00Z"))
	if h.Passes != 2 || h.Skips != 1 || h.RequestsLastHour != 1 || h.RequestsLast24h != 19 {
		t.Fatalf("bad counts %+v", h)
	}
	if h.Stop == "" || h.Webdriver == nil || !*h.Webdriver || len(h.RefreshCalls) != 1 || len(h.RefreshCookieExps) != 1 {
		t.Fatalf("bad health %+v", h)
	}
	if h.Recent[0].Type != "light" || h.Recent[2].Type != "full-load" {
		t.Fatalf("bad order %+v", h.Recent)
	}
}

func TestCaptureTime(t *testing.T) {
	got, err := captureTime("2026-11-09T21-13-59-652Z-refresh-load.json")
	if err != nil || !got.Equal(at("2026-11-09T21:13:59.652Z")) {
		t.Fatalf("got %v %v", got, err)
	}
}

// Shape seen on live data 2026-11-11 (post id, slug and fan made up here): BAE likes under the nickname "뵤".
func TestLiveLikedArtistShapeMapsArtistCodeNotNickname(t *testing.T) {
	raw := []byte(`{"data":{"posts":{"objects":[{"id":"90000001","slug":"demo90000001","likeCount":3,
		"firstActivatedAt":"2026-11-10T18:05:00.657611+00:00","member":{"nickname":"demo_fan","artist":null},
		"lastArtistComment":null,"likedArtists":[{"member":{"id":"72","nickname":"뵤",
		"artist":{"id":"54","code":"bae","__typename":"Artist"},"__typename":"Member"},"__typename":"Artist"}]}]}}}`)
	s := NewStore()
	if err := s.ingest(raw, at("2026-11-10T18:45:00Z")); err != nil {
		t.Fatal(err)
	}
	ev := s.Events("", "", "", 10)
	if len(ev) != 1 || ev[0].Member != "BAE" {
		t.Fatalf("want one BAE like, got %+v", ev)
	}
}

// A like on a post from 00:43, found by the 01:00 check, is placed at 00:43: the 00:00 hour, not 01:00.
func TestLikeCountsAtPostTime(t *testing.T) {
	s := NewStore()
	// 00:43 Bangkok: the post is new; the 01:00 check is the first to see it, already liked
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T17:43:00Z", likes: []string{"BAE"}}), at("2026-11-10T18:00:00Z"))
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	st := s.StatsAt(bkk, "", at("2026-11-10T19:10:00Z"))
	for _, m := range st.Members {
		if m.Member == "BAE" && (m.Hours[0] != 1 || m.Hours[1] != 0) {
			t.Fatalf("want the like in the 00:00 hour: %v", m.Hours)
		}
	}
	e := s.Events("", "", "", 1)[0]
	if e.At == nil || !e.At.Equal(at("2026-11-10T17:43:00Z")) || e.Late {
		t.Fatalf("at %v", e.At)
	}
}

func TestStatsSplitCommentsFromLikes(t *testing.T) {
	s := NewStore()
	c := [2]string{"BAE", "2026-11-10T18:10:00Z"}
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T18:00:00Z"}), at("2026-11-10T18:05:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T18:00:00Z", likes: []string{"BAE"}, comment: &c}), at("2026-11-10T18:15:00Z"))
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	st := s.StatsAt(bkk, "", at("2026-11-10T19:10:00Z"))
	for _, m := range st.Members {
		if m.Member != "BAE" {
			continue
		}
		if m.Hours[1] != 2 || m.HoursComments[1] != 1 || m.TimelineComments[TimelineHours-2] != 1 {
			t.Fatalf("want 1 like + 1 comment at 01:00: hours %v comments %v", m.Hours, m.HoursComments)
		}
	}
	if st.Heatmap[3][1] != 2 || st.HeatmapComments[3][1] != 1 { // Wed 11 Nov, 01:00 Bangkok
		t.Fatalf("heatmap %v / %v", st.Heatmap[3], st.HeatmapComments[3])
	}
}

func TestStatsPerMemberHoursAndTimeline(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T18:00:00Z"}), at("2026-11-10T18:05:00Z"))
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T18:00:00Z", likes: []string{"BAE"}}), at("2026-11-10T18:20:00Z"))
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	st := s.StatsAt(bkk, "", at("2026-11-10T19:10:00Z")) // 02:10 Bangkok
	var bae MemberStats
	for _, m := range st.Members {
		if m.Member == "BAE" {
			bae = m
		}
	}
	// a Late like (window 18:05–18:20), placed at its post's time, 18:00 (01:00 Bangkok)
	if bae.Hours[1] != 1 || bae.Late != 1 || bae.LastAt == nil || !bae.LastAt.Equal(at("2026-11-10T18:00:00Z")) {
		t.Fatalf("hours/lastAt: %+v", bae)
	}
	// timeline ends with the 02:00 Bangkok hour; the like at 01:00 Bangkok is in the one before it
	if len(bae.Timeline) != TimelineHours || bae.Timeline[TimelineHours-2] != 1 || bae.Timeline[TimelineHours-1] != 0 {
		t.Fatalf("timeline tail: %v", bae.Timeline[TimelineHours-3:])
	}
	if !st.TimelineStart.Equal(at("2026-11-10T19:00:00Z").Add(-(TimelineHours - 1) * time.Hour)) {
		t.Fatalf("start %v", st.TimelineStart)
	}
}

func TestMergeSpans(t *testing.T) {
	sp := func(a, b string) Span { return Span{Start: at(a), End: at(b), Count: 1} }
	got := mergeSpans([]Span{
		sp("2026-11-10T18:00:00Z", "2026-11-10T18:40:00Z"),
		sp("2026-11-10T15:00:00Z", "2026-11-10T18:10:00Z"),
		sp("2026-11-10T20:00:00Z", "2026-11-10T20:30:00Z"),
	})
	if len(got) != 2 || got[0].Count != 2 || !got[0].Start.Equal(at("2026-11-10T15:00:00Z")) ||
		!got[0].End.Equal(at("2026-11-10T18:40:00Z")) || got[1].Count != 1 {
		t.Fatalf("%+v", got)
	}
}

// Two captures that overlap cover 17:00–19:00 completely; a third after a gap covers only 21:30–22:00.
// A capture with gap-repair pages lists what it covered; the hole between its two runs stays uncovered.
func TestCaptureCoverageListOverridesOldestPost(t *testing.T) {
	s := NewStore()
	raw := capture(t,
		tPost{id: "3", created: "2026-11-10T18:50:00Z"},
		tPost{id: "2", created: "2026-11-10T18:40:00Z"}, // top run reaches back to 18:40
		tPost{id: "1", created: "2026-11-10T17:05:00Z"}) // repair page: 17:05 up to 17:30
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["coverage"] = []map[string]string{
		{"from": "2026-11-10T18:40:00Z", "to": "2026-11-10T19:00:00Z"},
		{"from": "2026-11-10T17:05:00Z", "to": "2026-11-10T17:30:00Z"},
	}
	raw, _ = json.Marshal(m)
	s.ingest(raw, at("2026-11-10T19:00:00Z"))
	st := s.StatsAt(time.UTC, "", at("2026-11-10T19:10:00Z"))
	idx := func(h string) int { return int(at(h).Sub(st.TimelineStart) / time.Hour) }
	c := st.Fans.Coverage
	if c[idx("2026-11-10T17:00:00Z")] != 25.0/60 || c[idx("2026-11-10T18:00:00Z")] != 20.0/60 {
		t.Fatalf("coverage 17h %v 18h %v, want the 17:30-18:40 hole left out", c[idx("2026-11-10T17:00:00Z")], c[idx("2026-11-10T18:00:00Z")])
	}
}

// Hours captured at least 80% count towards the averages (as seen); less, or not over yet, they don't.
func TestHourAverageTakesMostlyCapturedHours(t *testing.T) {
	s := NewStore()
	// 18:06-19:00 seen (90% of the 18:00 hour), 20:30-21:00 seen (50% of 20:00), 21:00-21:40 seen (21:00 in progress)
	s.ingest(capture(t, tPost{id: "2", created: "2026-11-10T18:30:00Z"}, tPost{id: "1", created: "2026-11-10T18:06:00Z"}), at("2026-11-10T19:00:00Z"))
	s.ingest(capture(t, tPost{id: "3", created: "2026-11-10T20:30:00Z"}), at("2026-11-10T21:00:00Z"))
	s.ingest(capture(t, tPost{id: "4", created: "2026-11-10T21:00:00Z"}), at("2026-11-10T21:40:00Z"))
	f := s.StatsAt(time.UTC, "", at("2026-11-10T21:45:00Z")).Fans
	if f.HourN[18] != 1 || f.HourPartial[18] != 1 || f.HourAvg[18] != 2 {
		t.Fatalf("18:00 (90%% captured) should count: n=%v partial=%v avg=%v", f.HourN[18], f.HourPartial[18], f.HourAvg[18])
	}
	if f.HourN[20] != 0 || f.HourN[21] != 0 {
		t.Fatalf("20:00 (50%%) and 21:00 (in progress) should not count: %v", f.HourN)
	}
}

func TestFanMinutesRealHourAndAverage(t *testing.T) {
	s := NewStore()
	// 18:06-19:00 captured; posts at 18:06 and 18:30 (same fan in the test helper)
	s.ingest(capture(t, tPost{id: "2", created: "2026-11-10T18:30:00Z"}, tPost{id: "1", created: "2026-11-10T18:06:00Z"}), at("2026-11-10T19:00:00Z"))
	m := s.FanMinutesAt(time.UTC, at("2026-11-10T18:20:00Z"))
	if m.Start == nil || !m.Start.Equal(at("2026-11-10T18:00:00Z")) || m.Posts[6] != 1 || m.Posts[30] != 1 || m.Fans[30] != 1 {
		t.Fatalf("real hour: %+v", m)
	}
	if m.Coverage[5] != 0 || m.Coverage[6] != 1 {
		t.Fatalf("coverage minute 5=%v 6=%v", m.Coverage[5], m.Coverage[6])
	}
	a := s.FanMinutesAvg(time.UTC, 18, at("2026-11-10T21:00:00Z"))
	if a.Start != nil || a.Hours != 1 || a.Posts[30] != 1 {
		t.Fatalf("average: %+v", a)
	}
}

func TestFanActivityCountsPostsAndCoverage(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t,
		tPost{id: "3", created: "2026-11-10T18:40:00Z"},
		tPost{id: "2", created: "2026-11-10T18:10:00Z"},
		tPost{id: "1", created: "2026-11-10T17:00:00Z"}), at("2026-11-10T18:45:00Z"))
	s.ingest(capture(t,
		tPost{id: "4", created: "2026-11-10T18:55:00Z"},
		tPost{id: "3", created: "2026-11-10T18:40:00Z"}), at("2026-11-10T19:00:00Z"))
	s.ingest(capture(t, tPost{id: "5", created: "2026-11-10T21:30:00Z"}), at("2026-11-10T22:00:00Z"))
	utc := time.UTC
	st := s.StatsAt(utc, "", at("2026-11-10T22:10:00Z"))
	f := st.Fans
	idx := func(h string) int { return int(at(h).Sub(st.TimelineStart) / time.Hour) }
	if f.Timeline[idx("2026-11-10T18:00:00Z")] != 3 || f.Timeline[idx("2026-11-10T17:00:00Z")] != 1 {
		t.Fatalf("counts: %v", f.Timeline[idx("2026-11-10T17:00:00Z"):])
	}
	if f.Coverage[idx("2026-11-10T17:00:00Z")] != 1 || f.Coverage[idx("2026-11-10T18:00:00Z")] != 1 ||
		f.Coverage[idx("2026-11-10T19:00:00Z")] != 0 || f.Coverage[idx("2026-11-10T21:00:00Z")] != 0.5 {
		t.Fatalf("coverage: %v", f.Coverage[idx("2026-11-10T17:00:00Z"):])
	}
	// only 17:00 and 18:00 are whole covered hours
	if f.HourN[17] != 1 || f.HourN[18] != 1 || f.HourN[21] != 0 || f.HourAvg[18] != 3 {
		t.Fatalf("hour avg: n=%v avg=%v", f.HourN, f.HourAvg)
	}
	// every test post is by the same fan: 3 posts at 18:00, but 1 person
	if f.People[idx("2026-11-10T18:00:00Z")] != 1 || f.PeopleAvg[18] != 1 || f.People[idx("2026-11-10T19:00:00Z")] != 0 {
		t.Fatalf("people: %v avg %v", f.People[idx("2026-11-10T17:00:00Z"):], f.PeopleAvg)
	}
}

func TestFanMinutesArtistMarks(t *testing.T) {
	s := NewStore()
	tp := func(v string) *time.Time { x := at(v); return &x }
	s.events = append(s.events,
		Event{Kind: "comment", Member: "LILY", CommentAt: tp("2026-11-10T18:07:30Z"), DetectedAt: at("2026-11-10T18:15:00Z")},
		Event{Kind: "like", Member: "BAE", Late: true, PostCreatedAt: at("2026-11-10T18:25:00Z"), WindowStart: tp("2026-11-10T18:28:00Z"), DetectedAt: at("2026-11-10T18:40:00Z")},
		Event{Kind: "like", Member: "BAE", PostCreatedAt: at("2026-11-10T17:10:00Z"), WindowStart: tp("2026-11-10T17:10:00Z"), DetectedAt: at("2026-11-10T18:30:00Z")},
		Event{Kind: "comment", Member: "HAEWON", CommentAt: tp("2026-11-11T18:45:00Z"), DetectedAt: at("2026-11-11T19:00:00Z")},
	)
	m := s.FanMinutesAt(time.UTC, at("2026-11-10T18:00:00Z"))
	if len(m.Artist) != 2 || m.Artist[0].Minute != 7 || m.Artist[1].Member != "BAE" || m.Artist[1].Minute != 25 || !m.Artist[1].Late {
		t.Fatalf("real hour: want Lily :07 and Bae's late like at its post's :25 (the 17:10 one is another hour), got %+v", m.Artist)
	}
	a := s.FanMinutesAvg(time.UTC, 18, at("2026-11-12T00:00:00Z"))
	if len(a.Artist) != 3 {
		t.Fatalf("hour of day: want the 3 events at 18:xx from both days, got %+v", a.Artist)
	}
}

func TestEventsWhenMatchesTheArtistStrip(t *testing.T) {
	s := NewStore()
	tp := func(v string) *time.Time { x := at(v); return &x }
	s.events = append(s.events,
		Event{ID: "a", Kind: "comment", Member: "LILY", CommentAt: tp("2026-11-10T18:07:30Z"), DetectedAt: at("2026-11-10T18:15:00Z")},
		Event{ID: "b", Kind: "like", Member: "BAE", PostCreatedAt: at("2026-11-10T18:25:00Z"), DetectedAt: at("2026-11-10T18:30:00Z")},
		Event{ID: "c", Kind: "like", Member: "BAE", PostCreatedAt: at("2026-11-10T17:10:00Z"), DetectedAt: at("2026-11-10T18:30:00Z")},
		Event{ID: "d", Kind: "comment", Member: "HAEWON", CommentAt: tp("2026-11-11T18:07:00Z"), DetectedAt: at("2026-11-11T19:00:00Z")},
	)
	ids := func(ev []Event) (s string) {
		for _, e := range ev {
			s += e.ID
		}
		return
	}
	if got := ids(s.EventsWhen("", "", "", 10, When{From: tp("2026-11-10T18:00:00Z"), To: tp("2026-11-10T19:00:00Z"), Hour: -1, Minute: -1})); got != "ba" {
		t.Fatalf("real hour: want b, a (newest first; c's post is from 17:10), got %q", got)
	}
	if got := ids(s.EventsWhen("", "", "", 10, When{Loc: time.UTC, Hour: 18, Minute: 7})); got != "da" {
		t.Fatalf("18:07 on any day: want d, a, got %q", got)
	}
	if got := ids(s.Events("", "", "", 10)); got != "dbac" {
		t.Fatalf("no time filter keeps everything, got %q", got)
	}
}

func TestFanDays(t *testing.T) {
	s := NewStore()
	tp := func(v string) *time.Time { x := at(v); return &x }
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	// captures saw 2026-11-10 20:00 to 2026-11-11 02:00 Bangkok (13:00Z to 19:00Z)
	s.covered = []Span{{Start: at("2026-11-10T13:00:00Z"), End: at("2026-11-10T19:00:00Z")}}
	s.posts["1"] = &postState{author: "a", createdAt: at("2026-11-10T13:30:00Z")}
	s.posts["2"] = &postState{author: "a", createdAt: at("2026-11-10T13:40:00Z")}
	s.posts["3"] = &postState{author: "b", createdAt: at("2026-11-10T17:10:00Z")} // 00:10 on the 11th, Bangkok
	s.posts["4"] = &postState{author: "x", createdAt: at("2026-11-10T13:50:00Z"), feed: feedFields{authorIsArtist: true}}
	s.events = append(s.events,
		Event{Kind: "comment", Member: "LILY", CommentAt: tp("2026-11-10T13:35:00Z"), DetectedAt: at("2026-11-10T13:45:00Z")},
		Event{Kind: "comment", Member: "BAE", CommentAt: tp("2026-11-10T17:20:00Z"), DetectedAt: at("2026-11-10T17:30:00Z")},
		Event{Kind: "like", Member: "BAE", PostCreatedAt: at("2026-11-10T17:25:00Z"), DetectedAt: at("2026-11-10T17:30:00Z")}, // 00:25 on the 11th
	)
	now := at("2026-11-10T19:00:00Z") // 02:00 on the 11th, Bangkok
	days := s.FanDays(bkk, now)
	if len(days) != 2 || days[0].Date != "2026-11-10" || days[1].Date != "2026-11-11" {
		t.Fatalf("want the 10th and 11th, got %+v", days)
	}
	if d := days[0]; d.Posts != 2 || d.Fans != 1 || d.Artist["LILY"] != 1 || d.Coverage < 0.166 || d.Coverage > 0.167 {
		t.Fatalf("10th: want 2 posts from 1 fan, Lily 1, 4 of 24 hours seen, got %+v", d)
	}
	if d := days[1]; d.Posts != 1 || d.Artist["BAE"] != 2 || d.Coverage != 1 {
		t.Fatalf("11th: want 1 post, Bae 2 (a comment and a like at its post's time), all of the day so far seen, got %+v", d)
	}
	day, err := s.FanDayAt(bkk, "2026-11-10", now)
	if err != nil || len(day.Hours) != 24 || day.Hours[20].Posts != 2 || day.Hours[20].Fans != 1 || day.Hours[20].Artist["LILY"] != 1 ||
		day.Hours[20].Coverage != 1 || day.Hours[19].Coverage != 0 || day.Posts != 2 {
		t.Fatalf("10th by hour: want 2 posts and Lily at 20:00, got %+v (%v)", day, err)
	}
	if _, err := s.FanDayAt(bkk, "nope", now); err == nil {
		t.Fatal("want an error for a bad date")
	}
}

func TestStatsMonth(t *testing.T) {
	s := NewStore()
	tp := func(v string) *time.Time { x := at(v); return &x }
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	s.covered = []Span{{Start: at("2026-10-31T10:00:00Z"), End: at("2026-11-03T10:00:00Z")}}
	s.events = append(s.events,
		Event{Kind: "comment", Member: "LILY", CommentAt: tp("2026-10-31T16:30:00Z"), DetectedAt: at("2026-10-31T16:40:00Z")}, // 23:30 Oct 31 BKK
		Event{Kind: "comment", Member: "LILY", CommentAt: tp("2026-10-31T17:30:00Z"), DetectedAt: at("2026-10-31T17:40:00Z")}, // 00:30 Nov 1 BKK
		Event{Kind: "comment", Member: "BAE", CommentAt: tp("2026-11-02T03:00:00Z"), DetectedAt: at("2026-11-02T03:10:00Z")},
	)
	now := at("2026-11-03T04:20:00Z") // 11:20 Nov 3 BKK
	st, err := s.StatsMonth(bkk, "", now, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	if !st.TimelineStart.Equal(at("2026-10-31T17:00:00Z")) || st.TimelineHours != 2*24+12 || st.Month != "2026-11" {
		t.Fatalf("want Nov 1 00:00 BKK, 60 hours up to the current one, got %v %d %q", st.TimelineStart, st.TimelineHours, st.Month)
	}
	heat := 0
	for _, row := range st.Heatmap {
		for _, v := range row {
			heat += v
		}
	}
	lily := st.Members[0]
	if heat != 2 || lily.Timeline[0] != 1 || len(st.Fans.Timeline) != 60 || lily.Comments != 2 {
		t.Fatalf("want 2 November events in the heatmap, Lily at hour 0, all-time totals kept; got heat %d, %+v", heat, lily)
	}
	if st.DataFrom == nil || !st.DataFrom.Equal(at("2026-10-31T10:00:00Z")) {
		t.Fatalf("dataFrom: %v", st.DataFrom)
	}
	if _, err := s.StatsMonth(bkk, "", now, "2026-12"); err == nil {
		t.Fatal("want an error for a month not started yet")
	}
}
