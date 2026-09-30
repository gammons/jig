// Package breadcrumb is jig's path widget: it renders a single row of
// segments joined by " › ", a right-aligned hint, and a matching rule row,
// truncating in slk's order when the row doesn't fit. It does no I/O; the
// App feeds it sanitized segments and a hint on every change.
package breadcrumb

import "charm.land/lipgloss/v2"

// Styles holds the breadcrumb's look.
type Styles struct {
	Muted       lipgloss.Style // earlier segments and the " › " separators
	Current     lipgloss.Style // the last segment, focused (bold accent)
	CurrentDim  lipgloss.Style // the last segment, unfocused
	Hint        lipgloss.Style // the right-aligned hint
	Rule        lipgloss.Style // the rule row, unfocused
	RuleFocused lipgloss.Style // the rule row, focused
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Muted:       lipgloss.NewStyle().Faint(true),
		Current:     lipgloss.NewStyle().Bold(true),
		CurrentDim:  lipgloss.NewStyle(),
		Hint:        lipgloss.NewStyle().Faint(true),
		Rule:        lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")),
		RuleFocused: lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
	}
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// Model is the breadcrumb. Use the Model most recently returned by any
// pointer method; it is not safe for concurrent use.
type Model struct {
	segments []string
	hint     string
	focused  bool
	w        int
	styles   Styles
}

// New builds an empty breadcrumb.
func New(opts ...Option) Model {
	m := Model{styles: DefaultStyles()}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// SetSegments replaces the shown path segments, in order from the root.
func (m *Model) SetSegments(s []string) { m.segments = s }

// SetHint replaces the right-aligned hint text.
func (m *Model) SetHint(h string) { m.hint = h }

// SetFocused sets whether the pane this breadcrumb belongs to has focus:
// it selects Current vs CurrentDim for the last segment, and Rule vs
// RuleFocused for RuleView.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetWidth sets the width both View and RuleView are fit to.
func (m *Model) SetWidth(w int) { m.w = w }

// SetStyles replaces the Styles.
func (m *Model) SetStyles(st Styles) { m.styles = st }

// RuleView renders one row of "─" cells, w wide, in Rule or RuleFocused.
func (m Model) RuleView() string {
	if m.w <= 0 {
		return ""
	}
	st := m.styles.Rule
	if m.focused {
		st = m.styles.RuleFocused
	}
	return st.Render(repeatRule(m.w))
}
