// Package selection holds a selection range and the pure functions to
// paint it over already-rendered rows and to extract its plain text. It
// knows nothing about the transcript, the details pane, the mouse, or the
// clipboard: those live in internal/ui. Ported from slk's
// internal/ui/selection.
package selection

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Point is one endpoint of a selection.
type Point struct {
	ID   string // item ID (blocklist) or a fixed pane ID (details)
	Line int    // line within the item's rendered lines, from 0
	Col  int    // display cell column within that line, from 0
}

// Range is a selection between Start and End, in either order. Active is
// true while the user is still dragging; a non-Active range is always
// Empty.
type Range struct {
	Start, End Point
	Active     bool
}

// Before reports whether a comes strictly before b in document order:
// item order (via order), then line, then column.
func Before(a, b Point, order func(id string) int) bool {
	oa, ob := order(a.ID), order(b.ID)
	if oa != ob {
		return oa < ob
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Col < b.Col
}

// Normalized returns r's endpoints in document order: lo is not after hi.
func (r Range) Normalized(order func(id string) int) (lo, hi Point) {
	if Before(r.End, r.Start, order) {
		return r.End, r.Start
	}
	return r.Start, r.End
}

// Empty reports whether r selects nothing: it is inactive, or its
// endpoints are equal.
func (r Range) Empty() bool {
	return !r.Active || r.Start == r.End
}

// Highlight wraps the selected cells of row, item id's rendered line
// (line), with on/off. Existing escape sequences are kept intact. row is
// returned unchanged when it falls outside r, or when r is Empty.
func Highlight(row, id string, line int, r Range, order func(string) int, on, off string) string {
	if r.Empty() {
		return row
	}
	lo, hi := r.Normalized(order)
	oID, oLo, oHi := order(id), order(lo.ID), order(hi.ID)
	if oID < oLo || oID > oHi {
		return row
	}
	if oID == oLo && line < lo.Line {
		return row
	}
	if oID == oHi && line > hi.Line {
		return row
	}

	bounds := cellBounds(row)
	width := bounds[len(bounds)-1]
	left, right := 0, width
	if oID == oLo && line == lo.Line {
		left = snapCol(bounds, lo.Col, false)
	}
	if oID == oHi && line == hi.Line {
		right = snapCol(bounds, hi.Col, true)
	}
	if right <= left {
		return row
	}

	before := ansi.Cut(row, 0, left)
	selected := ansi.Cut(row, left, right)
	after := ansi.Cut(row, right, width)
	return before + on + selected + off + after
}

// Text extracts the plain selected text from lines(id) for every id in
// ids, which runs from lo.ID to hi.ID in order. Trailing whitespace is
// trimmed per line; the lines are joined with "\n" with no trailing
// newline.
func Text(r Range, ids []string, order func(string) int, lines func(id string) []string) string {
	if r.Empty() || len(ids) == 0 {
		return ""
	}
	lo, hi := r.Normalized(order)

	var b strings.Builder
	wrote := false
	for _, id := range ids {
		ls := lines(id)
		oID, oLo, oHi := order(id), order(lo.ID), order(hi.ID)
		for line, raw := range ls {
			if oID == oLo && line < lo.Line {
				continue
			}
			if oID == oHi && line > hi.Line {
				break
			}
			bounds := cellBounds(raw)
			width := bounds[len(bounds)-1]
			left, right := 0, width
			if oID == oLo && line == lo.Line {
				left = snapCol(bounds, lo.Col, false)
			}
			if oID == oHi && line == hi.Line {
				right = snapCol(bounds, hi.Col, true)
			}
			seg := ""
			if right > left {
				seg = xansi.Strip(ansi.Cut(raw, left, right))
			}
			seg = strings.TrimRight(seg, " \t")
			if wrote {
				b.WriteByte('\n')
			}
			b.WriteString(seg)
			wrote = true
		}
	}
	return b.String()
}

// cellBounds returns the cumulative display-cell width after each visible
// grapheme cluster of s, starting with 0. Escape sequences contribute no
// width. The result is strictly increasing except for the leading 0, and
// its last element is s's total width.
func cellBounds(s string) []int {
	bounds := []int{0}
	col := 0
	var state byte
	for i := 0; i < len(s); {
		_, w, n, newState := xansi.DecodeSequence(s[i:], state, nil)
		state = newState
		if n <= 0 {
			n = 1
		}
		if w > 0 {
			col += w
			bounds = append(bounds, col)
		}
		i += n
	}
	return bounds
}

// snapCol clamps col to [0, the last bound] and, when it lands inside a
// wide rune's cell span rather than exactly on a boundary, snaps it to
// that rune's first cell (end == false) or to the cell just after it
// (end == true).
func snapCol(bounds []int, col int, end bool) int {
	width := bounds[len(bounds)-1]
	if col < 0 {
		col = 0
	}
	if col > width {
		col = width
	}
	for i := range len(bounds) - 1 {
		lo, hi := bounds[i], bounds[i+1]
		if col == lo {
			return col
		}
		if col > lo && col < hi {
			if end {
				return hi
			}
			return lo
		}
	}
	return col
}
