package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// onKey runs the keys fixed in every mode — ctrl+z suspends, ctrl+d
// quits (sender.ctrlD; in INSERT only on an empty prompt, else it
// deletes forward) — then routes the key to the current mode's handler.
// Keep in sync with actions.isFixed.
func (a *App) onKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+z":
		return tea.Suspend
	case "ctrl+d":
		if a.mode != modeInsert || a.w.prompt.Value() == "" {
			return a.sender().ctrlD()
		}
	}
	switch a.mode {
	case modeNormal:
		return normalKeys{a}.handle(k)
	case modePicker:
		var cmd tea.Cmd
		a.w.picker, cmd = a.w.picker.Update(k)
		return cmd
	}
	return insertKeys{a}.handle(k)
}

// windowTitle is the terminal title: the session's title (sanitized; it
// comes from the store or a model), or "jig" when there is none yet.
func (a *App) windowTitle() string {
	if t := strings.TrimSpace(ansi.SanitizeLine(a.sess.info.Title)); t != "" {
		return t
	}
	return "jig"
}
