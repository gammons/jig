package blocklist

import "charm.land/bubbles/v2/key"

// KeyMap holds the list's key bindings. Update matches only these.
type KeyMap struct {
	Down, Up, Bottom, HalfDown, HalfUp, NextMatch, PrevMatch key.Binding
}

// DefaultKeyMap returns the vim-style bindings: j k G ctrl+d ctrl+u n N.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Down:      key.NewBinding(key.WithKeys("j"), key.WithHelp("j", "next block")),
		Up:        key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "previous block")),
		Bottom:    key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last block")),
		HalfDown:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		HalfUp:    key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		NextMatch: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
		PrevMatch: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous match")),
	}
}
