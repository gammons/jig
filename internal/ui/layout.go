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
// 32% of the width, clamped to 30–50; the details split takes 50%. Both
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
// the side slot (sidebar or details split), the blank gap above the
// prompt, the prompt, and the status bar, inside an MX-column left/right
// and MY-row top/bottom margin. When SideSpans, the side slot runs from
// the top margin down beside the gap and the prompt to the status bar, and
// the gap and prompt are only as wide as the transcript column; the status
// bar always spans the full inner width. Narrow is terminal width < 120;
// SideVisible is whether the sidebar is shown (never while the details
// split holds the side slot).
type rects struct {
	Transcript, Side, Gap, Prompt, Status       wintree.Rect
	MX, MY                                      int
	Narrow, SideVisible, DetailsOpen, SideSpans bool
}

// computeLayout lays out a w×h terminal with a promptH-row prompt. The
// sidebar is visible iff w ≥ 120 and sidebarPref is nil or true; the
// details split, when open, takes the side slot at 50% of the width, or
// the whole transcript region when narrow. The margin rows are kept only
// while the body still keeps its gap and a transcript row, and the gap
// only while the transcript keeps a row, so on a short terminal the
// margin goes first, then the gap, and neither ever squeezes the prompt
// or status bar. The side margin goes below marginMinW columns. Negative
// sizes count as 0.
func computeLayout(w, h, promptH int, sidebarPref *bool, detailsOpen bool) rects {
	w, h = max(w, 0), max(h, 0)
	mx := 0
	if w >= marginMinW {
		mx = margin
	}
	narrow := w < sidebarMinTerm
	if h > 2*margin {
		r := layoutBody(w-2*mx, h-2*margin, promptH, sidebarPref, detailsOpen, narrow)
		if r.Gap.H == gapRows {
			return r.shift(mx, margin)
		}
	}
	return layoutBody(w-2*mx, h, promptH, sidebarPref, detailsOpen, narrow).shift(mx, 0)
}

// shift moves every rect of r right by mx and down by my, recording them
// as its margins.
func (r rects) shift(mx, my int) rects {
	for _, rc := range []*wintree.Rect{&r.Transcript, &r.Side, &r.Gap, &r.Prompt, &r.Status} {
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
func layoutBody(w, h, promptH int, sidebarPref *bool, detailsOpen, narrow bool) rects {
	r := rects{Narrow: narrow, DetailsOpen: detailsOpen}
	r.SideVisible = !detailsOpen && !r.Narrow && (sidebarPref == nil || *sidebarPref)

	side := 0
	switch {
	case detailsOpen && r.Narrow:
		side = w
	case detailsOpen:
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
	// bottom gapRows of it.
	region := max(r.Transcript.H, r.Side.H)
	if region > gapRows {
		top := r.Prompt.Y - gapRows
		r.Gap = wintree.Rect{X: 0, Y: top, W: w, H: gapRows}
		r.Transcript.H = max(r.Transcript.H-gapRows, 0)
		if side > 0 {
			r.Side.H = max(r.Side.H-gapRows, 0)
		}
	}
	// Beside a transcript column, the side slot runs on down beside the
	// gap and the prompt, which narrow to the transcript's blocks.
	if side > 0 && r.Transcript.W > 0 {
		r.SideSpans = true
		r.Side.H += r.Gap.H + r.Prompt.H
		r.Gap.W = r.Transcript.W
		r.Prompt.W = max(r.Transcript.W-1-rightPad, 0)
	}
	return r
}

// layoutFor computes a's layout and sizes the prompt to it. The prompt's width comes from the layout and
// its height (it wraps) feeds the layout, so a width change that re-wraps
// the prompt is laid out once more at the new height. a.lay.Prompt.W is
// the width the prompt was last given (0 before the first layout).
func layoutFor(a *App) rects {
	detailsOpen := a.view.detailsOpen
	lay := computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, detailsOpen)
	if lay.Prompt.W != a.lay.Prompt.W || a.lay.Prompt.W == 0 {
		a.w.prompt.SetWidth(lay.Prompt.W)
		lay = computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, detailsOpen)
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

// fit clips or pads s to exactly w×h cells: at most h lines, each cut or
// space-padded to width w. It returns "" when h is 0.
func fit(s string, w, h int) string {
	if h <= 0 {
		return ""
	}
	w = max(w, 0)
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], w, "")
		}
		if pad := w - ansi.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

// compose joins the regions of one frame per lay, inside its margins:
// the transcript beside the side slot; below the transcript, the blank
// gap and the prompt (when SideSpans, these sit in the transcript's
// column, each row ending in border, and the side slot runs on beside
// them); then the full-width status bar. Each region is fit to its rect
// first, so the frame is exactly the terminal's size.
func compose(lay rects, transcript, side, prompt, status, border string) string {
	left := []string{}
	if lay.Transcript.H > 0 {
		left = append(left, fit(transcript, lay.Transcript.W, lay.Transcript.H))
	}
	under := func(s string, rc wintree.Rect) {
		if rc.H <= 0 {
			return
		}
		if !lay.SideSpans {
			left = append(left, fit(s, rc.W, rc.H))
			return
		}
		// Fill the transcript column exactly: the content (never into
		// the border column), blank columns up to the border column, then
		// the border.
		w := lay.Transcript.W
		cw := min(rc.W, max(w-1, 0))
		pad := fit("", max(w-1-cw, 0), rc.H)
		col := fit(strings.TrimSuffix(strings.Repeat(border+"\n", rc.H), "\n"), 1, rc.H)
		left = append(left, lipgloss.JoinHorizontal(lipgloss.Top, fit(s, cw, rc.H), pad, col))
	}
	under("", lay.Gap)
	under(prompt, lay.Prompt)

	var rows []string
	body := lipgloss.JoinVertical(lipgloss.Left, left...)
	switch {
	case lay.SideSpans:
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, body, fit(side, lay.Side.W, lay.Side.H)))
	case lay.Side.W > 0 && lay.Side.H > 0:
		top := fit(side, lay.Side.W, lay.Side.H)
		if lay.Transcript.W > 0 && lay.Transcript.H > 0 {
			top = lipgloss.JoinHorizontal(lipgloss.Top, left[0], top)
		}
		rows = append(rows, top)
		if len(left) > 1 {
			rows = append(rows, left[1:]...)
		}
	case len(left) > 0:
		rows = append(rows, body)
	}
	if lay.Status.H > 0 {
		rows = append(rows, fit(status, lay.Status.W, lay.Status.H))
	}
	return frame(lay, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// frame surrounds body with lay's margins: MY blank rows above and below,
// MX blank columns left and right.
func frame(lay rects, body string) string {
	if lay.MX == 0 && lay.MY == 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, ansi.Width(l))
	}
	side := strings.Repeat(" ", lay.MX)
	blank := strings.Repeat(" ", w+2*lay.MX)
	out := make([]string, 0, len(lines)+2*lay.MY)
	for range lay.MY {
		out = append(out, blank)
	}
	for _, l := range lines {
		out = append(out, side+l+strings.Repeat(" ", max(w-ansi.Width(l), 0))+side)
	}
	for range lay.MY {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}
