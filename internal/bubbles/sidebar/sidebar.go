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
// filled bar with a percentage instead of Icon/Text.
type Row struct {
	Icon, Text string
	Tone       Tone
	Gauge      *Gauge
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
// mutator; it is not safe for concurrent use.
type Model struct {
	styles   Styles
	width    int
	height   int
	sections []Section
}

// New builds an empty sidebar.
func New(opts ...Option) Model {
	m := Model{styles: DefaultStyles()}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// SetSize sets the size the sidebar's View is fit to.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetStyles replaces the Styles.
func (m *Model) SetStyles(st Styles) { m.styles = st }

// SetSections replaces the shown sections.
func (m *Model) SetSections(s []Section) { m.sections = s }
