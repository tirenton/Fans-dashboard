package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFeedNewestFirstWithCursorAndFilters(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t,
		tPost{id: "3", created: "2026-11-10T00:03:00Z", likes: []string{"KYUJIN"}},
		tPost{id: "2", created: "2026-11-10T00:02:00Z"},
		tPost{id: "1", created: "2026-11-10T00:01:00Z"},
	), at("2026-11-10T00:05:00Z"))
	// post 1 drops off page 1 in the next capture but stays in the feed
	s.ingest(capture(t,
		tPost{id: "4", created: "2026-11-10T00:06:00Z"},
		tPost{id: "3", created: "2026-11-10T00:03:00Z", likes: []string{"KYUJIN"}},
		tPost{id: "2", created: "2026-11-10T00:02:00Z"},
	), at("2026-11-10T00:15:00Z"))

	f := s.Feed(FeedQuery{Limit: 2})
	if f.Total != 4 || !f.HasMore || len(f.Posts) != 2 || f.Posts[0].ID != "4" || f.Posts[1].ID != "3" {
		t.Fatalf("page 1: %+v", f)
	}
	if got := f.Posts[1].LikedBy; len(got) != 1 || got[0] != "KYUJIN" {
		t.Fatalf("likedBy: %v", got)
	}
	if !f.Posts[1].FirstSeen.Equal(at("2026-11-10T00:05:00Z")) || !f.Posts[1].LastSeen.Equal(at("2026-11-10T00:15:00Z")) {
		t.Fatalf("seen times: %+v", f.Posts[1])
	}

	last := f.Posts[1]
	f2 := s.Feed(FeedQuery{Limit: 2, BeforeTime: last.CreatedAt, BeforeID: last.ID})
	if len(f2.Posts) != 2 || f2.Posts[0].ID != "2" || f2.Posts[1].ID != "1" || f2.HasMore {
		t.Fatalf("page 2: %+v", f2)
	}

	if a := s.Feed(FeedQuery{Limit: 10, ArtistOnly: true}); a.Total != 1 || a.Posts[0].ID != "3" {
		t.Fatalf("artist only: %+v", a)
	}
	if q := s.Feed(FeedQuery{Limit: 10, Search: "DEMO"}); q.Total != 4 {
		t.Fatalf("search is case-insensitive over body: %+v", q)
	}
	if q := s.Feed(FeedQuery{Limit: 10, Search: "nothing-like-this"}); q.Total != 0 || len(q.Posts) != 0 {
		t.Fatalf("search miss: %+v", q)
	}
	// one minute: [00:02, 00:03) holds only post 2
	if m := s.Feed(FeedQuery{Limit: 10, From: at("2026-11-10T00:02:00Z"), To: at("2026-11-10T00:03:00Z")}); m.Total != 1 || m.Posts[0].ID != "2" {
		t.Fatalf("time range: %+v", m)
	}
}

func TestFeedMediaSavedFlag(t *testing.T) {
	dir := t.TempDir()
	s := NewStore()
	s.imageDir = dir
	raw := []byte(`{"data":{"posts":{"objects":[{"id":"9","slug":"s9","body":"x","firstActivatedAt":"2026-11-10T00:00:00Z",
	  "member":{"nickname":"fan"},"attachments":[{"key":"co/abc/1111.jpeg","mime":"image/jpeg"},{"key":"co/abc/2222.png","mime":"image/png"}]}]}}}`)
	if err := s.ingest(raw, at("2026-11-10T00:05:00Z")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1111.jpeg"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := s.Feed(FeedQuery{Limit: 1}).Posts[0].Media
	if len(m) != 2 || m[0].Name != "1111.jpeg" || !m[0].Saved || m[1].Name != "2222.png" || m[1].Saved {
		t.Fatalf("media: %+v", m)
	}
}

func TestImageFileName(t *testing.T) {
	for in, want := range map[string]string{"co/gAAA/ab12.jpeg": "ab12.jpeg", "../../etc/passwd": "passwd", "a/b c%2f.png": "bc2f.png", "x": "x"} {
		if got := imageFileName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestFansSearchByNickname(t *testing.T) {
	s := NewStore()
	s.ingest(capture(t, tPost{id: "2", created: "2026-11-10T00:02:00Z"}, tPost{id: "1", created: "2026-11-10T00:01:00Z"}), at("2026-11-10T00:05:00Z"))
	got, total := s.Fans("FA", 10) // every test post is by "fan"
	if total != 1 || len(got) != 1 || got[0].Nickname != "fan" || got[0].Posts != 2 || !got[0].LastAt.Equal(at("2026-11-10T00:02:00Z")) {
		t.Fatalf("fans: %+v total %d", got, total)
	}
	if _, total := s.Fans("nobody", 10); total != 0 {
		t.Fatalf("miss: %d", total)
	}
}

// The Fan feed's artist view (it replaced the Artist activity page): member filter, the time the artist acted
// (a like at its post's time, a comment at its own), comment text in search, and which likes were late.
func TestFeedArtistView(t *testing.T) {
	s := NewStore()
	// post 1 at 18:00: seen without likes, then LILY likes it (late); post 2 at 17:10: BAE comments at 18:30
	c := [2]string{"BAE", "2026-11-10T18:30:00Z"}
	s.ingest(capture(t, tPost{id: "1", created: "2026-11-10T18:00:00Z"}), at("2026-11-10T18:05:00Z"))
	s.ingest(capture(t,
		tPost{id: "1", created: "2026-11-10T18:00:00Z", likes: []string{"LILY"}},
		tPost{id: "2", created: "2026-11-10T17:10:00Z", comment: &c},
		tPost{id: "3", created: "2026-11-10T18:20:00Z"}), at("2026-11-10T18:40:00Z"))
	ids := func(f Feed) (s string) {
		for _, p := range f.Posts {
			s += p.ID
		}
		return
	}
	if f := s.Feed(FeedQuery{Limit: 10, Member: "BAE"}); ids(f) != "2" {
		t.Fatalf("member BAE: %q", ids(f))
	}
	if f := s.Feed(FeedQuery{Limit: 10, Member: "LILY"}); ids(f) != "1" || len(f.Posts[0].LateBy) != 1 || f.Posts[0].LateBy[0] != "LILY" {
		t.Fatalf("member LILY, late: %+v", f.Posts)
	}
	h, m := 18, 30
	// 18:xx on any day: post 1 (its like counts at the post's 18:00) and post 2 (BAE commented at 18:30)
	if f := s.Feed(FeedQuery{Limit: 10, ArtistOnly: true, Hour: &h, Loc: time.UTC}); ids(f) != "12" {
		t.Fatalf("artist at 18h: %q", ids(f))
	}
	if f := s.Feed(FeedQuery{Limit: 10, ArtistOnly: true, Hour: &h, Minute: &m, Loc: time.UTC}); ids(f) != "2" {
		t.Fatalf("artist at 18:30: %q", ids(f))
	}
	// without the artist view, an hour matches the post's own time only
	if f := s.Feed(FeedQuery{Limit: 10, Hour: &h, Loc: time.UTC}); ids(f) != "31" {
		t.Fatalf("all posts at 18h: %q", ids(f))
	}
	if f := s.Feed(FeedQuery{Limit: 10, Search: "hi"}); ids(f) != "2" { // the comment's text in the test helper
		t.Fatalf("search in the artist comment: %q", ids(f))
	}
}
