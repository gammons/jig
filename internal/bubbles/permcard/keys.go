package permcard

import "charm.land/bubbles/v2/key"

// KeyMap holds the card's bindings: a/A/d choose a reply directly, D opens
// the one-line deny-with-message input, enter sends it, and esc closes it
// without replying. These keys are fixed by the spec (R21) and cannot be
// remapped in NORMAL mode, but the Bindings still exist so a caller can
// read or display them.
type KeyMap struct {
	Allow  key.Binding
	Always key.Binding
	Deny   key.Binding

	DenyMsg key.Binding
	Send    key.Binding
	Cancel  key.Binding
}

// DefaultKeyMap returns jig's default permission-card bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Allow:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "allow")),
		Always:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "always")),
		Deny:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "deny")),
		DenyMsg: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "deny with message")),
		Send:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}
