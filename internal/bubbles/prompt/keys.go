package prompt

import "charm.land/bubbles/v2/key"

// KeyMap holds the prompt's bindings. enter, shift+enter/alt+enter, '@',
// and backspace are fixed (R21) and handled directly in Update; these
// Bindings exist so callers can display help for them, and so a caller
// wanting a different physical key for one (e.g. a themed help screen)
// has somewhere to read the current keys from. Overriding them via
// WithKeyMap does not change Update's fixed handling of enter/shift+enter/
// alt+enter/'@'/backspace themselves.
type KeyMap struct {
	Submit      key.Binding
	Newline     key.Binding
	Editor      key.Binding
	HistoryPrev key.Binding
	HistoryNext key.Binding
}

// DefaultKeyMap returns jig's default prompt bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Submit:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Newline:     key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline")),
		Editor:      key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "editor")),
		HistoryPrev: key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "history prev")),
		HistoryNext: key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "history next")),
	}
}
