// Package sidebar is jig's session sidebar: a stack of sections (Session,
// Todos, Files, Subagents, Browser, ...), each a title and a list of rows.
// A section with no rows is not drawn. It does no I/O; the App feeds it a
// fresh []Section on every change.
package sidebar

// Tone colors a Row's icon and text.
type Tone int

const (
	Normal Tone = iota
	Muted
	Accent
	Success
	Warning
	Error
)

// Gauge is a Used-of-Limit bar, e.g. a context window's token usage.
type Gauge struct {
	Used, Limit int64
}

// Row is one line of a Section. A Row with a non-nil Gauge renders as a
// filled bar with a percentage instead of Icon/Text. A Wrap row's text
// word-wraps onto as many lines as it needs instead of being cut with
// "…" (continuation lines hang under the text, past the icon).
type Row struct {
	Icon, Text string
	Tone       Tone
	Gauge      *Gauge
	Wrap       bool
}

// Section is a titled group of Rows. A Section with no Rows is not drawn.
type Section struct {
	Title string
	Rows  []Row
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// Model is the sidebar. Use the Model most recently returned by any
// mutator; it is not safe for concurrent use. The App sets the size and
// sections on every update and draws on every frame, so the setters
// render only when what they set differs, and View returns that render.
type Model struct {
	styles   Styles
	width    int
	height   int
	sections []Section
	out      string // the render of the fields above
	renders  int    // renders so far, for tests
}

// New builds an empty sidebar.
func New(opts ...Option) Model {
	m := Model{styles: DefaultStyles()}
	for _, o := range opts {
		o(&m)
	}
	m.refresh()
	return m
}

// SetSize sets the size the sidebar's View is fit to.
func (m *Model) SetSize(w, h int) {
	if w == m.width && h == m.height {
		return
	}
	m.width, m.height = w, h
	m.refresh()
}

// SetStyles replaces the Styles.
func (m *Model) SetStyles(st Styles) {
	m.styles = st
	m.refresh()
}

// SetSections replaces the shown sections. The sidebar keeps its own
// copy, so the caller may reuse s.
func (m *Model) SetSections(s []Section) {
	if sectionsEqual(s, m.sections) {
		return
	}
	m.sections = cloneSections(s)
	m.refresh()
}

// refresh re-renders out from the current state.
func (m *Model) refresh() {
	m.out = m.render()
	m.renders++
}

// sectionsEqual reports whether a and b would render the same: equal
// titles and rows, gauges compared by value.
func sectionsEqual(a, b []Section) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Title != b[i].Title || len(a[i].Rows) != len(b[i].Rows) {
			return false
		}
		for j, r := range a[i].Rows {
			q := b[i].Rows[j]
			if r.Icon != q.Icon || r.Text != q.Text || r.Tone != q.Tone || r.Wrap != q.Wrap || (r.Gauge == nil) != (q.Gauge == nil) {
				return false
			}
			if r.Gauge != nil && *r.Gauge != *q.Gauge {
				return false
			}
		}
	}
	return true
}

// cloneSections deep-copies s, gauges included.
func cloneSections(s []Section) []Section {
	out := make([]Section, len(s))
	for i, sec := range s {
		rows := make([]Row, len(sec.Rows))
		for j, r := range sec.Rows {
			if r.Gauge != nil {
				g := *r.Gauge
				r.Gauge = &g
			}
			rows[j] = r
		}
		out[i] = Section{Title: sec.Title, Rows: rows}
	}
	return out
}
