package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/wintree"
)

// Layout constants (spec §4): the sidebar shows at ≥ 120 columns and takes
// 32% of the width, clamped to 30–50; the details split takes 50%.
const (
	sidebarMinTerm = 120
	sidebarPercent = 32
	sidebarMin     = 30
	sidebarMax     = 50
	detailsPercent = 50
	statusRows     = 1
)

// splitBounds is the nominal size the window tree is split at. Splits are
// refused when a flex leaf would fall below wintree's minimums, so the tree
// is shaped at a size where every split fits, then laid out at the real
// size (which may be anything, down to 0×0).
const splitBounds = 1000

// rects is one frame's layout: the transcript, the side slot (sidebar or
// details split), the prompt, and the status bar. Narrow is width < 120;
// SideVisible is whether the sidebar is shown (never while the details
// split holds the side slot).
type rects struct {
	Transcript, Side, Prompt, Status wintree.Rect
	Narrow, SideVisible, DetailsOpen bool
}

// computeLayout lays out a w×h terminal with a promptH-row prompt. The
// sidebar is visible iff w ≥ 120 and sidebarPref is nil or true; the
// details split, when open, takes the side slot at 50% of the width, or
// the whole transcript region when narrow. Negative sizes count as 0.
func computeLayout(w, h, promptH int, sidebarPref *bool, detailsOpen bool) rects {
	w, h = max(w, 0), max(h, 0)
	r := rects{Narrow: w < sidebarMinTerm, DetailsOpen: detailsOpen}
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
	return r
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

// compose joins the regions of one frame per lay: transcript and side
// side by side, then the prompt, then the status bar. Each region is fit
// to its rect first, so the frame never exceeds the terminal.
func compose(lay rects, transcript, side, prompt, status string) string {
	var top []string
	if lay.Transcript.W > 0 {
		top = append(top, fit(transcript, lay.Transcript.W, lay.Transcript.H))
	}
	if lay.Side.W > 0 {
		top = append(top, fit(side, lay.Side.W, lay.Side.H))
	}
	var rows []string
	if lay.Transcript.H > 0 && len(top) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, top...))
	}
	if lay.Prompt.H > 0 {
		rows = append(rows, fit(prompt, lay.Prompt.W, lay.Prompt.H))
	}
	if lay.Status.H > 0 {
		rows = append(rows, fit(status, lay.Status.W, lay.Status.H))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
