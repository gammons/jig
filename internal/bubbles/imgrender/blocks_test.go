package imgrender

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

// twoByFour is a 2×4 px image with a known color per pixel, including a
// fully transparent and a half-transparent pixel.
func twoByFour() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 4))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetNRGBA(0, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	img.SetNRGBA(1, 2, color.NRGBA{R: 40, G: 50, B: 60, A: 255})
	img.SetNRGBA(0, 3, color.NRGBA{})
	img.SetNRGBA(1, 3, color.NRGBA{R: 255, A: 128})
	return img
}

func solid(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 7, A: 255})
		}
	}
	return img
}

func checkShape(t *testing.T, res Result, cols, rows int) {
	t.Helper()
	if len(res.Lines) != rows {
		t.Fatalf("got %d lines, want %d", len(res.Lines), rows)
	}
	for i, l := range res.Lines {
		if w := ansi.Width(l); w != cols {
			t.Errorf("line %d width = %d, want %d: %q", i, w, cols, l)
		}
		if !strings.HasSuffix(l, "\x1b[0m") {
			t.Errorf("line %d does not end with a reset: %q", i, l)
		}
	}
}

func TestBlocks_2x4Golden(t *testing.T) {
	t.Parallel()
	// One pixel per column and two per row, so 2×4 px is exactly 2×2 cells.
	r := New(Blocks, WithCellSize(1, 2))
	res := r.Render("k", twoByFour(), 2, 2)
	checkShape(t, res, 2, 2)
	if res.Upload != "" || res.Sixel != "" {
		t.Errorf("blocks: Upload=%q Sixel=%q, want empty", res.Upload, res.Sixel)
	}
	golden.Assert(t, "blocks_2x4", strings.Join(res.Lines, "\n"))
}

func TestRender_FitsAspect(t *testing.T) {
	t.Parallel()
	r := New(Blocks)
	res := r.Render("k", solid(1000, 500), 40, 40)
	rows := len(res.Lines)
	if rows == 0 {
		t.Fatal("no lines")
	}
	cols := ansi.Width(res.Lines[0])
	checkShape(t, res, cols, rows)
	if cols > 40 || rows > 40 {
		t.Errorf("%dx%d cells exceeds 40x40", cols, rows)
	}
	// 1000×500 px at 8×16 px cells is 125×31.25 cells: a 4:1 cell ratio.
	if ratio := float64(cols) / float64(rows); ratio < 3 || ratio > 5 {
		t.Errorf("cols/rows = %d/%d = %.2f, want 4±1", cols, rows, ratio)
	}
}

func TestRender_NeverUpscales(t *testing.T) {
	t.Parallel()
	// 80×64 px at 8×16 is 10×4 cells, well inside 40×40.
	res := New(Blocks).Render("k", solid(80, 64), 40, 40)
	checkShape(t, res, 10, 4)
}

func TestRender_NeverZero(t *testing.T) {
	t.Parallel()
	r := New(Blocks)
	for _, tc := range []struct{ w, h, maxC, maxR int }{
		{1, 1, 40, 40},
		{1, 5000, 40, 40},
		{5000, 1, 40, 40},
		{500, 500, 1, 1},
	} {
		res := r.Render("k", solid(tc.w, tc.h), tc.maxC, tc.maxR)
		if len(res.Lines) < 1 || len(res.Lines) > tc.maxR {
			t.Errorf("%dx%d in %dx%d: %d rows", tc.w, tc.h, tc.maxC, tc.maxR, len(res.Lines))
			continue
		}
		cols := ansi.Width(res.Lines[0])
		if cols < 1 || cols > tc.maxC {
			t.Errorf("%dx%d in %dx%d: %d cols", tc.w, tc.h, tc.maxC, tc.maxR, cols)
		}
		checkShape(t, res, cols, len(res.Lines))
	}
}

func TestRender_EmptyForNonPositiveBox(t *testing.T) {
	t.Parallel()
	for _, p := range []Protocol{Off, Blocks, Sixel, Kitty} {
		r := New(p)
		for _, box := range [][2]int{{0, 10}, {10, 0}, {-1, 5}, {5, -1}} {
			res := r.Render("k", solid(4, 4), box[0], box[1])
			if len(res.Lines) != 0 || res.Upload != "" || res.Sixel != "" {
				t.Errorf("%v %v: got %+v, want empty", p, box, res)
			}
		}
		if res := r.Render("k", nil, 10, 10); len(res.Lines) != 0 {
			t.Errorf("%v nil image: got %+v, want empty", p, res)
		}
	}
}

func TestRender_Off(t *testing.T) {
	t.Parallel()
	res := New(Off).Render("k", solid(640, 480), 40, 10)
	if len(res.Lines) != 1 || res.Lines[0] != "[image 640x480]" {
		t.Errorf("Lines = %q, want [\"[image 640x480]\"]", res.Lines)
	}
	res = New(Off).Render("k", solid(640, 480), 5, 10)
	if len(res.Lines) != 1 || ansi.Width(res.Lines[0]) > 5 {
		t.Errorf("narrow Off: Lines = %q, want one line of width <= 5", res.Lines)
	}
}

func TestRender_KittyAndSixelFallBackToBlocks(t *testing.T) {
	t.Parallel()
	want := New(Blocks).Render("k", solid(100, 100), 20, 20)
	for _, p := range []Protocol{Kitty, Sixel} {
		r := New(p)
		if r.Protocol() != p {
			t.Errorf("Protocol() = %v, want %v", r.Protocol(), p)
		}
		got := r.Render("k", solid(100, 100), 20, 20)
		if strings.Join(got.Lines, "\n") != strings.Join(want.Lines, "\n") {
			t.Errorf("%v: lines differ from blocks", p)
		}
	}
}

func TestPlace(t *testing.T) {
	t.Parallel()
	if got := Place(Result{Lines: []string{"x"}}, 3, 4); got != "" {
		t.Errorf("no sixel: Place = %q, want empty", got)
	}
	got := Place(Result{Sixel: "\x1bPq#0~\x1b\\"}, 3, 4)
	want := "\x1b7\x1b[5;4H\x1bPq#0~\x1b\\\x1b8"
	if got != want {
		t.Errorf("Place = %q, want %q", got, want)
	}
}
