package main

// Profiles: a display name and a profile picture per account, shown in the chat and the top bar. They live next to
// the accounts (PROFILES_FILE, AVATARS_DIR), not in the users file, so the PC's copy of the admins never touches them.
// Pictures are always re-drawn here into a fresh 256×256 JPEG: whatever the upload carried (camera details, GPS
// position, other hidden data) is gone, and only an image this code made is ever served.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // decode PNG uploads too
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	displayMaxRunes = 32
	avatarSide      = 256
	avatarMaxBytes  = 2 << 20 // what the page sends is ~30 KB; this only stops abuse
	avatarMaxSide   = 4096    // refuse bigger images before decoding them
)

var ErrBadImage = errors.New("not a JPEG or PNG picture")

type Profile struct {
	Display string `json:"display,omitempty"`
	Avatar  int64  `json:"avatar,omitempty"` // version (Unix ms) of the picture, for the page's cache; 0 = none
}

type Profiles struct {
	mu       sync.Mutex
	file     string
	dir      string
	m        map[string]Profile
	limit    limiter
	OnChange func() // the chat shows the new name or picture straight away
}

func NewProfiles(file, dir string) (*Profiles, error) {
	p := &Profiles{file: file, dir: dir, m: map[string]Profile{}, limit: limiter{n: 20, window: time.Hour, gap: 2 * time.Second}}
	if err := loadJSON(file, &p.m); err != nil {
		return p, err
	}
	if p.m == nil {
		p.m = map[string]Profile{}
	}
	return p, os.MkdirAll(dir, 0o755)
}

func (p *Profiles) Get(name string) Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[name]
}

// Of returns the profiles of the given names that have one.
func (p *Profiles) Of(names []string) map[string]Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]Profile{}
	for _, n := range names {
		if pr, ok := p.m[n]; ok {
			out[n] = pr
		}
	}
	return out
}

func (p *Profiles) saveLocked(name string, pr Profile) error {
	if pr == (Profile{}) {
		delete(p.m, name)
	} else {
		p.m[name] = pr
	}
	if err := saveJSON(p.file, p.m); err != nil {
		return err
	}
	if p.OnChange != nil {
		p.OnChange()
	}
	return nil
}

// SetDisplay sets the name shown instead of the account name; empty clears it.
func (p *Profiles) SetDisplay(name, display string, now time.Time) (Profile, error) {
	if display != "" {
		var err error
		if display, err = cleanText(display, displayMaxRunes, false); err != nil && !errors.Is(err, ErrEmpty) {
			return Profile{}, err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.limit.allow(name, now) {
		return Profile{}, ErrTooFast
	}
	pr := p.m[name]
	pr.Display = display
	return pr, p.saveLocked(name, pr)
}

// AvatarPath is where name's picture is (whether or not there is one).
func (p *Profiles) AvatarPath(name string) string { return filepath.Join(p.dir, name+".jpg") }

// SetAvatar checks the upload, crops it to the middle square, scales it to 256×256 and stores it as a new JPEG.
func (p *Profiles) SetAvatar(name string, data []byte, now time.Time) (Profile, error) {
	out, err := squareJPEG(data, avatarSide)
	if err != nil {
		return Profile{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.limit.allow(name, now) {
		return Profile{}, ErrTooFast
	}
	if err := writeAtomic(p.AvatarPath(name), out); err != nil {
		return Profile{}, err
	}
	pr := p.m[name]
	pr.Avatar = now.UnixMilli()
	return pr, p.saveLocked(name, pr)
}

// RemoveAvatar deletes name's picture (the person themselves, or an admin).
func (p *Profiles) RemoveAvatar(name string) (Profile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.Remove(p.AvatarPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Profile{}, err
	}
	pr := p.m[name]
	pr.Avatar = 0
	return pr, p.saveLocked(name, pr)
}

// squareJPEG decodes a JPEG or PNG, crops the middle square and scales it down to side×side (box filter).
func squareJPEG(data []byte, side int) ([]byte, error) {
	if len(data) == 0 || len(data) > avatarMaxBytes {
		return nil, ErrBadImage
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 16 || cfg.Height < 16 ||
		cfg.Width > avatarMaxSide || cfg.Height > avatarMaxSide {
		return nil, ErrBadImage
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBadImage
	}
	b := src.Bounds()
	s := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-s)/2, b.Min.Y+(b.Dy()-s)/2
	n := min(side, s)
	dst := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		sy0, sy1 := y0+y*s/n, y0+(y+1)*s/n
		for x := 0; x < n; x++ {
			sx0, sx1 := x0+x*s/n, x0+(x+1)*s/n
			var r, g, bl, a, cnt uint64
			for yy := sy0; yy < max(sy1, sy0+1); yy++ {
				for xx := sx0; xx < max(sx1, sx0+1); xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r, g, bl, a, cnt = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), cnt+1
				}
			}
			// transparent PNG areas become the site's dark background instead of black
			al := float64(a) / float64(cnt) / 0xffff
			bg := [3]float64{0x16, 0x16, 0x18}
			mix := func(c uint64, i int) uint8 {
				return uint8(float64(c)/float64(cnt)/0x101 + (1-al)*bg[i])
			}
			dst.Set(x, y, color.RGBA{mix(r, 0), mix(g, 1), mix(bl, 2), 0xff})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
