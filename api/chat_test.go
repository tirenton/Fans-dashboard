package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanText(t *testing.T) {
	for _, c := range []struct {
		in, want  string
		multiline bool
		err       error
	}{
		{"  hi  ", "hi", false, nil},
		{"a\r\nb", "a b", false, nil},
		{"a\n\n\n\nb", "a\n\nb", true, nil},
		{"evil‮name", "evilname", false, nil}, // right-to-left override
		{"bell\x07", "bell", false, nil},
		{"👨‍👩", "👨‍👩", false, nil},
		{" \n\t ", "", false, ErrEmpty},
		{strings.Repeat("가", 11), "", false, ErrTooLong},
		{strings.Repeat("가", 10), strings.Repeat("가", 10), false, nil},
	} {
		got, err := cleanText(c.in, 10, c.multiline)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("cleanText(%q) = %q, %v; want %q, %v", c.in, got, err, c.want, c.err)
		}
	}
}

func TestChat(t *testing.T) {
	file := filepath.Join(t.TempDir(), "chat.json")
	c, err := NewChat(file)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	v0 := c.View().Version
	if v0 > 1<<53 {
		t.Fatalf("version %d is too big for the page's JavaScript numbers", v0)
	}

	// a waiting reader wakes up on the next post
	got := make(chan ChatView, 1)
	go func() { got <- c.Wait(context.Background(), v0, 5*time.Second) }()
	time.Sleep(50 * time.Millisecond)
	if _, err := c.Post("fan", false, "hello", now); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v.Version == v0 || len(v.Messages) != 1 || v.Messages[0].Text != "hello" {
			t.Fatalf("woke with %+v", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader not woken by a post")
	}
	// an up-to-date reader gives up after the wait
	start := time.Now()
	if v := c.Wait(context.Background(), c.View().Version, 100*time.Millisecond); len(v.Messages) != 1 || time.Since(start) < 90*time.Millisecond {
		t.Fatalf("no-change wait returned early: %+v", v)
	}

	// rate limit: not twice in a second, at most 10 a minute
	if _, err := c.Post("fan", false, "again", now.Add(500*time.Millisecond)); !errors.Is(err, ErrTooFast) {
		t.Fatalf("second post within a second: %v", err)
	}
	for i := 1; i < chatPerMin; i++ {
		if _, err := c.Post("fan", false, "x", now.Add(time.Duration(i)*2*time.Second)); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	if _, err := c.Post("fan", false, "x", now.Add(30*time.Second)); !errors.Is(err, ErrTooFast) {
		t.Fatalf("11th post in a minute: %v", err)
	}
	if _, err := c.Post("other", true, "admin here", now.Add(30*time.Second)); err != nil {
		t.Fatalf("someone else: %v", err)
	}
	if _, err := c.Post("fan", false, "x", now.Add(61*time.Second)); err != nil {
		t.Fatalf("a minute later: %v", err)
	}

	// survives a restart, delete works and is kept
	first := c.View().Messages[0].ID
	if err := c.Delete(first, now); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(first, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	c2, err := NewChat(file)
	if err != nil {
		t.Fatal(err)
	}
	v := c2.View()
	if len(v.Messages) != chatPerMin+1 || v.Messages[0].ID == first {
		t.Fatalf("after restart: %d messages, first %d", len(v.Messages), v.Messages[0].ID)
	}
	m, _ := c2.Post("new", false, "hi", now.Add(2*time.Minute))
	if m.ID <= v.Messages[len(v.Messages)-1].ID {
		t.Fatalf("id %d reused after restart", m.ID)
	}
	for _, m := range v.Messages {
		if m.Admin != (m.Name == "other") {
			t.Fatalf("admin flag wrong after restart: %+v", m)
		}
	}
}

func TestChatKeepsTheLatest(t *testing.T) {
	c, _ := NewChat(filepath.Join(t.TempDir(), "chat.json"))
	now := time.Now()
	for i := 0; i < chatKeep+5; i++ {
		if _, err := c.Post("u", false, "m", now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	v := c.View()
	if len(v.Messages) != chatShow || v.Messages[len(v.Messages)-1].ID != chatKeep+5 || len(c.msgs) != chatKeep {
		t.Fatalf("showing %d, last id %d, kept %d", len(v.Messages), v.Messages[len(v.Messages)-1].ID, len(c.msgs))
	}
}

func TestFeedback(t *testing.T) {
	file := filepath.Join(t.TempDir(), "feedback.json")
	f, err := NewFeedbackBox(file)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for i := 0; i < feedbackPerHour; i++ {
		if _, err := f.Add("fan", "line one\nline two", now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	if _, err := f.Add("fan", "one more", now.Add(10*time.Minute)); !errors.Is(err, ErrTooFast) {
		t.Fatalf("6th in an hour: %v", err)
	}
	if _, err := f.Add("fan", strings.Repeat("a", feedbackMaxRunes+1), now.Add(2*time.Hour)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("too long: %v", err)
	}
	list := f.List()
	if len(list) != feedbackPerHour || list[0].ID != feedbackPerHour || list[0].Text != "line one\nline two" {
		t.Fatalf("list: %+v", list)
	}
	if err := f.Delete(list[0].ID); err != nil {
		t.Fatal(err)
	}
	f2, _ := NewFeedbackBox(file)
	if n := len(f2.List()); n != feedbackPerHour-1 {
		t.Fatalf("after restart: %d items", n)
	}
	if it, _ := f2.Add("other", "hi", now); it.ID != feedbackPerHour+1 {
		// the deleted newest id may be reused only if nothing newer is left; ids stay unique among kept items
		if it.ID <= list[1].ID {
			t.Fatalf("id %d reused", it.ID)
		}
	}
}
