package imgrender

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// twoByTwo is red, green / blue, white: all web-safe, so dithering keeps
// every pixel exact.
func twoByTwo() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	return img
}

func TestSixel_TwoByTwoGolden(t *testing.T) {
	t.Parallel()
	res := New(Sixel, WithCellSize(1, 1)).Render("k", twoByTwo(), 2, 2)
	// WebSafe index = 36r+6g+b: blue 5, green 30, red 180, white 215.
	// One band: blue in row 1 of x=0 ('?'+2 = 'A'), green row 0 of x=1,
	// red row 0 of x=0, white row 1 of x=1.
	want := "\x1bPq\"1;1;2;2" +
		"#5;2;0;0;100#30;2;0;100;0#180;2;100;0;0#215;2;100;100;100" +
		"#5A$#30?@$#180@$#215?A" +
		"\x1b\\"
	if res.Sixel != want {
		t.Errorf("sixel =\n%q\nwant\n%q", res.Sixel, want)
	}
	if len(res.Lines) != 2 || res.Lines[0] != "  " || res.Lines[1] != "  " {
		t.Errorf("Lines = %q, want two blank lines of width 2", res.Lines)
	}
	if res.Upload != "" {
		t.Errorf("sixel Upload = %q, want empty", res.Upload)
	}
}

func TestSixel_BandsAndRuns(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 5, 7))
	for y := range 7 {
		for x := range 5 {
			img.Set(x, y, color.RGBA{A: 255})
		}
	}
	got := encodeSixel(img)
	// Black (index 0) everywhere: a full band of five '~' (run-length
	// encoded), then a one-row band of five '@'.
	want := "\x1bPq\"1;1;5;7#0;2;0;0;0#0!5~-#0!5@\x1b\\"
	if got != want {
		t.Errorf("sixel = %q, want %q", got, want)
	}
}

func TestSixel_Shape(t *testing.T) {
	t.Parallel()
	res := New(Sixel).Render("k", solid(80, 64), 40, 40)
	if len(res.Lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(res.Lines))
	}
	for i, l := range res.Lines {
		if l != strings.Repeat(" ", 10) {
			t.Errorf("line %d = %q, want 10 blanks", i, l)
		}
	}
	if !strings.HasPrefix(res.Sixel, "\x1bPq\"1;1;80;64#") || !strings.HasSuffix(res.Sixel, "\x1b\\") {
		t.Errorf("sixel framing: %.40q … %q", res.Sixel, res.Sixel[len(res.Sixel)-2:])
	}
}

func TestPlace_CursorSaveRestore(t *testing.T) {
	t.Parallel()
	res := New(Sixel, WithCellSize(1, 1)).Render("k", twoByTwo(), 2, 2)
	got := Place(res, 7, 2)
	want := "\x1b7\x1b[3;8H" + res.Sixel + "\x1b8"
	if got != want {
		t.Errorf("Place = %q, want %q", got, want)
	}
	if Place(New(Kitty).Render("k", twoByTwo(), 2, 2), 0, 0) != "" {
		t.Error("Place of a kitty result should be empty")
	}
}
