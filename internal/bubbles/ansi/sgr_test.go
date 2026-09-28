package ansi

import (
	"image/color"
	"testing"
)

func TestSGR_ColorsOnDefaultOff(t *testing.T) {
	t.Parallel()
	on, off := SGR(color.RGBA{R: 1, G: 2, B: 3, A: 255}, color.RGBA{R: 255, G: 200, B: 0, A: 255})
	if want := "\x1b[38;2;1;2;3;48;2;255;200;0m"; on != want {
		t.Errorf("on = %q, want %q", on, want)
	}
	if want := "\x1b[39;49m"; off != want {
		t.Errorf("off = %q, want %q", off, want)
	}
}

func TestSGR_NilColorsUseDefaults(t *testing.T) {
	t.Parallel()
	on, _ := SGR(nil, nil)
	if want := "\x1b[39;49m"; on != want {
		t.Errorf("on = %q, want %q", on, want)
	}
}
