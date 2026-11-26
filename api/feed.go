package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// feedFields is the part of a fan post the fan feed shows, as of the latest capture of it.
type feedFields struct {
	body                     string
	authorIsArtist           bool
	status                   string
	likeCount, commentCount  int
	images, videos, stickers int
	artistComment            *ArtistComment
	media                    []Media
}

// Media is one attachment. Saved says whether the prototype has downloaded it to images/.
type Media struct {
	Name  string `json:"name"`
	Mime  string `json:"mime"`
	Saved bool   `json:"saved"`
}

// imageFileName mirrors the prototype: the last segment of the attachment key, restricted to a safe charset.
func imageFileName(key string) string {
	var b strings.Builder
	for _, r := range key[strings.LastIndex(key, "/")+1:] {
		if r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

type ArtistComment struct {
	Member string     `json:"member"`
	Body   string     `json:"body"`
	At     *time.Time `json:"at,omitempty"`
}

func feedFieldsOf(p rawPost) feedFields {
	f := feedFields{
		body: strings.TrimSpace(p.Body), authorIsArtist: p.Member.Artist != nil, status: p.Status,
		likeCount: p.LikeCount, commentCount: p.CommentCount,
	}
	for _, a := range p.Attachments {
		if n := imageFileName(a.Key); n != "" {
			f.media = append(f.media, Media{Name: n, Mime: a.Mime})
		}
		if strings.HasPrefix(a.Mime, "video/") {
			f.videos++
		} else {
			f.images++
		}
	}
	for _, b := range p.BodyBlocks {
		if b.Category == "STICKER" {
			f.stickers++
		}
	}
	if lc := p.LastArtistComment; lc != nil && lc.Body != "" {
		c := &ArtistComment{Member: canonicalMember(memberName(lc.Member)), Body: lc.Body}
		if t := parseTime(lc.FirstActivatedAt); !t.IsZero() {
			c.At = &t
		}
		f.artistComment = c
	}
	return f
}

// FeedPost is one fan post as the fan feed shows it.
type FeedPost struct {
	ID             string         `json:"id"`
	URL            string         `json:"url"`
	Author         string         `json:"author"`
	AuthorIsArtist bool           `json:"authorIsArtist"`
	Body           string         `json:"body"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	FirstSeen      time.Time      `json:"firstSeen"` // first capture that contained it
	LastSeen       time.Time      `json:"lastSeen"`  // counts below are as of this capture
	LikeCount      int            `json:"likeCount"`
	CommentCount   int            `json:"commentCount"`
	Images         int            `json:"images"`
	Videos         int            `json:"videos"`
	Stickers       int            `json:"stickers"`
	LikedBy        []string       `json:"likedBy"`
	LateBy         []string       `json:"lateBy"` // the members in LikedBy whose like was a Late like
	ArtistComment  *ArtistComment `json:"artistComment,omitempty"`
	Media          []Media        `json:"media"`
}

type Feed struct {
	Posts         []FeedPost `json:"posts"`
	Total         int        `json:"total"`   // posts matching the filters
	HasMore       bool       `json:"hasMore"` // more posts older than the last one returned
	LastCaptureAt *time.Time `json:"lastCaptureAt,omitempty"`
}

// FeedQuery selects fan posts, newest first. Before is a cursor: only posts created
// strictly before it (ties broken by ID) are returned.
type FeedQuery struct {
	Limit      int
	BeforeTime time.Time
	BeforeID   string
	Search     string
	ArtistOnly bool
	// Member, when set: only posts this member liked or commented on (implies ArtistOnly)
	Member string
	// From/To, when set, keep only posts created in [From, To) (the fan chart's "show this minute"); Hour (and
	// Minute) of day in Loc, when >= 0, the same on any day. With ArtistOnly a post also matches when its artist
	// comment was written then: the time the artist acted (a like counts at its post's time).
	From, To     time.Time
	Hour, Minute *int // nil = any
	Loc          *time.Location
}

// timeMatch: whether t is inside the query's range and hour of day.
func (q FeedQuery) timeMatch(t time.Time) bool {
	w := When{From: tp(q.From), To: tp(q.To), Loc: q.Loc, Hour: -1, Minute: -1}
	if q.Hour != nil {
		w.Hour = *q.Hour
		if q.Minute != nil {
			w.Minute = *q.Minute
		}
	}
	return w.match(t)
}

func (q FeedQuery) timed() bool { return !q.From.IsZero() || !q.To.IsZero() || q.Hour != nil }

func tp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func newer(a, b *postState) bool {
	if !a.createdAt.Equal(b.createdAt) {
		return a.createdAt.After(b.createdAt)
	}
	return a.id > b.id
}

// Feed returns the fan posts seen so far, newest first.
func (s *Store) Feed(q FeedQuery) Feed {
	s.mu.RLock()
	defer s.mu.RUnlock()
	needle := strings.ToLower(strings.TrimSpace(q.Search))
	all := make([]*postState, 0, len(s.posts))
	artistOnly := q.ArtistOnly || q.Member != ""
	for _, p := range s.posts {
		c := p.feed.artistComment
		if artistOnly && len(p.artists) == 0 && c == nil {
			continue
		}
		if q.Member != "" && !p.artists[q.Member] && (c == nil || c.Member != q.Member) {
			continue
		}
		if q.timed() && !q.timeMatch(p.createdAt) && !(artistOnly && c != nil && c.At != nil && q.timeMatch(*c.At)) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(p.feed.body), needle) &&
			!strings.Contains(strings.ToLower(p.author), needle) &&
			!(c != nil && strings.Contains(strings.ToLower(c.Body), needle)) {
			continue
		}
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return newer(all[i], all[j]) })

	out := Feed{Posts: []FeedPost{}, Total: len(all)}
	if !s.lastCapture.IsZero() {
		t := s.lastCapture
		out.LastCaptureAt = &t
	}
	cursor := &postState{createdAt: q.BeforeTime, id: q.BeforeID}
	for _, p := range all {
		if !q.BeforeTime.IsZero() && !newer(cursor, p) {
			continue
		}
		if len(out.Posts) == q.Limit {
			out.HasMore = true
			break
		}
		out.Posts = append(out.Posts, s.toFeedPost(p))
	}
	return out
}

func (s *Store) toFeedPost(p *postState) FeedPost {
	f := p.feed
	fp := FeedPost{
		ID: p.id, URL: "https://app.fans/community/nmixx/fan/" + p.slug, Author: p.author,
		AuthorIsArtist: f.authorIsArtist, Body: f.body, Status: f.status, CreatedAt: p.createdAt,
		FirstSeen: p.firstSeen, LastSeen: p.lastSeen, LikeCount: f.likeCount, CommentCount: f.commentCount,
		Images: f.images, Videos: f.videos, Stickers: f.stickers, LikedBy: []string{}, LateBy: []string{},
		ArtistComment: f.artistComment, Media: []Media{},
	}
	for _, m := range f.media {
		m.Saved = s.imageDir != "" && fileExists(filepath.Join(s.imageDir, m.Name))
		fp.Media = append(fp.Media, m)
	}
	for _, m := range Members {
		if p.artists[m] {
			fp.LikedBy = append(fp.LikedBy, m)
		}
	}
	extra := []string{}
	for m := range p.artists {
		if !contains(Members, m) {
			extra = append(extra, m)
		}
	}
	sort.Strings(extra)
	fp.LikedBy = append(fp.LikedBy, extra...)
	for _, m := range fp.LikedBy {
		if p.lateLikes[m] {
			fp.LateBy = append(fp.LateBy, m)
		}
	}
	return fp
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// FanMatch is one fan whose nickname matches a search, for the search popup's Fans tab.
type FanMatch struct {
	Nickname string    `json:"nickname"`
	Posts    int       `json:"posts"`
	LastAt   time.Time `json:"lastAt"`
}

// Fans returns fans (not artists) whose nickname contains search (case-insensitive), most posts first, and how
// many matched in total.
func (s *Store) Fans(search string, limit int) ([]FanMatch, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	needle := strings.ToLower(strings.TrimSpace(search))
	by := map[string]*FanMatch{}
	for _, p := range s.posts {
		if p.feed.authorIsArtist || p.author == "" || (needle != "" && !strings.Contains(strings.ToLower(p.author), needle)) {
			continue
		}
		f := by[p.author]
		if f == nil {
			f = &FanMatch{Nickname: p.author}
			by[p.author] = f
		}
		f.Posts++
		if p.createdAt.After(f.LastAt) {
			f.LastAt = p.createdAt
		}
	}
	out := make([]FanMatch, 0, len(by))
	for _, f := range by {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Posts != out[j].Posts {
			return out[i].Posts > out[j].Posts
		}
		return out[i].LastAt.After(out[j].LastAt)
	})
	total := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, total
}
