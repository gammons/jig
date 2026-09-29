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

// wheelLines is how many lines one wheel notch scrolls (spec §3.1).
const wheelLines = 3

// pane is which region of the layout a mouse cell falls in.
type pane int

const (
	paneNone pane = iota
	paneTranscript
	paneDetails
)

// dragPhase is the mouse's drag state machine (spec §3.2/§3.3).
type dragPhase int

const (
	dragIdle    dragPhase = iota
	dragPressed           // pressed, not yet moved: a release here is a click
	dragMoving            // moved since the press: a release here copies
)

// mouseState is the App's drag state (viewState's mouse field). phase is
// the drag state machine, pane is which pane the drag started in (motion
// is pinned to it so a selection never crosses panes), and sel is the
// range being dragged (or the last one drawn, after release: it stays
// Active until a new press, a wheel event, or esc clears it). 5b adds the
// last pointer cell and the auto-scroll tick generation for auto-scroll;
// they aren't declared here since this task never reads them
// (golangci-lint's unused check flags a field that is only written).
type mouseState struct {
	phase dragPhase
	pane  pane
	sel   selection.Range
}

// mouseCtl handles the App's mouse messages.
type mouseCtl struct{ a *App }

// handle routes one mouse message: the wheel scrolls; a press starts a
// drag (or, on release without motion, a click); motion extends the
// drag's selection; release copies it (or, for a click, selects the
// block).
func (m mouseCtl) handle(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	case tea.MouseClickMsg:
		return m.press(msg)
	case tea.MouseMotionMsg:
		return m.motion(msg)
	case tea.MouseReleaseMsg:
		return m.release(msg)
	}
	return nil
}

// paneAt maps a screen cell to the pane it falls in: the transcript, or
// the details split's body when it is open. Everything else — the gap,
// the prompt, the status bar, and the sidebar — is paneNone.
func paneAt(a *App, x, y int) pane {
	if inRect(a.lay.Transcript, x, y) {
		return paneTranscript
	}
	if a.lay.DetailsOpen && inRect(a.lay.Side, x, y) {
		return paneDetails
	}
	return paneNone
}

// inRect reports whether (x, y) falls inside r.
func inRect(r wintree.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// wheel scrolls the pane under the pointer by wheelLines, moving the
// highlight (blocklist.ScrollBy/details.ScrollBy do this themselves), and
// clears any selection. It does nothing while the picker is open, or
// outside the transcript and the (open) details split.
func (m mouseCtl) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	a := m.a
	a.view.mouse.sel = selection.Range{}
	if a.mode == modePicker {
		return nil
	}
	ms := msg.Mouse()
	n := wheelLines
	if ms.Button == tea.MouseWheelUp {
		n = -wheelLines
	}
	switch paneAt(a, ms.X, ms.Y) {
	case paneTranscript:
		a.w.list.ScrollBy(n)
	case paneDetails:
		a.w.details.ScrollBy(n)
	}
	return nil
}

// press starts a drag: it clears any previous selection, then, for a
// left-button press landing in the transcript, hit-tests the cell and
// starts a pending drag there. A press on the prefix or scrollbar column
// (HitTest's ok=false), on any other button, outside the transcript, or
// in a pane whose body is too short to drag in, starts nothing.
func (m mouseCtl) press(msg tea.MouseClickMsg) tea.Cmd {
	a := m.a
	a.view.mouse = mouseState{}
	ms := msg.Mouse()
	if ms.Button != tea.MouseLeft {
		return nil
	}
	if paneAt(a, ms.X, ms.Y) != paneTranscript {
		return nil
	}
	r := a.lay.Transcript
	if r.H <= 1 {
		return nil
	}
	id, line, col, ok := blocklist.HitTest(a.w.list, ms.X-r.X, ms.Y-r.Y)
	if !ok {
		return nil
	}
	pt := selection.Point{ID: id, Line: line, Col: col}
	a.view.mouse.phase = dragPressed
	a.view.mouse.pane = paneTranscript
	a.view.mouse.sel = selection.Range{Start: pt, End: pt}
	return nil
}

// motion extends the drag in progress to the pointer's current cell,
// pinned to the pane the drag started in (spec §3.2) so a selection never
// crosses panes. It does nothing outside a drag.
func (m mouseCtl) motion(msg tea.MouseMotionMsg) tea.Cmd {
	a := m.a
	if a.view.mouse.phase == dragIdle {
		return nil
	}
	ms := msg.Mouse()
	switch a.view.mouse.pane {
	case paneTranscript:
		r := a.lay.Transcript
		lx := max(1, min(ms.X-r.X, r.W-2))
		ly := max(0, min(ms.Y-r.Y, r.H-1))
		id, line, col, ok := blocklist.HitTest(a.w.list, lx, ly)
		if !ok {
			return nil
		}
		a.view.mouse.sel.End = selection.Point{ID: id, Line: line, Col: col}
		a.view.mouse.sel.Active = true
		a.view.mouse.phase = dragMoving
	}
	return nil
}

// release ends the drag: after real motion it copies the selected text to
// the clipboard and shows the "copied N chars" hint (nothing if the
// extracted text is empty); after a press with no motion it is a click,
// which selects the block under the pointer (§3.3). Either way phase
// returns to idle.
func (m mouseCtl) release(tea.MouseReleaseMsg) tea.Cmd {
	a := m.a
	phase := a.view.mouse.phase
	a.view.mouse.phase = dragIdle
	switch phase {
	case dragMoving:
		return m.copySelection()
	case dragPressed:
		return m.click()
	}
	return nil
}

// copySelection extracts the dragged range's plain text in document order
// and, if it isn't empty, copies it and sets the hint.
func (m mouseCtl) copySelection() tea.Cmd {
	a := m.a
	ids := transcriptIDs(a)
	order := indexOrder(ids)
	sel := a.view.mouse.sel
	lo, hi := sel.Normalized(order)
	loI, hiI := order(lo.ID), order(hi.ID)
	var subset []string
	if loI >= 0 && hiI < len(ids) && loI <= hiI {
		subset = ids[loI : hiI+1]
	}
	text := selection.Text(sel, subset, order, func(id string) []string { return blocklist.Lines(a.w.list, id) })
	if text == "" {
		return nil
	}
	a.view.hint = fmt.Sprintf("copied %d chars", utf8.RuneCountInString(text))
	return tea.SetClipboard(text)
}

// click selects the block the press landed on, the same as j/k (Select
// scrolls it into view); permCtl.sync, run after every Update, disarms
// the card when the selection lands on it.
func (m mouseCtl) click() tea.Cmd {
	a := m.a
	if a.view.mouse.pane != paneTranscript {
		return nil
	}
	if id := a.view.mouse.sel.Start.ID; id != "" {
		a.w.list.Select(id)
	}
	return nil
}

// transcriptIDs lists every transcript block's ID in document order.
func transcriptIDs(a *App) []string {
	blocks := a.sess.proj.Blocks()
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
// pane's already-composed view, exactly r.W×r.H cells), for the
// transcript only (the details pane is 5b's). It walks only the visible
// rows, mapping each back to (ID, line) with blocklist.HitTest, and skips
// the prefix column (x=0, the "▌" bar) and the scrollbar column
// (x=r.W-1). rendered is returned unchanged when there is no active,
// non-empty selection, or for any other pane.
func paintSelection(a *App, p pane, rendered string, r wintree.Rect) string {
	sel := a.view.mouse.sel
	if p != paneTranscript || !sel.Active || sel.Empty() || r.W < 3 {
		return rendered
	}
	order := indexOrder(transcriptIDs(a))
	on, off := a.theme.set.Selection.On, a.theme.set.Selection.Off
	lines := strings.Split(rendered, "\n")
	for y := range lines {
		if y >= r.H {
			break
		}
		id, line, _, ok := blocklist.HitTest(a.w.list, 1, y)
		if !ok {
			continue
		}
		row := lines[y]
		width := ansi.Width(row)
		prefix := ansi.Cut(row, 0, 1)
		content := ansi.Cut(row, 1, r.W-1)
		tail := ansi.Cut(row, r.W-1, width)
		lines[y] = prefix + selection.Highlight(content, id, line, sel, order, on, off) + tail
	}
	return strings.Join(lines, "\n")
}
