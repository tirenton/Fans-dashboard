package main

// The Main dashboard's open chat and feedback box. Both live in small JSON files next to the accounts
// (CHAT_FILE, FEEDBACK_FILE), so they survive restarts. Only the web app calls these endpoints, and it passes
// the signed-in name and role from its session; this API never sees a visitor directly.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	chatKeep     = 300 // messages kept in the file
	chatShow     = 100 // messages sent to the page
	chatMaxRunes = 500
	chatPerMin   = 10 // posts per person per minute
	chatGap      = time.Second

	feedbackKeep     = 1000
	feedbackMaxRunes = 2000
	feedbackPerHour  = 5
)

var (
	ErrEmpty    = errors.New("empty message")
	ErrTooLong  = errors.New("message too long")
	ErrTooFast  = errors.New("slow down a little")
	ErrNotFound = errors.New("not found")
)

// cleanText trims s, drops control characters and, unless multiline, turns line breaks into spaces.
// It fails when nothing is left or s is longer than max characters.
func cleanText(s string, max int, multiline bool) (string, error) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	blank := 0
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			if blank++; blank > 2 { // at most one empty line in a row
				continue
			}
			b.WriteRune(r)
			continue
		case r == '\n' || r == '\t' || r == '\r':
			r = ' '
		case unicode.IsControl(r) || r == utf8.RuneError || unicode.Is(unicode.Cf, r) && r != '‍':
			continue // keep the zero-width joiner, which emoji need
		}
		blank = 0
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", ErrEmpty
	}
	if utf8.RuneCountInString(out) > max {
		return "", ErrTooLong
	}
	return out, nil
}

// limiter allows n actions per person per window, and none closer together than gap.
type limiter struct {
	n      int
	window time.Duration
	gap    time.Duration
	recent map[string][]time.Time
}

func (l *limiter) allow(name string, now time.Time) bool {
	if l.recent == nil {
		l.recent = map[string][]time.Time{}
	}
	kept := l.recent[name][:0]
	for _, t := range l.recent[name] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.n || (len(kept) > 0 && now.Sub(kept[len(kept)-1]) < l.gap) {
		l.recent[name] = kept
		return false
	}
	l.recent[name] = append(kept, now)
	return true
}

func loadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func saveJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeAtomic(path, b)
}

type ChatMessage struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Admin bool      `json:"admin,omitempty"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// ChatView is what the page shows. Version changes on every post or delete, so the page can wait for the next one.
type ChatView struct {
	Version  int64         `json:"version"`
	Messages []ChatMessage `json:"messages"`
}

type Chat struct {
	mu      sync.Mutex
	file    string
	msgs    []ChatMessage
	nextID  int64
	version int64
	wake    chan struct{} // closed and replaced on every change
	limit   limiter
}

func NewChat(file string) (*Chat, error) {
	c := &Chat{file: file, nextID: 1, version: time.Now().UnixMilli(), wake: make(chan struct{}),
		limit: limiter{n: chatPerMin, window: time.Minute, gap: chatGap}}
	if err := loadJSON(file, &c.msgs); err != nil {
		return c, err
	}
	for _, m := range c.msgs {
		if m.ID >= c.nextID {
			c.nextID = m.ID + 1
		}
	}
	return c, nil
}

func (c *Chat) viewLocked() ChatView {
	from := max(0, len(c.msgs)-chatShow)
	return ChatView{Version: c.version, Messages: append([]ChatMessage{}, c.msgs[from:]...)}
}

func (c *Chat) View() ChatView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.viewLocked()
}

// Wait returns the chat once its version differs from since, holding on up to d for a change.
func (c *Chat) Wait(ctx context.Context, since int64, d time.Duration) ChatView {
	c.mu.Lock()
	if c.version == since {
		w := c.wake
		c.mu.Unlock()
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-w:
		case <-t.C:
		case <-ctx.Done():
		}
		c.mu.Lock()
	}
	defer c.mu.Unlock()
	return c.viewLocked()
}

func (c *Chat) bumpLocked(now time.Time) {
	// milliseconds, not nanoseconds: the page holds it as a JavaScript number, exact only below 2^53
	if v := now.UnixMilli(); v > c.version {
		c.version = v
	} else {
		c.version++
	}
	close(c.wake)
	c.wake = make(chan struct{})
}

func (c *Chat) changedLocked(now time.Time) error {
	c.bumpLocked(now)
	return saveJSON(c.file, c.msgs)
}

// Touch wakes the waiting pages without changing a message (someone changed their name or picture).
func (c *Chat) Touch(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bumpLocked(now)
}

func (c *Chat) Post(name string, admin bool, text string, now time.Time) (ChatMessage, error) {
	text, err := cleanText(text, chatMaxRunes, false)
	if err != nil {
		return ChatMessage{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.limit.allow(name, now) {
		return ChatMessage{}, ErrTooFast
	}
	m := ChatMessage{ID: c.nextID, Name: name, Admin: admin, Text: text, At: now.UTC()}
	c.nextID++
	c.msgs = append(c.msgs, m)
	if len(c.msgs) > chatKeep {
		c.msgs = append([]ChatMessage{}, c.msgs[len(c.msgs)-chatKeep:]...)
	}
	return m, c.changedLocked(now)
}

// Delete removes a message (admins, from the page).
func (c *Chat) Delete(id int64, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, m := range c.msgs {
		if m.ID == id {
			c.msgs = append(c.msgs[:i], c.msgs[i+1:]...)
			return c.changedLocked(now)
		}
	}
	return ErrNotFound
}

type Feedback struct {
	ID   int64     `json:"id"`
	Name string    `json:"name"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// FeedbackBox keeps what people send from the bottom of the Main dashboard, for the admins.
type FeedbackBox struct {
	mu     sync.Mutex
	file   string
	items  []Feedback
	nextID int64
	limit  limiter
}

func NewFeedbackBox(file string) (*FeedbackBox, error) {
	f := &FeedbackBox{file: file, nextID: 1, limit: limiter{n: feedbackPerHour, window: time.Hour, gap: 10 * time.Second}}
	if err := loadJSON(file, &f.items); err != nil {
		return f, err
	}
	for _, it := range f.items {
		if it.ID >= f.nextID {
			f.nextID = it.ID + 1
		}
	}
	return f, nil
}

func (f *FeedbackBox) Add(name, text string, now time.Time) (Feedback, error) {
	text, err := cleanText(text, feedbackMaxRunes, true)
	if err != nil {
		return Feedback{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.limit.allow(name, now) {
		return Feedback{}, ErrTooFast
	}
	it := Feedback{ID: f.nextID, Name: name, Text: text, At: now.UTC()}
	f.nextID++
	f.items = append(f.items, it)
	if len(f.items) > feedbackKeep {
		f.items = append([]Feedback{}, f.items[len(f.items)-feedbackKeep:]...)
	}
	return it, saveJSON(f.file, f.items)
}

// List returns the feedback newest first.
func (f *FeedbackBox) List() []Feedback {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Feedback, len(f.items))
	for i, it := range f.items {
		out[len(f.items)-1-i] = it
	}
	return out
}

// Delete removes one item (an admin marking it done).
func (f *FeedbackBox) Delete(id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, it := range f.items {
		if it.ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return saveJSON(f.file, f.items)
		}
	}
	return ErrNotFound
}
