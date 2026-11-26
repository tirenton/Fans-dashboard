package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Members is the canonical NMIXX line-up, in display order.
var Members = []string{"LILY", "HAEWON", "SULLYOON", "BAE", "JIWOO", "KYUJIN"}

var memberAliases = map[string]string{
	"릴리": "LILY", "해원": "HAEWON", "설윤": "SULLYOON", "배이": "BAE", "지우": "JIWOO", "규진": "KYUJIN",
}

// canonicalMember maps an artist nickname from FANS to a canonical member name.
// Unknown names are returned upper-cased so they still show up rather than vanish.
func canonicalMember(raw string) string {
	s := strings.TrimSpace(raw)
	up := strings.ToUpper(s)
	for _, m := range Members {
		if up == m || strings.Contains(up, m) {
			return m
		}
	}
	for k, m := range memberAliases {
		if strings.Contains(s, k) {
			return m
		}
	}
	return up
}

// Event is an artist like event or artist comment event: the first time the tracker saw it.
type Event struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"` // "like" | "comment"
	Member        string    `json:"member"`
	PostID        string    `json:"postId"`
	PostURL       string    `json:"postUrl"`
	PostAuthor    string    `json:"postAuthor"`
	PostExcerpt   string    `json:"postExcerpt"`
	PostCreatedAt time.Time `json:"postCreatedAt"`
	DetectedAt    time.Time `json:"detectedAt"`
	// WindowStart, likes: the last check that saw the post without the like, or the post's time if no check saw
	// the post before. The like happened between it and DetectedAt (shown on cards; charts use ActivityAt).
	WindowStart *time.Time `json:"windowStart,omitempty"`
	// Late: an earlier check had seen the post without this like, so the member came back to an older post (often a
	// visit without a Bubble announcement). Other likes are Instant: already there when the post was first seen.
	Late        bool       `json:"late"`
	CommentBody string     `json:"commentBody,omitempty"`
	CommentAt   *time.Time `json:"commentAt,omitempty"` // comments: exact time from FANS
	At          *time.Time `json:"at,omitempty"`        // ActivityAt, filled in by Events
}

type postState struct {
	id, slug, author, excerpt string
	createdAt                 time.Time
	firstSeen, lastSeen       time.Time
	artists                   map[string]bool
	lateLikes                 map[string]bool // members whose like here was a Late like (see Event.Late)
	commentKey                string
	feed                      feedFields // latest raw view of the post, for the fan feed
}

// Store holds everything derived from the capture files. Captures are the source of truth;
// the store is rebuilt from them on start-up and grows as new files appear.
type Store struct {
	mu           sync.RWMutex
	posts        map[string]*postState
	events       []Event
	captures     int
	covered      []Span // per capture: [oldest post in it, capture time]; every fan post in there was seen
	lastFile     string
	lastCapture  time.Time
	ingestErrors []string
	imageDir     string // where the prototype saves post images; "" = none
}

func NewStore() *Store {
	return &Store{posts: map[string]*postState{}}
}

// captureTime parses names like 2026-11-09T21-13-59-652Z.json or 2026-11-09T21-13-59-652Z-light.json.
func captureTime(name string) (time.Time, error) {
	base := filepath.Base(name)
	if len(base) < 24 {
		return time.Time{}, fmt.Errorf("short name %q", base)
	}
	s := base[:24] // 2026-11-09T21-13-59-652Z
	iso := s[:13] + ":" + s[14:16] + ":" + s[17:19] + "." + s[20:23] + "Z"
	return time.Parse(time.RFC3339Nano, iso)
}

// IngestDir processes capture files it has not seen yet, oldest first.
func (s *Store) IngestDir(dir string) (int, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return 0, err
	}
	sort.Strings(files)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, f := range files {
		name := filepath.Base(f)
		if name <= s.lastFile {
			continue
		}
		t, err := captureTime(name)
		if err != nil {
			s.noteError(fmt.Sprintf("%s: %v", name, err))
			s.lastFile = name
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return n, err // maybe still being written; retry next scan
		}
		if err := s.ingest(raw, t); err != nil {
			s.noteError(fmt.Sprintf("%s: %v", name, err))
		}
		s.lastFile = name
		n++
	}
	return n, nil
}

func (s *Store) noteError(e string) {
	s.ingestErrors = append(s.ingestErrors, e)
	if len(s.ingestErrors) > 20 {
		s.ingestErrors = s.ingestErrors[len(s.ingestErrors)-20:]
	}
}

type rawCapture struct {
	Data struct {
		Posts struct {
			Objects []rawPost `json:"objects"`
		} `json:"posts"`
	} `json:"data"`
	// Coverage, when the probe writes it, is exactly which stretches of the feed this capture read in full.
	// Gap-repair pages make a capture non-contiguous, so its oldest post no longer bounds what it saw.
	Coverage []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"coverage"`
}

type rawMember struct {
	Nickname string `json:"nickname"`
	Artist   *struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"artist"`
}

type rawPost struct {
	ID               string    `json:"id"`
	Slug             string    `json:"slug"`
	Body             string    `json:"body"`
	FirstActivatedAt string    `json:"firstActivatedAt"`
	Member           rawMember `json:"member"`
	Status           string    `json:"status"`
	LikeCount        int       `json:"likeCount"`
	CommentCount     int       `json:"commentCount"`
	BodyBlocks       []struct {
		Category string `json:"category"`
	} `json:"bodyBlocks"`
	Attachments []struct {
		Key  string `json:"key"`
		Mime string `json:"mime"`
	} `json:"attachments"`
	// Live shape (confirmed 2026-11-11): {member{nickname:"뵤", artist{code:"bae"}}}. Bare {nickname} or
	// {artist{...}} are still accepted.
	LikedArtists []struct {
		rawMember
		Member *rawMember `json:"member"`
	} `json:"likedArtists"`
	LastArtistComment *struct {
		Body             string    `json:"body"`
		FirstActivatedAt string    `json:"firstActivatedAt"`
		Member           rawMember `json:"member"`
	} `json:"lastArtistComment"`
}

// memberName prefers the artist code ("bae"): artists pick playful nicknames ("뵤" for BAE) that change
// and match no member name. The nickname is only the fallback.
func memberName(m rawMember) string {
	if m.Artist != nil {
		if m.Artist.Code != "" {
			return m.Artist.Code
		}
		if m.Artist.Name != "" {
			return m.Artist.Name
		}
	}
	return m.Nickname
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *Store) ingest(raw []byte, at time.Time) error {
	var c rawCapture
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	at = at.UTC()
	for _, p := range c.Data.Posts.Objects {
		if p.ID == "" {
			continue
		}
		prev := s.posts[p.ID]
		cur := &postState{
			id: p.ID, slug: p.Slug, author: p.Member.Nickname, excerpt: excerpt(p.Body, 160),
			createdAt: parseTime(p.FirstActivatedAt), firstSeen: at, lastSeen: at, artists: map[string]bool{},
			lateLikes: map[string]bool{},
		}
		if prev != nil {
			cur.firstSeen = prev.firstSeen
			cur.commentKey = prev.commentKey
			for m := range prev.lateLikes {
				cur.lateLikes[m] = true
			}
		}
		cur.feed = feedFieldsOf(p)
		for _, la := range p.LikedArtists {
			name := memberName(la.rawMember)
			if la.Member != nil && memberName(*la.Member) != "" {
				name = memberName(*la.Member)
			}
			if name == "" {
				continue
			}
			m := canonicalMember(name)
			cur.artists[m] = true
			if prev != nil && prev.artists[m] {
				continue
			}
			ev := s.baseEvent("like", m, cur, at)
			ws := cur.createdAt
			if prev != nil {
				ws = prev.lastSeen // an earlier capture saw this post without this member's like
				ev.Late = true
				cur.lateLikes[m] = true
			}
			if !ws.IsZero() {
				w := ws
				ev.WindowStart = &w
			}
			s.events = append(s.events, ev)
		}
		if lc := p.LastArtistComment; lc != nil && lc.Body != "" {
			m := canonicalMember(memberName(lc.Member))
			key := m + "|" + lc.FirstActivatedAt
			if key != cur.commentKey {
				ev := s.baseEvent("comment", m, cur, at)
				ev.CommentBody = lc.Body
				if t := parseTime(lc.FirstActivatedAt); !t.IsZero() {
					ev.CommentAt = &t
				}
				ev.ID += "|" + lc.FirstActivatedAt
				s.events = append(s.events, ev)
				cur.commentKey = key
			}
		}
		s.posts[p.ID] = cur
	}
	s.captures++
	// what the capture saw in full: its own coverage list, or (older captures) the newest posts contiguous back to
	// its oldest one
	if len(c.Coverage) > 0 {
		for _, cv := range c.Coverage {
			if f, t := parseTime(cv.From), parseTime(cv.To); !f.IsZero() && t.After(f) {
				s.covered = append(s.covered, Span{Start: f, End: t})
			}
		}
	} else if objs := c.Data.Posts.Objects; len(objs) > 0 {
		if o := parseTime(objs[len(objs)-1].FirstActivatedAt); !o.IsZero() && o.Before(at) {
			s.covered = append(s.covered, Span{Start: o, End: at})
		}
	}
	if at.After(s.lastCapture) {
		s.lastCapture = at
	}
	return nil
}

func (s *Store) baseEvent(kind, member string, p *postState, at time.Time) Event {
	return Event{
		ID: kind + "|" + p.id + "|" + member + "|" + at.Format(time.RFC3339), Kind: kind, Member: member,
		PostID: p.id, PostURL: "https://app.fans/community/nmixx/fan/" + p.slug, PostAuthor: p.author,
		PostExcerpt: p.excerpt, PostCreatedAt: p.createdAt, DetectedAt: at,
	}
}

// ActivityAt is when the artist acted, as placed on every chart: a comment at its exact time from FANS, a like at
// its post's time. Members announce a visit on Bubble, fans rush to post, and the member likes those fresh posts as
// they arrive, so the post's time is the best estimate FANS allows (it never says when a like happened). A Late
// like is placed at its post's time too, although it happened after the check that saw the post without it.
func (e Event) ActivityAt() time.Time {
	if e.CommentAt != nil {
		return *e.CommentAt
	}
	if e.Kind == "like" && !e.PostCreatedAt.IsZero() {
		return e.PostCreatedAt
	}
	return e.DetectedAt
}

// Events returns events by activity time (newest first), filtered by member and kind when given, and by search:
// a case-insensitive match on the post text, the fan's nickname, the comment or the member.
func (s *Store) Events(member, kind, search string, limit int) []Event {
	return s.EventsWhen(member, kind, search, limit, When{Hour: -1, Minute: -1})
}

// When narrows events to when the artist acted, the way the Fan feed's artist strip counts them: a real time
// range [From, To), or an hour of day (and optionally a minute of it) in Loc on any day, matched against
// ActivityAt; the zero range with Hour -1 filters nothing.
type When struct {
	From, To     *time.Time
	Loc          *time.Location
	Hour, Minute int // -1 = any
}

func (w When) active() bool { return w.From != nil || w.To != nil || w.Hour >= 0 }

func (w When) match(at time.Time) bool {
	if w.From != nil && at.Before(*w.From) || w.To != nil && !at.Before(*w.To) {
		return false
	}
	if w.Hour >= 0 {
		loc := w.Loc
		if loc == nil {
			loc = time.UTC
		}
		l := at.In(loc)
		if l.Hour() != w.Hour || w.Minute >= 0 && l.Minute() != w.Minute {
			return false
		}
	}
	return true
}

// EventsWhen is Events narrowed by when (see When).
func (s *Store) EventsWhen(member, kind, search string, limit int, when When) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	needle := strings.ToLower(strings.TrimSpace(search))
	out := []Event{}
	for _, e := range s.events {
		if (member != "" && e.Member != member) || (kind != "" && e.Kind != kind) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.PostExcerpt+"\n"+e.PostAuthor+"\n"+e.CommentBody+"\n"+e.Member), needle) {
			continue
		}
		at := e.ActivityAt()
		if when.active() && !when.match(at) {
			continue
		}
		e.At = &at
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ActivityAt().After(out[j].ActivityAt()) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

type MemberStats struct {
	Member           string     `json:"member"`
	Likes            int        `json:"likes"`
	Comments         int        `json:"comments"`
	Late             int        `json:"late"`             // the Late likes among Likes
	Hours            [24]int    `json:"hours"`            // likes and comments by local hour of day, at ActivityAt
	HoursComments    [24]int    `json:"hoursComments"`    // the comments among Hours (the rest are likes)
	LastAt           *time.Time `json:"lastAt,omitempty"` // latest like or comment
	Timeline         []int      `json:"timeline"`         // likes and comments per local hour, oldest first, from Stats.TimelineStart
	TimelineComments []int      `json:"timelineComments"` // the comments among Timeline
}

// Span is a stretch of time (capture coverage), with a count when spans are merged.
type Span struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Count int       `json:"count"`
}

// mergeSpans joins overlapping or touching windows, adding up their counts.
func mergeSpans(in []Span) []Span {
	sort.Slice(in, func(i, j int) bool { return in[i].Start.Before(in[j].Start) })
	out := []Span{}
	for _, w := range in {
		if n := len(out); n > 0 && !w.Start.After(out[n-1].End) {
			if w.End.After(out[n-1].End) {
				out[n-1].End = w.End
			}
			out[n-1].Count += w.Count
			continue
		}
		out = append(out, w)
	}
	return out
}

// FanActivity is fan posts per hour. Counts are only trustworthy where captures covered the hour:
// Coverage says how much of each hour some capture saw completely (0 = no data, 1 = every post counted).
type FanActivity struct {
	Timeline []int       `json:"timeline"` // posts per hour, aligned with Stats.TimelineStart
	Coverage []float64   `json:"coverage"` // 0..1 per timeline hour
	HourAvg  [24]float64 `json:"hourAvg"`  // average posts per local hour of day, over finished hours covered >= MinAvgCoverage
	HourN    [24]int     `json:"hourN"`    // how many hours each average is based on
	// HourPartial: how many of HourN were not fully covered (their counts are a slight undercount)
	HourPartial [24]int `json:"hourPartial"`
	// People is distinct fans posting per timeline hour; PeopleAvg its hour-of-day average (same hours as HourAvg).
	// One fan posting 17 times in a burst counts once, so these aren't inflated by spam.
	People    []int       `json:"people"`
	PeopleAvg [24]float64 `json:"peopleAvg"`
}

// MinAvgCoverage: an hour counts towards the hour-of-day averages once captures saw at least this much of it.
const MinAvgCoverage = 0.8

// overlap returns how much of [a, b) the merged spans cover.
func overlap(spans []Span, a, b time.Time) time.Duration {
	var d time.Duration
	for _, s := range spans {
		lo, hi := s.Start, s.End
		if lo.Before(a) {
			lo = a
		}
		if hi.After(b) {
			hi = b
		}
		if hi.After(lo) {
			d += hi.Sub(lo)
		}
	}
	return d
}

type avgHour struct {
	at  time.Time // start of the hour
	got float64   // how much of it captures saw (>= MinAvgCoverage)
}

// avgHours lists the hours the hour-of-day averages use: every finished hour captured at least MinAvgCoverage,
// counted as seen. Leaving out every imperfect hour dropped exactly the big bursts (the probe falls behind in
// them); a few unread minutes only undercount slightly. cov must be merged spans.
func avgHours(cov []Span, now time.Time) []avgHour {
	out := []avgHour{}
	seen := map[int64]bool{}
	for _, c := range cov {
		for h := c.Start.Truncate(time.Hour); h.Before(c.End); h = h.Add(time.Hour) {
			k := h.Unix() / 3600
			if seen[k] || h.Add(time.Hour).After(now) {
				continue // counted already, or not over yet
			}
			seen[k] = true
			if got := float64(overlap(cov, h, h.Add(time.Hour))) / float64(time.Hour); got >= MinAvgCoverage {
				out = append(out, avgHour{at: h, got: got})
			}
		}
	}
	return out
}

// fanActivity counts fan posts (not posts by artists) per hour, and how well each hour was covered.
func (s *Store) fanActivity(loc *time.Location, start time.Time, hours int, now time.Time) FanActivity {
	f := FanActivity{Timeline: make([]int, hours), Coverage: make([]float64, hours)}
	perHour := map[int64]int{}             // posts per absolute hour (unix hour number)
	authors := map[int64]map[string]bool{} // distinct posters per absolute hour: spam-proof activity
	for _, p := range s.posts {
		if p.feed.authorIsArtist || p.createdAt.IsZero() {
			continue
		}
		k := p.createdAt.Unix() / 3600
		perHour[k]++
		if authors[k] == nil {
			authors[k] = map[string]bool{}
		}
		authors[k][p.author] = true
	}
	f.People = make([]int, hours)
	cov := mergeSpans(append([]Span{}, s.covered...))
	for i := 0; i < hours; i++ {
		a := start.Add(time.Duration(i) * time.Hour)
		f.Timeline[i] = perHour[a.Unix()/3600]
		f.People[i] = len(authors[a.Unix()/3600])
		f.Coverage[i] = float64(overlap(cov, a, a.Add(time.Hour))) / float64(time.Hour)
	}
	var sum, people [24]int
	for _, ah := range avgHours(cov, now) {
		k := ah.at.Unix() / 3600
		hod := ah.at.In(loc).Hour()
		f.HourN[hod]++
		if ah.got < 0.999 {
			f.HourPartial[hod]++
		}
		sum[hod] += perHour[k]
		people[hod] += len(authors[k])
	}
	for h := range f.HourAvg {
		if f.HourN[h] > 0 {
			f.HourAvg[h] = float64(sum[h]) / float64(f.HourN[h])
			f.PeopleAvg[h] = float64(people[h]) / float64(f.HourN[h])
		}
	}
	return f
}

// TimelineHours is the span of the per-member activity timeline: the last 7 days, hour by hour.
const TimelineHours = 7 * 24

type Stats struct {
	TZ              string        `json:"tz"`
	Members         []MemberStats `json:"members"`
	Heatmap         [7][24]int    `json:"heatmap"`         // [weekday Sun=0][hour], likes and comments at ActivityAt
	HeatmapComments [7][24]int    `json:"heatmapComments"` // the comments among Heatmap
	HeatmapMember   string        `json:"heatmapMember"`
	// HeatmapByMember splits Heatmap by member (always all members, whatever HeatmapMember is), for per-member colour
	HeatmapByMember map[string]*[7][24]int `json:"heatmapByMember"`
	PostsTracked    int                    `json:"postsTracked"`
	PostsWithArtist int                    `json:"postsWithArtist"`
	Captures        int                    `json:"captures"`
	LastCaptureAt   *time.Time             `json:"lastCaptureAt,omitempty"`
	IngestErrors    []string               `json:"ingestErrors"`
	TimelineStart   time.Time              `json:"timelineStart"` // start of the first timeline hour
	TimelineHours   int                    `json:"timelineHours"`
	Month           string                 `json:"month,omitempty"`    // YYYY-MM when the timeline and heatmap cover one month
	DataFrom        *time.Time             `json:"dataFrom,omitempty"` // when captures begin: the first month there is
	Fans            FanActivity            `json:"fans"`
}

// Stats aggregates per member, plus a weekday×hour heatmap in loc. Events are placed at ActivityAt (a like at its
// post's time, a comment at its own time).
func (s *Store) Stats(loc *time.Location, heatMember string) Stats {
	return s.StatsAt(loc, heatMember, time.Now())
}

// StatsAt is Stats with an explicit "now", which anchors the activity timeline.
func (s *Store) StatsAt(loc *time.Location, heatMember string, now time.Time) Stats {
	// the timeline ends with the current local hour; Truncate works on absolute time, which is fine
	// for whole-hour offsets like Bangkok and Seoul
	start := now.In(loc).Truncate(time.Hour).Add(-(TimelineHours - 1) * time.Hour)
	return s.statsWindow(loc, heatMember, now, start, TimelineHours, time.Time{}, time.Time{})
}

// StatsMonth is Stats with the activity timeline and the weekday heatmap covering one local month (YYYY-MM),
// up to the current hour. Totals and hour-of-day charts stay all-time.
func (s *Store) StatsMonth(loc *time.Location, heatMember string, now time.Time, month string) (Stats, error) {
	from, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil {
		return Stats{}, err
	}
	next := from.AddDate(0, 1, 0)
	end := next
	if cur := now.Truncate(time.Hour).Add(time.Hour); cur.Before(end) {
		end = cur
	}
	if !end.After(from) {
		return Stats{}, errors.New("month not started yet")
	}
	st := s.statsWindow(loc, heatMember, now, from, int(end.Sub(from)/time.Hour), from, next)
	st.Month = month
	return st, nil
}

// statsWindow builds Stats with a timeline of the given hours from start; a non-zero [heatFrom, heatTo) limits the
// weekday heatmap to events then.
func (s *Store) statsWindow(loc *time.Location, heatMember string, now, start time.Time, hours int, heatFrom, heatTo time.Time) Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := map[string]*MemberStats{}
	st := Stats{TZ: loc.String(), HeatmapMember: heatMember, Captures: s.captures, IngestErrors: append([]string{}, s.ingestErrors...),
		TimelineStart: start, TimelineHours: hours}
	inHeat := func(at time.Time) bool { return heatFrom.IsZero() || (!at.Before(heatFrom) && at.Before(heatTo)) }
	newMember := func(m string) MemberStats {
		return MemberStats{Member: m, Timeline: make([]int, hours), TimelineComments: make([]int, hours)}
	}
	st.HeatmapByMember = map[string]*[7][24]int{}
	for _, m := range Members {
		st.Members = append(st.Members, newMember(m))
	}
	for i := range st.Members {
		idx[st.Members[i].Member] = &st.Members[i]
	}
	for _, e := range s.events {
		ms := idx[e.Member]
		if ms == nil {
			st.Members = append(st.Members, newMember(e.Member))
			ms = &st.Members[len(st.Members)-1]
			idx = map[string]*MemberStats{}
			for i := range st.Members {
				idx[st.Members[i].Member] = &st.Members[i]
			}
		}
		at := e.ActivityAt()
		if e.Kind == "like" {
			ms.Likes++
			if e.Late {
				ms.Late++
			}
		} else {
			ms.Comments++
		}
		ms.Hours[at.In(loc).Hour()]++
		if e.Kind == "comment" {
			ms.HoursComments[at.In(loc).Hour()]++
		}
		if ms.LastAt == nil || at.After(*ms.LastAt) {
			t := at
			ms.LastAt = &t
		}
		if i := int(at.Sub(start) / time.Hour); at.Compare(start) >= 0 && i < hours {
			ms.Timeline[i]++
			if e.Kind == "comment" {
				ms.TimelineComments[i]++
			}
		}
		if !inHeat(at) {
			continue // the rest is the weekday heatmap
		}
		lt := at.In(loc)
		hm := st.HeatmapByMember[e.Member]
		if hm == nil {
			hm = &[7][24]int{}
			st.HeatmapByMember[e.Member] = hm
		}
		hm[int(lt.Weekday())][lt.Hour()]++
		if heatMember != "" && e.Member != heatMember {
			continue
		}
		st.Heatmap[int(lt.Weekday())][lt.Hour()]++
		if e.Kind == "comment" {
			st.HeatmapComments[int(lt.Weekday())][lt.Hour()]++
		}
	}
	st.Fans = s.fanActivity(loc, start, hours, now)
	for _, c := range s.covered {
		if st.DataFrom == nil || c.Start.Before(*st.DataFrom) {
			t := c.Start
			st.DataFrom = &t
		}
	}
	for _, p := range s.posts {
		st.PostsTracked++
		if len(p.artists) > 0 || p.commentKey != "" {
			st.PostsWithArtist++
		}
	}
	if !s.lastCapture.IsZero() {
		t := s.lastCapture
		st.LastCaptureAt = &t
	}
	return st
}
