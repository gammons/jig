package imgrender

import (
	"encoding/base64"
	"image"
	"image/color"
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// noise is a w×h image of pseudo-random pixels (a fixed LCG), so its PNG
// barely compresses.
func noise(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	s := uint32(12345)
	for i := range img.Pix {
		s = s*1664525 + 1013904223
		img.Pix[i] = uint8(s >> 24)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	return img
}

func TestKitty_IDEncodedInForeground(t *testing.T) {
	t.Parallel()
	r := New(Kitty)
	r.nextID = 0x010203
	res := r.Render("k", solid(16, 32), 2, 2)
	checkShape(t, res, 2, 2)
	for i, l := range res.Lines {
		if !strings.HasPrefix(l, "\x1b[38;2;1;2;3m") {
			t.Errorf("line %d = %q, want the id 0x010203 as fg 38;2;1;2;3m", i, l)
		}
	}
	if !strings.Contains(res.Upload, ",i=66051,") {
		t.Errorf("upload does not carry i=66051: %.80q", res.Upload)
	}
}

func TestKitty_IDsPerKey(t *testing.T) {
	t.Parallel()
	r := New(Kitty)
	a := r.Render("a", solid(8, 16), 1, 1)
	b := r.Render("b", solid(8, 16), 1, 1)
	a2 := r.Render("a", solid(8, 16), 1, 1)
	if !strings.HasPrefix(a.Lines[0], "\x1b[38;2;0;0;1m") || !strings.HasPrefix(b.Lines[0], "\x1b[38;2;0;0;2m") {
		t.Errorf("ids: a=%q b=%q, want 1 and 2", a.Lines[0], b.Lines[0])
	}
	if a2.Lines[0] != a.Lines[0] {
		t.Errorf("key a re-rendered with a different id: %q vs %q", a2.Lines[0], a.Lines[0])
	}
}

func TestKitty_IDWrapsWithin24Bits(t *testing.T) {
	t.Parallel()
	r := New(Kitty)
	r.nextID = maxKittyID
	r.Render("a", solid(8, 16), 1, 1)
	res := r.Render("b", solid(8, 16), 1, 1)
	if !strings.HasPrefix(res.Lines[0], "\x1b[38;2;0;0;1m") {
		t.Errorf("after 0xFFFFFF the next id = %q, want 1", res.Lines[0])
	}
}

func TestKitty_UploadOnce(t *testing.T) {
	t.Parallel()
	r := New(Kitty)
	img := solid(32, 64)
	first := r.Render("k", img, 4, 4)
	if first.Upload == "" {
		t.Fatal("first render: no upload")
	}
	if !strings.HasPrefix(first.Upload, "\x1b_Ga=T,f=100,t=d,i=1,U=1,c=4,r=4,q=2,m=") {
		t.Errorf("upload header = %.80q", first.Upload)
	}
	if again := r.Render("k", img, 4, 4); again.Upload != "" {
		t.Errorf("second render uploaded again: %.60q", again.Upload)
	}
	smaller := r.Render("k", img, 2, 2)
	if !strings.HasPrefix(smaller.Upload, "\x1b_Ga=T,f=100,t=d,i=1,U=1,c=2,r=2,") {
		t.Errorf("new size: upload = %.80q, want a fresh upload for 2x2 with the same id", smaller.Upload)
	}
	if first.Sixel != "" {
		t.Errorf("kitty Sixel = %q, want empty", first.Sixel)
	}
}

func TestKitty_ReuploadWhenReturningToEarlierSize(t *testing.T) {
	t.Parallel()
	// Re-transmitting an id replaces the terminal's image, so going back
	// to a size uploaded earlier must upload again.
	r := New(Kitty)
	img := solid(32, 64)
	for i, tc := range []struct {
		size   int
		upload bool
	}{{4, true}, {4, false}, {2, true}, {2, false}, {4, true}, {4, false}} {
		res := r.Render("k", img, tc.size, tc.size)
		if got := res.Upload != ""; got != tc.upload {
			t.Errorf("render %d (%dx%d): upload = %v, want %v", i, tc.size, tc.size, got, tc.upload)
		}
	}
}

var apcRE = regexp.MustCompile(`\x1b_G([^;]*);([^\x1b]*)\x1b\\`)

func TestKitty_Chunking(t *testing.T) {
	t.Parallel()
	r := New(Kitty, WithCellSize(1, 1))
	res := r.Render("k", noise(100, 100), 100, 100)
	chunks := apcRE.FindAllStringSubmatch(res.Upload, -1)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want several", len(chunks))
	}
	if joined := strings.Join(apcRE.FindAllString(res.Upload, -1), ""); joined != res.Upload {
		t.Fatal("upload has bytes outside the APC chunks")
	}
	var payload strings.Builder
	for i, c := range chunks {
		keys, data := c[1], c[2]
		if len(data) > 4096 {
			t.Errorf("chunk %d: %d bytes > 4096", i, len(data))
		}
		wantM := "m=1"
		if i == len(chunks)-1 {
			wantM = "m=0"
		}
		if !strings.HasSuffix(keys, wantM) {
			t.Errorf("chunk %d keys %q, want suffix %s", i, keys, wantM)
		}
		if i > 0 && keys != wantM {
			t.Errorf("continuation chunk %d keys %q, want only %s", i, keys, wantM)
		}
		payload.WriteString(data)
	}
	raw, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if !strings.HasPrefix(string(raw), "\x89PNG\r\n\x1a\n") {
		t.Errorf("payload is not a PNG")
	}
}

func TestKitty_TmuxWrap(t *testing.T) {
	t.Parallel()
	plain := New(Kitty).Render("k", solid(16, 32), 2, 2)
	wrapped := New(Kitty, WithTmux(true)).Render("k", solid(16, 32), 2, 2)
	var want strings.Builder
	for _, seq := range apcRE.FindAllString(plain.Upload, -1) {
		want.WriteString("\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\")
	}
	if wrapped.Upload != want.String() {
		t.Errorf("tmux upload = %.120q\nwant %.120q", wrapped.Upload, want.String())
	}
	if strings.Join(wrapped.Lines, "\n") != strings.Join(plain.Lines, "\n") {
		t.Error("tmux changed the placeholder lines")
	}
	if off := New(Kitty, WithTmux(false)).Render("k", solid(16, 32), 2, 2); off.Upload != plain.Upload {
		t.Error("WithTmux(false) changed the upload")
	}
}

func TestKitty_DiacriticsRowCol(t *testing.T) {
	t.Parallel()
	res := New(Kitty, WithCellSize(1, 1)).Render("k", solid(5, 4), 5, 4)
	checkShape(t, res, 5, 4)
	sgr := "\x1b[38;2;0;0;1m"
	line := strings.TrimSuffix(strings.TrimPrefix(res.Lines[2], sgr), "\x1b[0m")
	cells := []rune(line)
	if len(cells) != 5*3 {
		t.Fatalf("row 2 has %d runes, want 15: %q", len(cells), line)
	}
	got := cells[3*3 : 3*3+3]
	want := []rune{ansi.PlaceholderRune, diacritic(2), diacritic(3)}
	if string(got) != string(want) {
		t.Errorf("cell (2,3) = %U, want %U", got, want)
	}
	if diacritic(0) != 0x0305 || diacritic(1) != 0x030D || diacritic(296) != 0x1D244 {
		t.Errorf("diacritic table endpoints wrong: %U %U %U", diacritic(0), diacritic(1), diacritic(296))
	}
}

func TestKitty_ClampsToDiacriticTable(t *testing.T) {
	t.Parallel()
	res := New(Kitty, WithCellSize(1, 1)).Render("k", solid(1000, 2), 1000, 10)
	if w := ansi.Width(res.Lines[0]); w != diacriticCount {
		t.Errorf("width = %d, want clamp to %d", w, diacriticCount)
	}
}

func TestKitty_TransparentStillUploads(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 16))
	img.SetNRGBA(0, 0, color.NRGBA{})
	if res := New(Kitty).Render("k", img, 1, 1); res.Upload == "" || len(res.Lines) != 1 {
		t.Errorf("got %+v", res)
	}
}
