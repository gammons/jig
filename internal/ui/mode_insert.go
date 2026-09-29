package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/selection"
)

// insertMode is the keymap mode name for INSERT.
const insertMode = "insert"

// insertKeys handles key presses in INSERT (spec §6.2).
type insertKeys struct{ a *App }

// handle runs the fixed INSERT keys (R21) first, so a config binding one
// of them never overrides it, then a remappable binding, and otherwise
// types into the prompt.
func (h insertKeys) handle(k tea.KeyPressMsg) tea.Cmd {
	a := h.a
	key := k.String()
	switch key {
	case "ctrl+c":
		return a.sender().ctrlC()
	case "esc":
		a.view.mouse.sel = selection.Range{}
		return a.setMode(modeNormal)
	case "tab":
		return a.cycleAgent(1)
	case "shift+tab":
		return a.cycleAgent(-1)
	case "enter", "shift+enter", "alt+enter", "@", "up", "down", "ctrl+d": // ctrl+d: App.onKey quits on an empty prompt; else forward-delete
	default:
		if id, ok := a.opts.Keymap.Lookup(insertMode, key); ok {
			return a.runAction(id)
		}
	}
	var cmd tea.Cmd
	a.w.prompt, cmd = a.w.prompt.Update(k)
	return cmd
}
