package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/wintree"
)

// Layout constants (spec §4): the sidebar shows at ≥ 120 columns and takes
// 32% of the width, clamped to 30–50; the column takes 50%. Both
// are measured inside the margin.
const (
	sidebarMinTerm = 120
	sidebarPercent = 32
	sidebarMin     = 30
	sidebarMax     = 50
	detailsPercent = 50
	statusRows     = 1
	// gapRows blank rows separate the transcript from the prompt, so model
	// output doesn't butt against the input box.
	gapRows = 1
	// belowRows blank rows separate the prompt from the status bar.
	belowRows = 1
	// margin blank cells surround the whole frame on every side, so
	// nothing butts against the terminal's edges.
	margin = 1
	// marginMinW is the narrowest terminal that keeps the left/right
	// margin; below it every column goes to the body.
	marginMinW = 20
)

// splitBounds is the nominal size the window tree is split at. Splits are
// refused when a flex leaf would fall below wintree's minimums, so the tree
// is shaped at a size where every split fits, then laid out at the real
// size (which may be anything, down to 0×0).
const splitBounds = 1000

// rects is one frame's layout, in terminal coordinates: the transcript,
// the side slot (sidebar or the column), the blank gap above the
// prompt, the prompt, the blank row(s) below it, and the status bar,
// inside an MX-column left/right and MY-row top/bottom margin. When
// SideSpans, the side slot runs from the top margin down beside the gap,
// the prompt, and the row below it to the status bar, and those are only
// as wide as the transcript column; the status bar always spans the full
// inner width. Narrow is terminal width < 120;
// SideVisible is whether the sidebar is shown (never while the column
// holds the side slot). MainCrumb is whether main's region carries its
// own breadcrumb+rule rows (narrow, column open, main focused).
type rects struct {
	Transcript, Side, Gap, Prompt, Below, Status wintree.Rect
	MX, MY                                       int
	Narrow, SideVisible, ColumnOpen, SideSpans   bool
	MainCrumb                                    bool
}

// computeLayout lays out a w×h terminal with a promptH-row prompt. The
// sidebar is visible iff w ≥ 120 and sidebarPref is nil or true; the
// column, when open, takes the side slot at 50% of the width when wide,
// or, when narrow, the whole transcript region if it has focus, else
// none of it (main takes the whole region instead, with its own
// breadcrumb rows). The margin rows are kept only
// while the body still keeps both blank rows and a transcript row; the
// row below the prompt only while the gap and a transcript row stay; the
// gap only while the transcript keeps a row. So on a short terminal the
// margin goes first, then the row below the prompt, then the gap, and
// none ever squeezes the prompt or status bar. The side margin goes below marginMinW columns. Negative
// sizes count as 0.
func computeLayout(w, h, promptH int, sidebarPref *bool, columnOpen bool, focus focus) rects {
	w, h = max(w, 0), max(h, 0)
	mx := 0
	if w >= marginMinW {
		mx = margin
	}
	narrow := w < sidebarMinTerm
	if h > 2*margin {
		r := layoutBody(w-2*mx, h-2*margin, promptH, sidebarPref, columnOpen, narrow, focus)
		if r.Below.H == belowRows {
			return r.shift(mx, margin)
		}
	}
	return layoutBody(w-2*mx, h, promptH, sidebarPref, columnOpen, narrow, focus).shift(mx, 0)
}

// shift moves every rect of r right by mx and down by my, recording them
// as its margins.
func (r rects) shift(mx, my int) rects {
	for _, rc := range []*wintree.Rect{&r.Transcript, &r.Side, &r.Gap, &r.Prompt, &r.Below, &r.Status} {
		if rc.W > 0 || rc.H > 0 {
			rc.X += mx
			rc.Y += my
		}
	}
	r.MX, r.MY = mx, my
	return r
}

// layoutBody lays out everything inside the margin in a w×h area, at the
// origin; see computeLayout. narrow is decided by the terminal's width,
// not w.
func layoutBody(w, h, promptH int, sidebarPref *bool, columnOpen, narrow bool, focus focus) rects {
	r := rects{Narrow: narrow, ColumnOpen: columnOpen}
	r.SideVisible = !columnOpen && !r.Narrow && (sidebarPref == nil || *sidebarPref)

	side := 0
	switch {
	case columnOpen && r.Narrow && focus == focusColumn:
		side = w
	case columnOpen && r.Narrow:
		r.MainCrumb = true
	case columnOpen:
		side = w * detailsPercent / 100
	case r.SideVisible:
		side = min(max(w*sidebarPercent/100, sidebarMin), sidebarMax)
	}

	nominal := wintree.Rect{W: splitBounds, H: splitBounds}
	tree, trans := wintree.New()
	prompt, _ := tree.Split(trans, wintree.SplitStacked, nominal)
	status, _ := tree.Split(prompt, wintree.SplitStacked, nominal)
	_ = tree.SetFixed(prompt, max(promptH, 0))
	_ = tree.SetFixed(status, statusRows)
	var sideID wintree.LeafID
	if side > 0 {
		sideID, _ = tree.Split(trans, wintree.SplitSideBySide, nominal)
		_ = tree.SetFixed(sideID, side)
	}

	all := tree.ComputeRects(wintree.Rect{W: w, H: h})
	r.Transcript, r.Prompt, r.Status = all[trans], all[prompt], all[status]
	if side > 0 {
		r.Side = all[sideID]
	}
	// The transcript and side slot share one row span; the gap takes the
	// bottom gapRows of it, and then, while a transcript row and the gap
	// remain, the row below the prompt takes belowRows more, moving the
	// gap and prompt up.
	carve := func(n int) bool {
		if max(r.Transcript.H, r.Side.H) <= n {
			return false
		}
		r.Transcript.H = max(r.Transcript.H-n, 0)
		if side > 0 {
			r.Side.H = max(r.Side.H-n, 0)
		}
		return true
	}
	if carve(gapRows) {
		r.Gap = wintree.Rect{X: 0, Y: r.Prompt.Y - gapRows, W: w, H: gapRows}
		if carve(belowRows) {
			r.Gap.Y -= belowRows
			r.Prompt.Y -= belowRows
			r.Below = wintree.Rect{X: 0, Y: r.Prompt.Y + r.Prompt.H, W: w, H: belowRows}
		}
	}
	// Beside a transcript column, the side slot runs on down beside the
	// gap, the prompt, and the row below it, which narrow to the
	// transcript's blocks.
	if side > 0 && r.Transcript.W > 0 {
		r.SideSpans = true
		r.Side.H += r.Gap.H + r.Prompt.H + r.Below.H
		r.Gap.W = r.Transcript.W
		r.Below.W = r.Transcript.W
		r.Prompt.W = max(r.Transcript.W-1-rightPad, 0)
	}
	return r
}

// layoutFor computes a's layout and sizes the prompt to it. The prompt's width comes from the layout and
// its height (it wraps) feeds the layout, so a width change that re-wraps
// the prompt is laid out once more at the new height. a.lay.Prompt.W is
// the width the prompt was last given (0 before the first layout).
func layoutFor(a *App) rects {
	columnOpen := columnOpen(a)
	lay := computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, columnOpen, a.view.focus)
	if lay.Prompt.W != a.lay.Prompt.W || a.lay.Prompt.W == 0 {
		a.w.prompt.SetWidth(lay.Prompt.W)
		lay = computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, columnOpen, a.view.focus)
	}
	return lay
}

// borderCell is one cell of the transcript's right-hand border (its bare
// scrollbar track), for the rows below the transcript that the side slot
// runs beside.
func borderCell(st blocklist.Styles) string {
	return lipgloss.NewStyle().Background(orNoColor(st.ScrollBg)).Foreground(orNoColor(st.Track)).Render("│")
}

// orNoColor maps a nil color to lipgloss.NoColor.
func orNoColor(c color.Color) color.Color {
	if c == nil {
		return lipgloss.NoColor{}
	}
	return c
}

// region is a block of rendered rows, each exactly w cells wide. Joining
// regions needs no measuring: every row's width is already known, which
// keeps composing a frame to one width pass over each widget's output.
type region struct {
	rows []string
	w    int
}

// fitRows clips or pads s to exactly w×h cells: h rows (missing ones
// blank), each with tabs expanded to 4 spaces, then cut or space-padded
// to width w. Each line is measured once (twice when it is cut). It
// returns an empty region when h is 0.
func fitRows(s string, w, h int) region {
	if h <= 0 {
		return region{}
	}
	w = max(w, 0)
	r := region{rows: make([]string, h), w: w}
	blank := strings.Repeat(" ", w)
	rest, more := s, true
	for i := range r.rows {
		if !more {
			r.rows[i] = blank
			continue
		}
		var line string
		line, rest, more = strings.Cut(rest, "\n")
		r.rows[i] = fitRow(line, w, blank)
	}
	return r
}

// fitRow cuts or pads one line to exactly w cells; blank is w spaces.
func fitRow(line string, w int, blank string) string {
	if line == "" {
		return blank
	}
	if strings.Contains(line, "\t") {
		line = strings.ReplaceAll(line, "\t", "    ")
	}
	lw := ansi.Width(line)
	if lw > w {
		line = ansi.Truncate(line, w, "")
		lw = ansi.Width(line)
	}
	if lw < w {
		line += blank[:w-lw]
	}
	return line
}

// repeatRows is h rows of cell, a string one cell wide.
func repeatRows(cell string, h int) region {
	r := region{rows: make([]string, max(h, 0)), w: 1}
	for i := range r.rows {
		r.rows[i] = cell
	}
	return r
}

// hjoin places parts side by side, top-aligned: as tall as the tallest,
// the missing rows of a shorter part blank.
func hjoin(parts ...region) region {
	h, w := 0, 0
	for _, p := range parts {
		h = max(h, len(p.rows))
		w += p.w
	}
	out := region{rows: make([]string, h), w: w}
	var b strings.Builder
	for i := range out.rows {
		b.Reset()
		b.Grow(w * 2)
		for _, p := range parts {
			if i < len(p.rows) {
				b.WriteString(p.rows[i])
			} else {
				b.WriteString(strings.Repeat(" ", p.w))
			}
		}
		out.rows[i] = b.String()
	}
	return out
}

// vjoin stacks parts, left-aligned, padding every row to the widest part.
// No parts is one empty row.
func vjoin(parts ...region) region {
	if len(parts) == 0 {
		return region{rows: []string{""}}
	}
	n, w := 0, 0
	for _, p := range parts {
		n += len(p.rows)
		w = max(w, p.w)
	}
	out := region{rows: make([]string, 0, n), w: w}
	for _, p := range parts {
		pad := strings.Repeat(" ", w-p.w)
		for _, row := range p.rows {
			out.rows = append(out.rows, row+pad)
		}
	}
	return out
}

// compose joins the regions of one frame per lay, inside its margins:
// the transcript beside the side slot; below the transcript, the blank
// gap, the prompt, and the blank row below it (when SideSpans, these sit
// in the transcript's column, each row ending in border, and the side
// slot runs on beside them); then the full-width status bar. Each region is fit to its rect
// first, so the frame is exactly the terminal's size.
func compose(lay rects, transcript, side, prompt, status, border string) string {
	left := []region{}
	if lay.Transcript.H > 0 {
		left = append(left, fitRows(transcript, lay.Transcript.W, lay.Transcript.H))
	}
	borderCol := fitRow(border, 1, " ")
	under := func(s string, rc wintree.Rect) {
		if rc.H <= 0 {
			return
		}
		if !lay.SideSpans {
			left = append(left, fitRows(s, rc.W, rc.H))
			return
		}
		// Fill the transcript column exactly: the content (never into
		// the border column), blank columns up to the border column, then
		// the border.
		w := lay.Transcript.W
		cw := min(rc.W, max(w-1, 0))
		pad := fitRows("", max(w-1-cw, 0), rc.H)
		left = append(left, hjoin(fitRows(s, cw, rc.H), pad, repeatRows(borderCol, rc.H)))
	}
	under("", lay.Gap)
	under(prompt, lay.Prompt)
	under("", lay.Below)

	var rows []region
	switch {
	case lay.SideSpans:
		rows = append(rows, hjoin(vjoin(left...), fitRows(side, lay.Side.W, lay.Side.H)))
	case lay.Side.W > 0 && lay.Side.H > 0:
		top := fitRows(side, lay.Side.W, lay.Side.H)
		if lay.Transcript.W > 0 && lay.Transcript.H > 0 {
			top = hjoin(left[0], top)
		}
		rows = append(rows, top)
		if len(left) > 1 {
			rows = append(rows, left[1:]...)
		}
	case len(left) > 0:
		rows = append(rows, vjoin(left...))
	}
	if lay.Status.H > 0 {
		rows = append(rows, fitRows(status, lay.Status.W, lay.Status.H))
	}
	return frame(lay, vjoin(rows...))
}

// frame surrounds body with lay's margins, MY blank rows above and below
// and MX blank columns left and right, and joins the frame's rows.
func frame(lay rects, body region) string {
	if lay.MX == 0 && lay.MY == 0 {
		return strings.Join(body.rows, "\n")
	}
	side := strings.Repeat(" ", lay.MX)
	blank := strings.Repeat(" ", body.w+2*lay.MX)
	var b strings.Builder
	b.Grow((len(body.rows) + 2*lay.MY) * (len(blank) + 1) * 2)
	first := true
	row := func(parts ...string) {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		for _, p := range parts {
			b.WriteString(p)
		}
	}
	for range lay.MY {
		row(blank)
	}
	for _, l := range body.rows {
		row(side, l, side)
	}
	for range lay.MY {
		row(blank)
	}
	return b.String()
}
