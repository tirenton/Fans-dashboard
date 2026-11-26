package main

import "time"

// FanMinutes is one hour of fan posts, minute by minute: either one real hour, or (Hours > 1) the minute-by-minute
// average over the hours an hour-of-day average uses.
type FanMinutes struct {
	Start    *time.Time  `json:"start,omitempty"` // the real hour shown; nil for an average
	Hour     int         `json:"hour"`            // local hour of day, 0-23
	Hours    int         `json:"hours"`           // how many hours the numbers come from (1 for a real hour)
	Posts    [60]float64 `json:"posts"`           // posts per minute (average when Hours > 1)
	Fans     [60]float64 `json:"fans"`            // different fans posting per minute (average when Hours > 1)
	Coverage [60]float64 `json:"coverage"`        // 0..1 per minute: how much of it captures saw (average when Hours > 1)
	// Artist: the artists' likes and comments in this hour (a real hour), or at this hour of day on any day
	// (an average: every day, not only the hours the fan average uses).
	Artist []ArtistMark `json:"artist"`
}

// ArtistMark is one artist like or comment, placed at ActivityAt (a like: its post's time).
type ArtistMark struct {
	Member string    `json:"member"`
	Kind   string    `json:"kind"`
	Late   bool      `json:"late,omitempty"` // a Late like (see Event.Late)
	At     time.Time `json:"at"`
	Minute int       `json:"minute"` // local minute of the hour, 0-59
}

// artistMarks lists the artist events whose ActivityAt passes keep. Callers hold s.mu.
func (s *Store) artistMarks(loc *time.Location, keep func(at time.Time) bool) []ArtistMark {
	out := []ArtistMark{}
	for _, e := range s.events {
		at := e.ActivityAt()
		if keep(at) {
			out = append(out, ArtistMark{Member: e.Member, Kind: e.Kind, Late: e.Late, At: at, Minute: at.In(loc).Minute()})
		}
	}
	return out
}

// fanMinute counts fan posts and distinct posters in each minute of [start, start+1h).
func (s *Store) fanMinute(start time.Time, cov []Span, posts, fans, covg *[60]float64) {
	authors := [60]map[string]bool{}
	end := start.Add(time.Hour)
	for _, p := range s.posts {
		if p.feed.authorIsArtist || p.createdAt.Before(start) || !p.createdAt.Before(end) {
			continue
		}
		m := int(p.createdAt.Sub(start) / time.Minute)
		posts[m]++
		if authors[m] == nil {
			authors[m] = map[string]bool{}
		}
		authors[m][p.author] = true
	}
	for m := 0; m < 60; m++ {
		fans[m] += float64(len(authors[m]))
		a := start.Add(time.Duration(m) * time.Minute)
		covg[m] += float64(overlap(cov, a, a.Add(time.Minute))) / float64(time.Minute)
	}
}

// FanMinutesAt is the real hour starting at `start` (truncated to the hour), minute by minute.
func (s *Store) FanMinutesAt(loc *time.Location, start time.Time) FanMinutes {
	s.mu.RLock()
	defer s.mu.RUnlock()
	start = start.Truncate(time.Hour)
	f := FanMinutes{Start: &start, Hour: start.In(loc).Hour(), Hours: 1}
	s.fanMinute(start, mergeSpans(append([]Span{}, s.covered...)), &f.Posts, &f.Fans, &f.Coverage)
	end := start.Add(time.Hour)
	f.Artist = s.artistMarks(loc, func(at time.Time) bool { return !at.Before(start) && at.Before(end) })
	return f
}

// FanMinutesAvg averages, minute by minute, the hours the hour-of-day average for `hour` (local) uses.
func (s *Store) FanMinutesAvg(loc *time.Location, hour int, now time.Time) FanMinutes {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := FanMinutes{Hour: hour}
	cov := mergeSpans(append([]Span{}, s.covered...))
	for _, ah := range avgHours(cov, now) {
		if ah.at.In(loc).Hour() == hour {
			f.Hours++
			s.fanMinute(ah.at, cov, &f.Posts, &f.Fans, &f.Coverage)
		}
	}
	if f.Hours > 1 {
		for m := 0; m < 60; m++ {
			n := float64(f.Hours)
			f.Posts[m] /= n
			f.Fans[m] /= n
			f.Coverage[m] /= n
		}
	}
	f.Artist = s.artistMarks(loc, func(at time.Time) bool { return at.In(loc).Hour() == hour })
	return f
}
