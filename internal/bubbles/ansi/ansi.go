// Package ansi is jig's boundary for terminal text: it makes untrusted text
// inert (Sanitize, SanitizeLine) and wraps the ANSI-aware width, truncate,
// cut, wrap, and highlight helpers the widgets share.
package ansi

import xansi "github.com/charmbracelet/x/ansi"

// PlaceholderRune is the kitty graphics unicode-placeholder cell. Only jig's
// own image renderer may emit it, so Sanitize drops it from untrusted text.
const PlaceholderRune = '\U0010EEEE'

// Width returns the cell width of s, ignoring escape sequences.
func Width(s string) int {
	return xansi.StringWidth(s)
}

// Truncate cuts s to at most width cells, ending with tail when it cuts.
func Truncate(s string, width int, tail string) string {
	return xansi.Truncate(s, width, tail)
}

// Cut returns the cells of s in [left, right).
func Cut(s string, left, right int) string {
	return xansi.Cut(s, left, right)
}

// Wrap word-wraps s to width cells, hard-breaking any word longer than width.
func Wrap(s string, width int) string {
	return xansi.Hardwrap(xansi.Wordwrap(s, width, ""), width, true)
}
