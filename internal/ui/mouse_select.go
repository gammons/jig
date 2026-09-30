package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/selection"
	"github.com/gammons/jig/internal/bubbles/wintree"
)

// copySelection extracts the dragged range's plain text in document order
// (or, in the column's body, from its single "line space" for a details
// pane, or its pane's own document order for a transcript pane) and, if
// it isn't empty, copies it and sets the hint.
func (m mouseCtl) copySelection() tea.Cmd {
	a := m.a
	sel := a.view.mouse.sel
	var text string
	if a.view.mouse.pane == regionDetails {
		top := columnTop(a)
		if top != nil && top.kind == paneTranscript {
			ids := paneIDs(top)
			order := indexOrder(ids)
			lo, hi := sel.Normalized(order)
			loI, hiI := order(lo.ID), order(hi.ID)
			var subset []string
			if loI >= 0 && hiI < len(ids) && loI <= hiI {
				subset = ids[loI : hiI+1]
			}
			text = selection.Text(sel, subset, order, func(id string) []string { return blocklist.Lines(top.list, id) })
		} else {
			order := func(string) int { return 0 }
			text = selection.Text(sel, []string{detailsSelID}, order, func(string) []string {
				if top == nil || top.kind != paneDetails {
					return nil
				}
				return top.body.Lines()
			})
		}
	} else {
		ids := transcriptIDs(a)
		order := indexOrder(ids)
		lo, hi := sel.Normalized(order)
		loI, hiI := order(lo.ID), order(hi.ID)
		var subset []string
		if loI >= 0 && hiI < len(ids) && loI <= hiI {
			subset = ids[loI : hiI+1]
		}
		text = selection.Text(sel, subset, order, func(id string) []string { return blocklist.Lines(a.sess.main.list, id) })
	}
	if text == "" {
		return nil
	}
	a.view.hint = fmt.Sprintf("copied %d chars", utf8.RuneCountInString(text))
	return tea.SetClipboard(text)
}

// click selects the block the press landed on, the same as j/k (Select
// scrolls it into view); permCtl.sync, run after every Update, disarms
// the card when the selection lands on it. In the column's details pane
// a click selects nothing (there is no analogous block to select); in a
// transcript pane it selects that pane's block.
func (m mouseCtl) click() tea.Cmd {
	a := m.a
	id := a.view.mouse.sel.Start.ID
	if id == "" {
		return nil
	}
	switch a.view.mouse.pane {
	case regionTranscript:
		a.sess.main.list.Select(id)
	case regionDetails:
		if top := columnTop(a); top != nil && top.kind == paneTranscript {
			top.list.Select(id)
		}
	}
	return nil
}

// transcriptIDs lists every transcript block's ID in document order.
func transcriptIDs(a *App) []string {
	return paneIDs(a.sess.main)
}

// paneIDs lists every block ID in p's projection, in document order.
func paneIDs(p *pane) []string {
	blocks := p.proj.Blocks()
	ids := make([]string, len(blocks))
	for i, b := range blocks {
		ids[i] = string(b.ID)
	}
	return ids
}

// indexOrder returns the selection order function for ids: an id's
// position in ids, or len(ids) for one not found (so it sorts after every
// known id, and never panics).
func indexOrder(ids []string) func(string) int {
	idx := make(map[string]int, len(ids))
	for i, id := range ids {
		idx[id] = i
	}
	return func(id string) int {
		if i, ok := idx[id]; ok {
			return i
		}
		return len(ids)
	}
}

// paintSelection draws the active drag's highlight over rendered (a
// pane's already-composed view, exactly r.W×r.H cells). A transcript
// selection whose block no longer exists (a Load dropped it) is cleared
// here, at paint time, rather than from onEvent (spec §5). rendered is
// returned unchanged when there is no active, non-empty selection for p,
// or for any other pane.
func paintSelection(a *App, p mouseRegion, rendered string, r wintree.Rect) string {
	if a.view.mouse.pane == regionTranscript {
		clearGoneSelection(a)
	}
	sel := a.view.mouse.sel
	if p != a.view.mouse.pane || !sel.Active || sel.Empty() || (p == regionTranscript && r.W < 3) {
		return rendered
	}
	order := paneOrder(a, p)
	on, off := a.theme.set.Selection.On, a.theme.set.Selection.Off
	listPane := p == regionTranscript
	x := 1
	if p == regionDetails {
		x = 0
		if top := columnTop(a); top != nil && top.kind == paneTranscript {
			listPane = true
			x = 1
		}
	}
	lines := strings.Split(rendered, "\n")
	for y := range lines {
		if y >= r.H {
			break
		}
		id, line, _, ok := hitTest(a, p, x, y)
		if !ok {
			continue
		}
		lines[y] = paintRow(lines[y], id, line, sel, order, on, off, p, r.W, listPane)
	}
	return strings.Join(lines, "\n")
}

// paneOrder is p's selection order function: document order for the
// transcript, or, for the column, document order of its top transcript
// pane, or a constant (there is only one ID) for its details pane.
func paneOrder(a *App, p mouseRegion) func(string) int {
	if p == regionDetails {
		if top := columnTop(a); top != nil && top.kind == paneTranscript {
			return indexOrder(paneIDs(top))
		}
		return func(string) int { return 0 }
	}
	return indexOrder(transcriptIDs(a))
}

// paintRow highlights row's selected cells. For the transcript, and for
// a transcript pane in the column, it skips the prefix column (x=0, the
// "▌" bar) and the scrollbar column (x=w-1); for the column's details
// pane the whole row is eligible.
func paintRow(row, id string, line int, sel selection.Range, order func(string) int, on, off string, p mouseRegion, w int, listPane bool) string {
	if p != regionTranscript && !listPane {
		return selection.Highlight(row, id, line, sel, order, on, off)
	}
	width := ansi.Width(row)
	prefix := ansi.Cut(row, 0, 1)
	content := ansi.Cut(row, 1, w-1)
	tail := ansi.Cut(row, w-1, width)
	return prefix + selection.Highlight(content, id, line, sel, order, on, off) + tail
}

// clearGoneSelection clears an active transcript selection whose Start or
// End block no longer exists in the projection.
func clearGoneSelection(a *App) {
	sel := a.view.mouse.sel
	if !sel.Active {
		return
	}
	ids := transcriptIDs(a)
	order := indexOrder(ids)
	if order(sel.Start.ID) >= len(ids) || order(sel.End.ID) >= len(ids) {
		a.view.mouse.sel = selection.Range{}
	}
}
