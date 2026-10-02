package picker

import "charm.land/bubbles/v2/key"

// KeyMap holds the picker's navigation bindings. enter, esc, and tab are
// fixed (R21) and handled directly in Update, not through a Binding;
// backspace only edits the filter or input text.
type KeyMap struct {
	Up, Down key.Binding
}

// DefaultKeyMap returns ↑/ctrl+p and ↓/ctrl+n.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:   key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/ctrl+p", "up")),
		Down: key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓/ctrl+n", "down")),
	}
}
