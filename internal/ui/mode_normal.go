package ui

import (
	tea "charm.land/bubbletea/v2"
)

// normalMode is the keymap mode name for NORMAL.
const normalMode = "normal"

// normalKeys handles key presses in NORMAL (spec §6.3).
type normalKeys struct{ a *App }

// handle runs the fixed NORMAL keys (R21) first, then a remappable
// binding, and otherwise moves through the transcript.
func (h normalKeys) handle(k tea.KeyPressMsg) tea.Cmd {
	a := h.a
	key := k.String()
	switch key {
	case "ctrl+c":
		return a.sender().ctrlC()
	case "i", "a":
		return a.setMode(modeInsert)
	case "tab":
		return a.cycleAgent(1)
	case "shift+tab":
		return a.cycleAgent(-1)
	case "esc", "q":
		return nil
	case "j", "k", "G", "ctrl+d", "ctrl+u", "n", "N":
	default:
		if id, ok := a.opts.Keymap.Lookup(normalMode, key); ok {
			return a.runAction(id)
		}
	}
	var cmd tea.Cmd
	a.w.list, cmd = a.w.list.Update(k)
	return cmd
}
