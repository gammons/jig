package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
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
	switch key {
	case "ctrl+c":
		return a.sender().ctrlC()
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
		return a.cycleAgent(1)
	case "shift+tab":
		return a.cycleAgent(-1)
	case "enter":
		return h.toggleDetails()
	case "ctrl+e":
		a.w.details.ScrollBy(1)
		return nil
	case "ctrl+y":
		a.w.details.ScrollBy(-1)
		return nil
	case "q", "esc":
		return h.closeOrClear()
	case "j", "k", "G", "ctrl+d", "ctrl+u", "n", "N":
		return h.navigate(k)
	default:
		if id, ok := a.opts.Keymap.Lookup(normalMode, key); ok {
			return a.runAction(id)
		}
	}
	return nil
}

// navigate forwards k to the transcript list, then, if the details split
// is open and the selection moved, rebuilds it for the new block.
func (h normalKeys) navigate(k tea.KeyPressMsg) tea.Cmd {
	a := h.a
	before, _ := a.w.list.Selected()
	var cmd tea.Cmd
	a.w.list, cmd = a.w.list.Update(k)
	return tea.Batch(cmd, h.syncDetails(before))
}

// gotoTop selects the first block ("gg"), rebuilding the details for it
// when the split is open.
func (h normalKeys) gotoTop() tea.Cmd {
	a := h.a
	before, _ := a.w.list.Selected()
	a.w.list.Top()
	return h.syncDetails(before)
}

// syncDetails rebuilds the details pane for the selection's new block, if
// the split is open and the selection actually moved from before.
func (h normalKeys) syncDetails(before blocklist.Item) tea.Cmd {
	a := h.a
	if !a.view.detailsOpen {
		return nil
	}
	after, ok := a.w.list.Selected()
	if !ok || after.ID == before.ID {
		return nil
	}
	return h.openDetailsFor(transcript.BlockID(after.ID))
}

// toggleDetails opens the details split for the selected block, building
// its content, or closes an already-open split.
func (h normalKeys) toggleDetails() tea.Cmd {
	a := h.a
	if a.view.detailsOpen {
		a.view.detailsOpen = false
		return nil
	}
	item, ok := a.w.list.Selected()
	if !ok {
		return nil
	}
	a.view.detailsOpen = true
	return h.openDetailsFor(transcript.BlockID(item.ID))
}

// openDetailsFor builds and shows id's details. It sizes the build for the
// details split at the App's current width/height directly, rather than
// a.lay (not yet recomputed for detailsOpen when opening for the first
// time in this same key press), so the content is never built at a stale
// width.
func (h normalKeys) openDetailsFor(id transcript.BlockID) tea.Cmd {
	a := h.a
	b, ok := a.sess.proj.Block(id)
	if !ok {
		return nil
	}
	a.view.detailsFor = id
	a.img.shown = nil
	lay := computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, true)
	content, cmd := buildDetails(a.ctx, b, lay.Side.W, lay.Side.H, a.w.render, a.ports, a.img)
	a.w.details.SetContent(content)
	return cmd
}

// closeOrClear closes the details split, or, when it is already closed,
// clears any applied search (spec §6.3: "q/esc close the split, else
// clear the search").
func (h normalKeys) closeOrClear() tea.Cmd {
	a := h.a
	if a.view.detailsOpen {
		a.view.detailsOpen = false
		return nil
	}
	a.w.list.SetSearch("")
	return nil
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
		a.w.list.SetSearch(a.w.search.Value())
		a.w.search.Blur()
		return nil
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

// yank copies the selected block's text to the clipboard (spec §6.3, via
// yankText) and shows a "yanked" hint.
func (h normalKeys) yank() tea.Cmd {
	a := h.a
	item, ok := a.w.list.Selected()
	if !ok {
		return nil
	}
	b, ok := a.sess.proj.Block(transcript.BlockID(item.ID))
	if !ok {
		return nil
	}
	a.view.hint = "yanked"
	return tea.SetClipboard(yankText(b))
}
