package picker

import "charm.land/bubbles/v2/key"

// KeyMap holds the picker's navigation bindings. enter, esc, tab, and
// backspace are fixed (R21) and handled directly in Update, not through a
// Binding.
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
