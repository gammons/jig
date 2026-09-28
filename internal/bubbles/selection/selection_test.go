package selection

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

// order over a:0, b:1, unknown IDs after every known one.
func order2(id string) int {
	switch id {
	case "a":
		return 0
	case "b":
		return 1
	default:
		return 2
	}
}

func TestBefore_OrdersByItemThenLineThenCol(t *testing.T) {
	t.Parallel()
	if !Before(Point{ID: "a", Line: 5, Col: 9}, Point{ID: "b", Line: 0, Col: 0}, order2) {
		t.Error("a comes before b regardless of line/col")
	}
	if !Before(Point{ID: "a", Line: 1, Col: 0}, Point{ID: "a", Line: 1, Col: 3}, order2) {
		t.Error("lower col on the same line/item should be before")
	}
	p := Point{ID: "a", Line: 1, Col: 3}
	if Before(p, p, order2) {
		t.Error("a point must not be before itself")
	}
}

func TestRange_NormalizedAndEmpty(t *testing.T) {
	t.Parallel()
	r := Range{
		Start:  Point{ID: "b", Line: 2, Col: 4},
		End:    Point{ID: "a", Line: 0, Col: 1},
		Active: true,
	}
	lo, hi := r.Normalized(order2)
	if lo != (Point{ID: "a", Line: 0, Col: 1}) {
		t.Errorf("lo = %+v, want {a,0,1}", lo)
	}
	if hi != (Point{ID: "b", Line: 2, Col: 4}) {
		t.Errorf("hi = %+v, want {b,2,4}", hi)
	}

	inactive := Range{Start: Point{ID: "a"}, End: Point{ID: "b"}, Active: false}
	if !inactive.Empty() {
		t.Error("an inactive range must be Empty")
	}

	p := Point{ID: "a", Line: 1, Col: 1}
	sameEndpoints := Range{Start: p, End: p, Active: true}
	if !sameEndpoints.Empty() {
		t.Error("a range with Start == End must be Empty")
	}

	nonEmpty := Range{Start: Point{ID: "a", Line: 0, Col: 0}, End: Point{ID: "a", Line: 0, Col: 1}, Active: true}
	if nonEmpty.Empty() {
		t.Error("a range with different endpoints must not be Empty")
	}
}

func TestHighlight_WrapsOnlySelectedCells(t *testing.T) {
	t.Parallel()
	r := Range{
		Start:  Point{ID: "a", Line: 0, Col: 2},
		End:    Point{ID: "a", Line: 0, Col: 7},
		Active: true,
	}
	got := Highlight("hello world", "a", 0, r, order2, "<", ">")
	if got != "he<llo w>orld" {
		t.Errorf("Highlight = %q, want %q", got, "he<llo w>orld")
	}

	// Same row, outside the range (line 1), is returned unchanged.
	got = Highlight("hello world", "a", 1, r, order2, "<", ">")
	if got != "hello world" {
		t.Errorf("Highlight outside range = %q, want unchanged %q", got, "hello world")
	}
}

func TestHighlight_MiddleLineOfMultiLineRangeIsHighlightedWhole(t *testing.T) {
	t.Parallel()
	r := Range{
		Start:  Point{ID: "a", Line: 0, Col: 3},
		End:    Point{ID: "a", Line: 2, Col: 2},
		Active: true,
	}
	got := Highlight("middle row", "a", 1, r, order2, "<", ">")
	if got != "<middle row>" {
		t.Errorf("Highlight middle line = %q, want %q", got, "<middle row>")
	}
}

func TestHighlight_EmptyRangeReturnsUnchanged(t *testing.T) {
	t.Parallel()
	var r Range
	got := Highlight("hello world", "a", 0, r, order2, "<", ">")
	if got != "hello world" {
		t.Errorf("Highlight with Empty range = %q, want unchanged", got)
	}
}

func TestHighlight_KeepsExistingEscapes(t *testing.T) {
	t.Parallel()
	row := "\x1b[1mbold\x1b[m text"
	r := Range{
		Start:  Point{ID: "a", Line: 0, Col: 2},
		End:    Point{ID: "a", Line: 0, Col: 6},
		Active: true,
	}
	got := Highlight(row, "a", 0, r, order2, "<", ">")
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("Highlight dropped the existing style escape: %q", got)
	}
	wantPlain := xansi.Strip(row)
	gotPlain := strings.NewReplacer("<", "", ">", "").Replace(xansi.Strip(got))
	if gotPlain != wantPlain {
		t.Errorf("plain text changed: got %q, want %q", gotPlain, wantPlain)
	}
}

func TestText_AcrossLinesAndItems(t *testing.T) {
	t.Parallel()
	lines := func(id string) []string {
		switch id {
		case "a":
			return []string{"one two  ", "three"}
		case "b":
			return []string{"four"}
		}
		return nil
	}
	r := Range{
		Start:  Point{ID: "a", Line: 0, Col: 4},
		End:    Point{ID: "b", Line: 0, Col: 2},
		Active: true,
	}
	got := Text(r, []string{"a", "b"}, order2, lines)
	want := "two\nthree\nfo"
	if got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

func TestText_StylesAndWideRunes(t *testing.T) {
	t.Parallel()
	lines := func(id string) []string {
		if id == "a" {
			return []string{"\x1b[31m日本\x1b[m語 x"}
		}
		return nil
	}

	// Col 1 is the second cell of 日 and snaps to col 0.
	r := Range{
		Start:  Point{ID: "a", Line: 0, Col: 1},
		End:    Point{ID: "a", Line: 0, Col: 5},
		Active: true,
	}
	got := Text(r, []string{"a"}, order2, lines)
	if got != "日本語" {
		t.Errorf("Text = %q, want %q", got, "日本語")
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("Text left an escape in the result: %q", got)
	}

	// End col 3 is the second cell of 本.
	r2 := Range{
		Start:  Point{ID: "a", Line: 0, Col: 1},
		End:    Point{ID: "a", Line: 0, Col: 3},
		Active: true,
	}
	got2 := Text(r2, []string{"a"}, order2, lines)
	if got2 != "日本" {
		t.Errorf("Text = %q, want %q", got2, "日本")
	}
}

func TestHighlight_Golden(t *testing.T) {
	t.Parallel()
	on, off := ansi.SGR(lipgloss.Color("#000000"), lipgloss.Color("#5fafff"))
	r := Range{
		Start:  Point{ID: "a", Line: 1, Col: 2},
		End:    Point{ID: "a", Line: 2, Col: 3},
		Active: true,
	}
	rows := []string{
		Highlight("first row of item a", "a", 0, r, order2, on, off),
		Highlight("second row of item a", "a", 1, r, order2, on, off),
		Highlight("third row of item a", "a", 2, r, order2, on, off),
	}
	golden.Assert(t, "highlight", strings.Join(rows, "\n"))
}

// TestText_UnknownEndpointIDDoesNotPanic exercises the documented order
// contract: order treats an ID it doesn't know as coming after every known
// one, rather than panicking. Here the range's high endpoint names an ID
// ("c") absent from ids; since order never matches any id in ids to it,
// Text includes every line of every id (ids is authoritative for
// inclusion; order only normalizes the endpoints).
func TestText_UnknownEndpointIDDoesNotPanic(t *testing.T) {
	t.Parallel()
	lines := func(id string) []string {
		switch id {
		case "a":
			return []string{"one"}
		case "b":
			return []string{"two"}
		}
		return nil
	}
	r := Range{
		Start:  Point{ID: "c", Line: 0, Col: 0}, // unknown, ordered after a and b
		End:    Point{ID: "a", Line: 0, Col: 0},
		Active: true,
	}
	got := Text(r, []string{"a", "b"}, order2, lines)
	if got != "one\ntwo" {
		t.Errorf("Text with an unknown endpoint ID = %q, want %q", got, "one\ntwo")
	}
}

// TestHighlight_UnknownIDDoesNotPanic exercises the same order contract
// for Highlight: an id Highlight is asked to paint that order doesn't
// recognize is treated as coming after every known item, so it can fall
// outside the range without panicking.
func TestHighlight_UnknownIDDoesNotPanic(t *testing.T) {
	t.Parallel()
	r := Range{
		Start:  Point{ID: "a", Line: 0, Col: 0},
		End:    Point{ID: "b", Line: 0, Col: 2},
		Active: true,
	}
	got := Highlight("row", "unknown-id", 0, r, order2, "<", ">")
	if got != "row" {
		t.Errorf("Highlight for an unknown id = %q, want unchanged %q", got, "row")
	}
}
