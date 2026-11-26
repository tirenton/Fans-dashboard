package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfiles(t *testing.T) {
	dir := t.TempDir()
	p, err := NewProfiles(filepath.Join(dir, "profiles.json"), filepath.Join(dir, "avatars"))
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	p.OnChange = func() { changes++ }
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

	if pr, err := p.SetDisplay("ann", "  Ann\n🐳  ", now); err != nil || pr.Display != "Ann 🐳" {
		t.Fatalf("display: %+v %v", pr, err)
	}
	if _, err := p.SetDisplay("ann", strings.Repeat("x", 33), now.Add(time.Minute)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("33 characters: %v", err)
	}
	if _, err := p.SetDisplay("ann", "Again", now.Add(time.Second)); !errors.Is(err, ErrTooFast) {
		t.Fatalf("a second later: %v", err)
	}

	// a 600x400 PNG with a red left half: cropped to the middle square, scaled to 256x256, stored as JPEG
	src := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			c := color.RGBA{0, 0, 255, 255}
			if x < 300 {
				c = color.RGBA{255, 0, 0, 255}
			}
			src.Set(x, y, c)
		}
	}
	var in bytes.Buffer
	png.Encode(&in, src)
	pr, err := p.SetAvatar("ann", in.Bytes(), now.Add(2*time.Minute))
	if err != nil || pr.Avatar != now.Add(2*time.Minute).UnixMilli() || pr.Display != "Ann 🐳" {
		t.Fatalf("avatar: %+v %v", pr, err)
	}
	out, err := os.ReadFile(p.AvatarPath("ann"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("stored picture: %v %v", img.Bounds(), err)
	}
	if r, _, b, _ := img.At(10, 128).RGBA(); r>>8 < 200 || b>>8 > 60 {
		t.Fatalf("left side should stay red, got r=%d b=%d", r>>8, b>>8)
	}
	if r, _, b, _ := img.At(245, 128).RGBA(); b>>8 < 200 || r>>8 > 60 {
		t.Fatalf("right side should stay blue, got r=%d b=%d", r>>8, b>>8)
	}

	for _, bad := range [][]byte{nil, []byte("not a picture"), []byte("GIF89a......")} {
		if _, err := p.SetAvatar("ann", bad, now.Add(time.Hour)); !errors.Is(err, ErrBadImage) {
			t.Fatalf("%q: %v", bad, err)
		}
	}

	// it survives a restart, and the chat hears about every change
	p2, _ := NewProfiles(filepath.Join(dir, "profiles.json"), filepath.Join(dir, "avatars"))
	if got := p2.Of([]string{"ann", "bob"}); len(got) != 1 || got["ann"].Avatar == 0 {
		t.Fatalf("reloaded: %+v", got)
	}
	if changes != 2 {
		t.Fatalf("changes: %d", changes)
	}

	if pr, err := p.RemoveAvatar("ann"); err != nil || pr.Avatar != 0 {
		t.Fatalf("remove: %+v %v", pr, err)
	}
	if _, err := os.Stat(p.AvatarPath("ann")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the file should be gone")
	}
	if _, err := p.SetDisplay("ann", "", now.Add(3*time.Hour)); err != nil || len(p.Of([]string{"ann"})) != 0 {
		t.Fatalf("an empty profile is dropped: %v %+v", err, p.Of([]string{"ann"}))
	}
}

func TestChangePassword(t *testing.T) {
	file := filepath.Join(t.TempDir(), "users.json")
	h, _ := HashPassword("old password 1")
	h2, _ := HashPassword("other password 1")
	SaveUsers(file, []User{{Name: "ann", Role: RoleUser, Hash: h}, {Name: "bob", Role: RoleUser, Hash: h2, Email: "bob@example.com"}})
	a := NewAuth(file)
	now := time.Now()

	if err := a.ChangePassword("ann", "old password 1", "short", now); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("short: %v", err)
	}
	if err := a.ChangePassword("ann", "wrong", "new password 22", now); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("wrong current: %v", err)
	}
	if err := a.ChangePassword("ann", "old password 1", "new password 22", now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login("ann", "old password 1", "", now); err == nil {
		t.Fatal("the old password must stop working")
	}
	if u, err := a.Login("ann", "new password 22", "", now); err != nil || u.Name != "ann" {
		t.Fatalf("new password: %+v %v", u, err)
	}
	if u, _ := LoadUsers(file); u[1].Hash != h2 || u[1].Email != "bob@example.com" {
		t.Fatal("the other account must stay as it was")
	}
	if a.Email("bob") != "bob@example.com" || a.Email("ann") != "" {
		t.Fatal("Email")
	}
}

func TestChatTouch(t *testing.T) {
	c, _ := NewChat(filepath.Join(t.TempDir(), "chat.json"))
	v := c.View().Version
	c.Touch(time.Now())
	if c.View().Version == v {
		t.Fatal("Touch must change the version so waiting pages reload")
	}
}
