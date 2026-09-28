package ansi

import (
	"image/color"

	xansi "github.com/charmbracelet/x/ansi"
)

// SGR returns the escape that sets fg and bg (on) and the one that resets
// both to the terminal defaults (off), for callers such as Highlight that
// take raw on/off strings. A nil color means the terminal default.
func SGR(fg, bg color.Color) (on, off string) {
	on = xansi.Style{}.ForegroundColor(fg).BackgroundColor(bg).String()
	off = xansi.Style{}.ForegroundColor(nil).BackgroundColor(nil).String()
	return on, off
}
