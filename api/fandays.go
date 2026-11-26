package main

import "time"

// FanDay is one local calendar day: fan posts, how much of the day captures saw, and the artists' timed likes and
// comments (the same events as the Fan feed's artist strip: uncertain likes are left out).
type FanDay struct {
	Date     string         `json:"date"`     // YYYY-MM-DD, local
	Start    time.Time      `json:"start"`    // local midnight
	Posts    int            `json:"posts"`    // fan posts created that day (artists' own posts left out)
	Fans     int            `json:"fans"`     // different fans posting
	Coverage float64        `json:"coverage"` // 0..1 of the day captures saw (for today: of the day so far)
	Artist   map[string]int `json:"artist"`   // timed likes and comments per member
}

// FanDayHour is one hour of a FanDay.
type FanDayHour struct {
	At       time.Time      `json:"at"`   // start of the hour
	Hour     int            `json:"hour"` // local hour of day
	Posts    int            `json:"posts"`
	Fans     int            `json:"fans"`
	Coverage float64        `json:"coverage"` // 0..1 of the hour captures saw (for the current hour: so far)
	Artist   map[string]int `json:"artist"`
}

// FanDayHours is one day hour by hour.
type FanDayHours struct {
	FanDay
	Hours []FanDayHour `json:"hours"`
}

const dateLayout = "2006-01-02"

func localMidnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// seenShare is how much of [a, b) captures saw, counting only up to now.
func seenShare(cov []Span, a, b, now time.Time) float64 {
	if !now.After(a) {
		return 0
	}
	if b.After(now) {
		b = now
	}
	return float64(overlap(cov, a, b)) / float64(b.Sub(a))
}

// fanBucket counts fan posts, different fans and timed artist events in [a, b).
type fanBucket struct {
	posts  int
	fans   map[string]bool
	artist map[string]int
}

// FanDays lists every local day from the first one captures saw up to today, oldest first.
func (s *Store) FanDays(loc *time.Location, now time.Time) []FanDay {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cov := mergeSpans(append([]Span{}, s.covered...))
	out := []FanDay{}
	if len(cov) == 0 {
		return out
	}
	buckets := map[string]*fanBucket{}
	get := func(t time.Time) *fanBucket {
		k := t.In(loc).Format(dateLayout)
		b := buckets[k]
		if b == nil {
			b = &fanBucket{fans: map[string]bool{}, artist: map[string]int{}}
			buckets[k] = b
		}
		return b
	}
	for _, p := range s.posts {
		if p.feed.authorIsArtist || p.createdAt.IsZero() {
			continue
		}
		b := get(p.createdAt)
		b.posts++
		b.fans[p.author] = true
	}
	for _, m := range s.artistMarks(loc, func(time.Time) bool { return true }) {
		get(m.At).artist[m.Member]++
	}
	for d := localMidnight(cov[0].Start, loc); d.Before(now); {
		next := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
		day := FanDay{Date: d.Format(dateLayout), Start: d, Coverage: seenShare(cov, d, next, now), Artist: map[string]int{}}
		if b := buckets[day.Date]; b != nil {
			day.Posts, day.Fans, day.Artist = b.posts, len(b.fans), b.artist
		}
		out = append(out, day)
		d = next
	}
	return out
}

// FanDayAt is one local day (date YYYY-MM-DD in loc) hour by hour.
func (s *Store) FanDayAt(loc *time.Location, date string, now time.Time) (FanDayHours, error) {
	d, err := time.ParseInLocation(dateLayout, date, loc)
	if err != nil {
		return FanDayHours{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
	cov := mergeSpans(append([]Span{}, s.covered...))
	out := FanDayHours{FanDay: FanDay{Date: date, Start: d, Coverage: seenShare(cov, d, next, now), Artist: map[string]int{}}}
	idx := map[int64]int{} // unix hour -> index in Hours
	for h := d; h.Before(next); h = h.Add(time.Hour) {
		idx[h.Unix()/3600] = len(out.Hours)
		out.Hours = append(out.Hours, FanDayHour{At: h, Hour: h.In(loc).Hour(),
			Coverage: seenShare(cov, h, h.Add(time.Hour), now), Artist: map[string]int{}})
	}
	dayFans := map[string]bool{}
	hourFans := make([]map[string]bool, len(out.Hours))
	for _, p := range s.posts {
		if p.feed.authorIsArtist || p.createdAt.Before(d) || !p.createdAt.Before(next) {
			continue
		}
		i := idx[p.createdAt.Unix()/3600]
		out.Hours[i].Posts++
		out.Posts++
		dayFans[p.author] = true
		if hourFans[i] == nil {
			hourFans[i] = map[string]bool{}
		}
		hourFans[i][p.author] = true
	}
	out.Fans = len(dayFans)
	for i := range out.Hours {
		out.Hours[i].Fans = len(hourFans[i])
	}
	for _, m := range s.artistMarks(loc, func(at time.Time) bool { return !at.Before(d) && at.Before(next) }) {
		out.Hours[idx[m.At.Unix()/3600]].Artist[m.Member]++
		out.Artist[m.Member]++
	}
	return out, nil
}
