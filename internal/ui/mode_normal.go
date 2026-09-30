package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/selection"
	"github.com/gammons/jig/internal/ui/transcript"
)

// normalMode is the keymap mode name for NORMAL.
const normalMode = "normal"

// normalKeys handles key presses in NORMAL (spec §6.3).
type normalKeys struct{ a *App }

// handle runs the fixed NORMAL keys (R21) first, then a remappable
// binding, and otherwise moves through the transcript. While the
// one-line search input is open, every key but enter/esc types into it
// instead (handleSearch).
func (h normalKeys) handle(k tea.KeyPressMsg) tea.Cmd {
	a := h.a
	if a.view.searching {
		return h.handleSearch(k)
	}
	if a.view.keyPrefix == "g" {
		a.view.keyPrefix = ""
		switch k.String() {
		case "g":
			return h.gotoTop()
		case "p":
			return permCtl{a}.next()
		}
		return nil // any unknown g-command: swallowed.
	}

	key := k.String()
	if a.w.card.Typing() && key != "ctrl+c" {
		return permCtl{a}.key(k) // the deny message, enter, esc
	}
	if key == "esc" && a.view.mouse.sel.Active {
		a.view.mouse.sel = selection.Range{}
		return nil
	}
	switch key {
	case "ctrl+c": // spec §6.3: cancel the run only; never clear or quit
		return a.sender().normalCtrlC()
	case "g":
		a.view.keyPrefix = "g"
		return nil
	case "a", "A", "d", "D":
		if (permCtl{a}).onCard() {
			return permCtl{a}.key(k)
		}
		if key == "a" {
			return a.setMode(modeInsert)
		}
		return nil
	case "i":
		return a.setMode(modeInsert)
	case "tab":
		if columnOpen(a) {
			return h.toggleFocus()
		}
		return a.cycleAgent(1)
	case "shift+tab":
		if columnOpen(a) {
			return h.toggleFocus()
		}
		return a.cycleAgent(-1)
	case "h":
		if columnOpen(a) {
			setFocus(a, focusMain)
		}
		return nil
	case "l":
		if columnOpen(a) {
			setFocus(a, focusColumn)
		}
		return nil
	case "enter":
		return h.enter()
	case "ctrl+e":
		h.scrollTop(1)
		return nil
	case "ctrl+y":
		h.scrollTop(-1)
		return nil
	case "q", "esc":
		return h.closeOrClear()
	case "j", "k", "G", "ctrl+u", "n", "N": // ctrl+d quits (App.onKey)
		return h.navigate(k)
	default:
		if id, ok := a.opts.Keymap.Lookup(normalMode, key); ok {
			return a.runAction(id)
		}
	}
	return nil
}

// toggleFocus swaps NORMAL's focus between main and the column, only
// while the column is open.
func (h normalKeys) toggleFocus() tea.Cmd {
	a := h.a
	if a.view.focus == focusMain {
		setFocus(a, focusColumn)
	} else {
		setFocus(a, focusMain)
	}
	return nil
}

// scrollTop scrolls the column's top entry's body by n lines, when it is
// a details pane (spec §5.2: ctrl+e/ctrl+y act on it whatever has focus).
func (h normalKeys) scrollTop(n int) {
	if top := columnTop(h.a); top != nil && top.kind == paneDetails {
		top.body.ScrollBy(n)
	}
}

// navigate forwards k to the focused pane's list. On main, when the
// column holds a single details entry, a moved selection rebuilds it
// (the split "follows" the cursor, spec §5.2). On a focused details
// pane, j/k scroll its body and everything else (G, ctrl+u, n, N) does
// nothing (spec §5.2).
func (h normalKeys) navigate(k tea.KeyPressMsg) tea.Cmd {
	a := h.a
	p := columnFocused(a)
	if p.kind == paneDetails {
		switch k.String() {
		case "j":
			p.body.ScrollBy(1)
		case "k":
			p.body.ScrollBy(-1)
		}
		return nil
	}
	before, _ := p.list.Selected()
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(k)
	if p != a.sess.main {
		return cmd
	}
	return tea.Batch(cmd, h.syncDetails(before))
}

// gotoTop selects the first block ("gg") in the focused pane, rebuilding
// a following details entry.
func (h normalKeys) gotoTop() tea.Cmd {
	a := h.a
	p := columnFocused(a)
	if p.kind == paneDetails {
		return nil
	}
	before, _ := p.list.Selected()
	p.list.Top()
	if p != a.sess.main {
		return nil
	}
	return h.syncDetails(before)
}

// syncDetails rebuilds the column's sole details entry for main's new
// selection, if the column holds exactly one and it is a details pane
// following main, and the selection actually moved from before (spec
// §5.2: "moving main's cursor rebuilds that entry for the new block").
func (h normalKeys) syncDetails(before blocklist.Item) tea.Cmd {
	a := h.a
	if len(a.view.col) != 1 || a.view.col[0].kind != paneDetails {
		return nil
	}
	after, ok := a.sess.main.list.Selected()
	if !ok || after.ID == before.ID {
		return nil
	}
	return h.openDetailsFor(transcript.BlockID(after.ID))
}

// enter opens or closes the column, per spec §5.2's table:
//   - main focused: same block as the column's base entry closes it,
//     otherwise the whole stack is replaced with a new entry for it.
//   - column focused on a transcript pane: pushes an entry for the
//     selected block (subagent drill-in; a later task fills entryFor's
//     kid case, so today this always pushes a details pane).
//   - column focused on a details pane: nothing.
func (h normalKeys) enter() tea.Cmd {
	a := h.a
	if a.view.focus == focusColumn {
		top := columnTop(a)
		if top == nil || top.kind != paneTranscript {
			return nil
		}
		item, ok := top.list.Selected()
		if !ok {
			return nil
		}
		p, cmd := columnEntryFor(a, top, transcript.BlockID(item.ID))
		if p == nil {
			return nil
		}
		return tea.Batch(cmd, columnPush(a, p))
	}
	item, ok := a.sess.main.list.Selected()
	if !ok {
		return nil
	}
	id := transcript.BlockID(item.ID)
	if columnTop(a) != nil && a.view.col[0].forBlock == id {
		columnClose(a)
		return nil
	}
	p, cmd := columnEntryFor(a, a.sess.main, id)
	if p == nil {
		return nil
	}
	return tea.Batch(cmd, columnReplace(a, p))
}

// openDetailsFor rebuilds the column's sole entry for id (main's new
// selection), keeping it as the column's only entry.
func (h normalKeys) openDetailsFor(id transcript.BlockID) tea.Cmd {
	a := h.a
	p, cmd := columnEntryFor(a, a.sess.main, id)
	if p == nil {
		return nil
	}
	if a.view.mouse.pane == regionDetails {
		a.view.mouse.sel = selection.Range{}
	}
	return tea.Batch(cmd, columnReplace(a, p))
}

// closeOrClear pops the column when it is open (closing it entirely from
// main, or one entry from the column — spec §5.2), or, when it is
// already closed, clears any applied search.
func (h normalKeys) closeOrClear() tea.Cmd {
	a := h.a
	switch {
	case a.view.focus == focusColumn:
		columnPop(a)
		return nil
	case columnOpen(a):
		columnClose(a)
		return nil
	}
	a.sess.main.list.SetSearch("")
	return foldCtl{a}.setSearch(false)
}

// openSearch focuses the one-line search input, shown in place of the
// status bar, ready to type a new query.
func (h normalKeys) openSearch() tea.Cmd {
	a := h.a
	a.view.searching = true
	a.w.search.Reset()
	return a.w.search.Focus()
}

// handleSearch routes a key while the search input is open: enter applies
// the query to the transcript list, esc cancels it, everything else types
// into it.
func (h normalKeys) handleSearch(k tea.KeyPressMsg) tea.Cmd {
	a := h.a
	switch k.String() {
	case "enter":
		a.view.searching = false
		query := a.w.search.Value()
		cmd := foldCtl{a}.setSearch(query != "")
		a.sess.main.list.SetSearch(query)
		a.w.search.Blur()
		return cmd
	case "esc":
		a.view.searching = false
		a.w.search.Reset()
		a.w.search.Blur()
		return nil
	}
	var cmd tea.Cmd
	a.w.search, cmd = a.w.search.Update(k)
	return cmd
}

// yank copies the selected block's text (a group header's member subjects,
// groupYank) to the clipboard (spec §6.3, via yankText) and shows a
// "yanked" hint.
func (h normalKeys) yank() tea.Cmd {
	a := h.a
	item, ok := a.sess.main.list.Selected()
	if !ok {
		return nil
	}
	if members, _, ok := (foldCtl{a}).group(transcript.BlockID(item.ID)); ok {
		a.view.hint = "yanked"
		return tea.SetClipboard(groupYank(members))
	}
	b, ok := a.sess.main.proj.Block(transcript.BlockID(item.ID))
	if !ok {
		return nil
	}
	a.view.hint = "yanked"
	return tea.SetClipboard(yankText(b))
}
