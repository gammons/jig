// Package confirm is jig's small modal dialog: a title, a block of body
// lines that scroll with j/k when they overflow, and a row of choices,
// each bound to a key that emits a ChosenMsg. It backs both the trust
// dialog (a short-lived tea program shown before the main TUI) and any
// in-TUI confirmation the App composes with internal/bubbles/overlay.
package confirm

import (
	tea "charm.land/bubbletea/v2"
)

// Choice is one key the dialog accepts, e.g. {Key: "t", Label: "Trust"}.
// A Choice with Key "esc" is what the fixed Escape key matches.
type Choice struct {
	Key, Label string
}

// ChosenMsg is emitted when a pressed key matches a Choice's Key.
type ChosenMsg struct {
	Key string
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// WithKeyMap sets the scroll key bindings.
func WithKeyMap(km KeyMap) Option { return func(m *Model) { m.keys = km } }

// Model is the confirm dialog. Use the Model most recently returned by
// Update; it is not safe for concurrent use.
type Model struct {
	styles  Styles
	keys    KeyMap
	termW   int
	termH   int
	title   string
	lines   []string
	choices []Choice
	yOffset int
}

// New builds an empty dialog.
func New(opts ...Option) Model {
	m := Model{styles: DefaultStyles(), keys: DefaultKeyMap()}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// Set replaces the dialog's content and resets any scroll position.
func (m *Model) Set(title string, lines []string, choices []Choice) {
	m.title = title
	m.lines = lines
	m.choices = choices
	m.yOffset = 0
}

// SetSize sets the outer terminal size the box is centered within.
func (m *Model) SetSize(termW, termH int) { m.termW, m.termH = termW, termH }

// keyMatches returns the Choice whose Key equals k's string form, or
// (Choice{}, false).
func keyMatches(k tea.KeyPressMsg, choices []Choice) (Choice, bool) {
	s := k.String()
	for _, c := range choices {
		if c.Key == s {
			return c, true
		}
	}
	return Choice{}, false
}
