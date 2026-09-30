package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/selection"
	"github.com/gammons/jig/internal/bubbles/wintree"
)

// wheelLines is how many lines one wheel notch scrolls (spec §3.1).
const wheelLines = 3

// autoScrollEvery is how often an edge-pinned drag scrolls its pane by one
// line, extending the selection with it (spec §3.2/§5).
const autoScrollEvery = 50 * time.Millisecond

// detailsSelID is the fixed selection.Point ID for a details-pane
// selection (the pane has no per-item IDs, unlike the transcript).
const detailsSelID = "details"

// mouseCell is a pane-local pointer cell.
type mouseCell = struct{ x, y int }

// mouseScrollMsg is one scheduled auto-scroll tick. gen is checked against
// mouseState.autoGen so a tick superseded by a release, a new press, or a
// later reschedule from the same drag does nothing.
type mouseScrollMsg struct{ gen int }

// pane is which region of the layout a mouse cell falls in.
type mouseRegion int

const (
	regionNone mouseRegion = iota
	regionTranscript
	regionDetails
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
// Active until a new press, a wheel event, or esc clears it). last is the
// pane-local pointer cell as of the most recent press or motion (a
// sentinel {-1,-1} right after a press, before any motion); it drives
// auto-scroll. autoGen increases on every armed tick, release, and press,
// so a stale tick is a no-op. wheel is the wheel's acceleration streak
// (wheelAccel); resetting mouseState (a press, the picker opening) just
// starts the next notch on a new streak.
type mouseState struct {
	phase   dragPhase
	pane    mouseRegion
	sel     selection.Range
	last    mouseCell
	autoGen int
	wheel   wheelAccel
}

// mouseCtl handles the App's mouse messages.
type mouseCtl struct{ a *App }

// handle routes one mouse message: the wheel scrolls; a press starts a
// drag (or, on release without motion, a click); motion extends the
// drag's selection, arming auto-scroll at an edge row; a mouseScrollMsg
// advances an armed auto-scroll; release copies the drag (or, for a
// click, selects the block).
func (m mouseCtl) handle(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	case tea.MouseClickMsg:
		return m.press(msg)
	case tea.MouseMotionMsg:
		return m.motion(msg)
	case tea.MouseReleaseMsg:
		return m.release(msg)
	case mouseScrollMsg:
		return m.tick(msg)
	}
	return nil
}

// paneAt maps a screen cell to the pane it falls in: the transcript, or
// the column's body when it is open. Everything else — the gap,
// the prompt, the status bar, and the sidebar — is regionNone.
func paneAt(a *App, x, y int) mouseRegion {
	if inRect(a.lay.Transcript, x, y) {
		return regionTranscript
	}
	if a.lay.ColumnOpen && inRect(a.lay.Side, x, y) {
		return regionDetails
	}
	return regionNone
}

// inRect reports whether (x, y) falls inside r.
func inRect(r wintree.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// paneRect is p's rect: the transcript's, or the (open) details split's
// side slot.
func paneRect(a *App, p mouseRegion) wintree.Rect {
	if p == regionDetails {
		return a.lay.Side
	}
	return a.lay.Transcript
}

// hitTest maps pane-local (x, y) to a selection.Point's fields: for the
// transcript, blocklist.HitTest's item ID and line; for the column's
// details body, the fixed detailsSelID and details.HitTest's content
// line. Only a details pane at the column's top supports hit-testing;
// anything else there is not draggable (its caller checks bodyHeight
// first).
func hitTest(a *App, p mouseRegion, x, y int) (id string, line, col int, ok bool) {
	switch p {
	case regionTranscript:
		return blocklist.HitTest(a.sess.main.list, x, y)
	case regionDetails:
		top := columnTop(a)
		if top == nil || top.kind != paneDetails {
			return "", 0, 0, false
		}
		line, col, ok = top.body.HitTest(x, y-columnHeaderRows)
		return detailsSelID, line, col, ok
	}
	return "", 0, 0, false
}

// pinCell clamps (x, y) to r's draggable body: for the transcript, x in
// [1, w-2] (skipping the prefix and scrollbar columns) and y in [0, h-1];
// for the column's body, x in [0, w-1] and y in [columnHeaderRows, h-1]
// (skipping the breadcrumb and rule rows).
func pinCell(p mouseRegion, r wintree.Rect, x, y int) (int, int) {
	if p == regionDetails {
		return max(0, min(x, r.W-1)), max(columnHeaderRows, min(y, r.H-1))
	}
	return max(1, min(x, r.W-2)), max(0, min(y, r.H-1))
}

// edgeRow reports whether pane-local y is p's top or bottom row: 0 and
// h-1 for the transcript, or columnHeaderRows (the first body row) and
// h-1 for the column's body. A pane whose body height is at most 1 has
// no edges.
func edgeRow(p mouseRegion, y, h int) bool {
	if p == regionDetails {
		return h-columnHeaderRows > 1 && (y == columnHeaderRows || y == h-1)
	}
	return h > 1 && (y == 0 || y == h-1)
}

// scrollEdge scrolls p by one line toward the edge row y is on (up from
// the top edge, down from the bottom).
func scrollEdge(a *App, p mouseRegion, y int) {
	n := 1
	if (p == regionDetails && y == columnHeaderRows) || (p != regionDetails && y == 0) {
		n = -1
	}
	if p == regionDetails {
		if top := columnTop(a); top != nil && top.kind == paneDetails {
			top.body.ScrollBy(n)
		}
		return
	}
	a.sess.main.list.ScrollBy(n)
}

// wheel scrolls the pane under the pointer, moving the highlight
// (blocklist.ScrollBy/details.ScrollBy do this themselves), and clears
// any selection. A slow notch scrolls wheelLines; a fast streak scrolls
// further (wheelAccel, timed on the App clock). It does nothing while the
// picker is open, or outside the transcript and the (open) column.
func (m mouseCtl) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	a := m.a
	a.view.mouse.sel = selection.Range{}
	if a.mode == modePicker {
		return nil
	}
	ms := msg.Mouse()
	dir := 1
	if ms.Button == tea.MouseWheelUp {
		dir = -1
	}
	switch p := paneAt(a, ms.X, ms.Y); p {
	case regionTranscript:
		a.sess.main.list.ScrollBy(a.view.mouse.wheel.lines(a.opts.Clock.Now(), dir, p))
	case regionDetails:
		if top := columnTop(a); top != nil && top.kind == paneDetails {
			top.body.ScrollBy(a.view.mouse.wheel.lines(a.opts.Clock.Now(), dir, p))
		}
	}
	return nil
}

// bodyHeight is p's draggable body height: the transcript's full height,
// or the column's body height minus its breadcrumb and rule rows.
func bodyHeight(p mouseRegion, r wintree.Rect) int {
	if p == regionDetails {
		return r.H - columnHeaderRows
	}
	return r.H
}

// press starts a drag: it clears any previous selection (bumping autoGen
// so a pending auto-scroll tick from an earlier drag can never fire into
// this one), then, for a left-button press landing in the transcript or
// the (open) details body, hit-tests the cell and starts a pending drag
// there. A press on the prefix or scrollbar column, the details header or
// rule rows (hitTest's ok=false), on any other button, outside both
// panes, or in a pane whose body is too short to drag in, starts nothing.
func (m mouseCtl) press(msg tea.MouseClickMsg) tea.Cmd {
	a := m.a
	a.view.mouse = mouseState{autoGen: a.view.mouse.autoGen + 1, last: mouseCell{x: -1, y: -1}}
	ms := msg.Mouse()
	if ms.Button != tea.MouseLeft {
		return nil
	}
	p := paneAt(a, ms.X, ms.Y)
	if p == regionNone {
		return nil
	}
	if a.mode == modeNormal {
		if p == regionDetails {
			setFocus(a, focusColumn)
		} else {
			setFocus(a, focusMain)
		}
	}
	r := paneRect(a, p)
	if bodyHeight(p, r) <= 1 {
		return nil
	}
	id, line, col, ok := hitTest(a, p, ms.X-r.X, ms.Y-r.Y)
	if !ok {
		return nil
	}
	pt := selection.Point{ID: id, Line: line, Col: col}
	a.view.mouse.phase = dragPressed
	a.view.mouse.pane = p
	a.view.mouse.sel = selection.Range{Start: pt, End: pt}
	return nil
}

// motion extends the drag in progress to the pointer's current cell,
// pinned to the pane the drag started in (spec §3.2) so a selection never
// crosses panes. It does nothing outside a drag. On an edge row it arms
// auto-scroll.
func (m mouseCtl) motion(msg tea.MouseMotionMsg) tea.Cmd {
	a := m.a
	if a.view.mouse.phase == dragIdle {
		return nil
	}
	p := a.view.mouse.pane
	r := paneRect(a, p)
	ms := msg.Mouse()
	lx, ly := pinCell(p, r, ms.X-r.X, ms.Y-r.Y)
	id, line, col, ok := hitTest(a, p, lx, ly)
	if !ok {
		return nil
	}
	a.view.mouse.sel.End = selection.Point{ID: id, Line: line, Col: col}
	a.view.mouse.sel.Active = true
	a.view.mouse.phase = dragMoving
	return m.armAutoScroll(p, lx, ly, r.H)
}

// armAutoScroll records (x, y) as the drag's last pointer cell and, the
// first time it lands on an edge row (the previous last cell was not also
// on one), schedules the first auto-scroll tick.
func (m mouseCtl) armAutoScroll(p mouseRegion, x, y, h int) tea.Cmd {
	a := m.a
	wasEdge := edgeRow(p, a.view.mouse.last.y, h)
	a.view.mouse.last = mouseCell{x: x, y: y}
	if wasEdge || !edgeRow(p, y, h) {
		return nil
	}
	a.view.mouse.autoGen++
	return a.after(autoScrollEvery, mouseScrollMsg{gen: a.view.mouse.autoGen})
}

// tick advances one armed auto-scroll: it scrolls the drag's pane by one
// line toward the edge the pointer is pinned to, re-hit-tests the last
// pointer cell to extend the selection, and reschedules itself. A stale
// generation, a drag no longer moving, or a pointer that has left the
// edge row stops it silently.
func (m mouseCtl) tick(msg mouseScrollMsg) tea.Cmd {
	a := m.a
	if msg.gen != a.view.mouse.autoGen || a.view.mouse.phase != dragMoving {
		return nil
	}
	p := a.view.mouse.pane
	r := paneRect(a, p)
	last := a.view.mouse.last
	if !edgeRow(p, last.y, r.H) {
		return nil
	}
	scrollEdge(a, p, last.y)
	if id, line, col, ok := hitTest(a, p, last.x, last.y); ok {
		a.view.mouse.sel.End = selection.Point{ID: id, Line: line, Col: col}
	}
	return a.after(autoScrollEvery, mouseScrollMsg{gen: msg.gen})
}

// release ends the drag: after real motion it copies the selected text to
// the clipboard and shows the "copied N chars" hint (nothing if the
// extracted text is empty); after a press with no motion it is a click,
// which selects the block under the pointer (§3.3), or, in the details
// pane, selects nothing. Either way phase returns to idle and autoGen
// bumps, so any auto-scroll tick still pending for this drag becomes
// stale.
func (m mouseCtl) release(tea.MouseReleaseMsg) tea.Cmd {
	a := m.a
	phase := a.view.mouse.phase
	a.view.mouse.phase = dragIdle
	a.view.mouse.autoGen++
	switch phase {
	case dragMoving:
		return m.copySelection()
	case dragPressed:
		return m.click()
	}
	return nil
}

// copySelection extracts the dragged range's plain text in document order
// (or, in the details pane, from its single "line space") and, if it
// isn't empty, copies it and sets the hint.
func (m mouseCtl) copySelection() tea.Cmd {
	a := m.a
	sel := a.view.mouse.sel
	var text string
	if a.view.mouse.pane == regionDetails {
		top := columnTop(a)
		order := func(string) int { return 0 }
		text = selection.Text(sel, []string{detailsSelID}, order, func(string) []string {
			if top == nil || top.kind != paneDetails {
				return nil
			}
			return top.body.Lines()
		})
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
// the card when the selection lands on it. In the details pane a click
// selects nothing (there is no analogous block to select).
func (m mouseCtl) click() tea.Cmd {
	a := m.a
	if a.view.mouse.pane != regionTranscript {
		return nil
	}
	if id := a.view.mouse.sel.Start.ID; id != "" {
		a.sess.main.list.Select(id)
	}
	return nil
}

// transcriptIDs lists every transcript block's ID in document order.
func transcriptIDs(a *App) []string {
	blocks := a.sess.main.proj.Blocks()
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
	x := 1
	if p == regionDetails {
		x = 0
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
		lines[y] = paintRow(lines[y], id, line, sel, order, on, off, p, r.W)
	}
	return strings.Join(lines, "\n")
}

// paneOrder is p's selection order function: document order for the
// transcript, or a constant (there is only one ID) for the details pane.
func paneOrder(a *App, p mouseRegion) func(string) int {
	if p == regionDetails {
		return func(string) int { return 0 }
	}
	return indexOrder(transcriptIDs(a))
}

// paintRow highlights row's selected cells. For the transcript it skips
// the prefix column (x=0, the "▌" bar) and the scrollbar column (x=w-1);
// for the details pane the whole row is eligible.
func paintRow(row, id string, line int, sel selection.Range, order func(string) int, on, off string, p mouseRegion, w int) string {
	if p != regionTranscript {
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
