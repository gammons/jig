// Package statusbar is jig's one-line, lualine-style status bar: colored
// blocks joined by powerline arrows (Nerd Font glyphs). Left to right: the
// mode, the git branch, then agent/model, run state, and an optional hint
// on the filler; right-aligned, the indicators, context/cost, and a ctrl+p
// reminder in the mode's color. It does no I/O; the App feeds it a fresh
// State on every change.
package statusbar

import "time"

// State is everything the bar renders. Elapsed is a plain field (never
// time.Now) so the widget stays pure and testable; the App computes it
// from its own clock.
type State struct {
	Mode         string // "INSERT", "NORMAL", "PICKER"
	Branch       string // git branch; "" hides section b
	Agent, Model string
	Running      bool
	Elapsed      time.Duration
	Frame        int // spinner frame index
	CtxUsed      int64
	CtxLimit     int64
	CostUSD      float64
	Pending      int
	Queued       bool
	Untrusted    bool
	Hint         string // e.g. "⚠ permission pending · esc gp"
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// Model is the status bar. Use the Model most recently returned by any
// mutator; it is not safe for concurrent use.
type Model struct {
	styles Styles
	width  int
	state  State
}

// New builds an empty bar.
func New(opts ...Option) Model {
	m := Model{styles: DefaultStyles()}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// SetWidth sets the width the bar's single line is fit to.
func (m *Model) SetWidth(w int) { m.width = w }

// SetStyles replaces the Styles.
func (m *Model) SetStyles(st Styles) { m.styles = st }

// Set replaces the shown state.
func (m *Model) Set(s State) { m.state = s }
