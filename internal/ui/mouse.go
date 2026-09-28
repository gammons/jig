package ui

import (
	tea "charm.land/bubbletea/v2"

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

// mouseState is the App's drag state (viewState's mouse field). Only sel
// is used by this task's wheel handling (it is cleared on every wheel
// event). Task 5 adds phase (idle/pressed/dragging), pane (which pane the
// drag started in), last (the last pointer cell), and autoGen (the
// generation of the auto-scroll tick); golangci-lint's unused check flags
// an unused field, so they aren't declared here yet.
type mouseState struct {
	sel selection.Range
}

// mouseCtl handles the App's mouse messages.
type mouseCtl struct{ a *App }

// handle routes one mouse message: only the wheel does anything in this
// task (a click, a motion, or a release is Task 5's).
func (m mouseCtl) handle(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.wheel(msg)
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
