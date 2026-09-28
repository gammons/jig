package confirm

import "charm.land/bubbles/v2/key"

// KeyMap holds the dialog's scroll bindings. A key equal to a Choice's Key
// (including "esc") is handled directly in Update, not through a Binding.
type KeyMap struct {
	Up, Down key.Binding
}

// DefaultKeyMap returns j/k (and ↑/↓) for scrolling the body lines.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:   key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "scroll up")),
		Down: key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "scroll down")),
	}
}
