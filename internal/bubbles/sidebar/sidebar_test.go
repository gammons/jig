package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fafff")),
		Normal:     lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Muted:      lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Accent:     lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Success:    lipgloss.NewStyle().Foreground(lipgloss.Color("#5fff5f")),
		Warning:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Error:      lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")),
		GaugeEmpty: lipgloss.NewStyle().Foreground(lipgloss.Color("#404040")),
	}
}

func TestSidebar_HidesEmptySections(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 10)
	m.SetSections([]Section{
		{Title: "Session", Rows: []Row{{Text: "my session"}}},
		{Title: "Empty", Rows: nil},
		{Title: "Todos", Rows: []Row{{Icon: "✓", Text: "done thing", Tone: Success}}},
	})
	got := xansi.Strip(m.View())
	if strings.Contains(got, "Empty") {
		t.Fatalf("View() = %q, want it to not draw the empty section's title", got)
	}
	if !strings.Contains(got, "Session") || !strings.Contains(got, "Todos") {
		t.Fatalf("View() = %q, want both non-empty section titles", got)
	}
}

func TestSidebar_Gauge(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 3)
	m.SetSections([]Section{
		{Title: "Session", Rows: []Row{{Gauge: &Gauge{Used: 50, Limit: 100}}}},
	})
	lines := strings.Split(xansi.Strip(m.View()), "\n")
	if len(lines) != 3 {
		t.Fatalf("View() produced %d lines, want 3", len(lines))
	}
	gaugeLine := lines[1]
	if !strings.HasSuffix(strings.TrimRight(gaugeLine, " "), "50%") {
		t.Fatalf("gauge line = %q, want it to end in 50%%", gaugeLine)
	}
	filled := strings.Count(gaugeLine, "█")
	empty := strings.Count(gaugeLine, "░")
	if filled == 0 || empty == 0 {
		t.Fatalf("gauge line = %q, want a mix of filled and empty cells", gaugeLine)
	}
	// 50% fill: filled and empty portions should be equal (± rounding).
	if diff := filled - empty; diff < -1 || diff > 1 {
		t.Fatalf("gauge line = %q, filled=%d empty=%d, want roughly equal for 50%%", gaugeLine, filled, empty)
	}
}

func TestSidebar_ClipsToHeight(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(20, 2)
	m.SetSections([]Section{
		{Title: "Session", Rows: []Row{
			{Text: "one"}, {Text: "two"}, {Text: "three"},
		}},
	})
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 2 {
		t.Fatalf("View() produced %d lines, want clipped to 2", len(lines))
	}
}

// TestSidebar_Margins: every line leaves padX blank columns on each side,
// so headers don't sit against the transcript's scrollbar and gauges and
// long rows don't touch the terminal edge. Rows stay indented under
// their header, and every line is still exactly the sidebar's width.
func TestSidebar_Margins(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 6)
	m.SetSections([]Section{{Title: "Session", Rows: []Row{
		{Text: strings.Repeat("long ", 20)},
		{Gauge: &Gauge{Used: 100, Limit: 100}},
	}}})
	lines := strings.Split(xansi.Strip(m.View()), "\n")
	pad := strings.Repeat(" ", padX)
	for i, l := range lines {
		if w := xansi.StringWidth(l); w != 30 {
			t.Errorf("line %d is %d cells, want 30: %q", i, w, l)
		}
		if !strings.HasPrefix(l, pad) || !strings.HasSuffix(l, pad) {
			t.Errorf("line %d = %q, want %d blank columns on each side", i, l, padX)
		}
	}
	if !strings.HasPrefix(lines[0], pad+"Session") {
		t.Errorf("header = %q, want it right after the left margin", lines[0])
	}
	if !strings.HasPrefix(lines[1], pad+"  long") {
		t.Errorf("row = %q, want it indented two more columns under its header", lines[1])
	}
	if !strings.HasSuffix(strings.TrimRight(lines[2], " "), "100%") {
		t.Errorf("gauge = %q, want it to end in 100%% before the right margin", lines[2])
	}
}

// TestSidebar_RendersOnlyOnChange: the App calls SetSize and SetSections
// on every Update, and View runs on every frame, so View reuses its last
// render until the sections (by value), the size, or the styles change.
func TestSidebar_RendersOnlyOnChange(t *testing.T) {
	t.Parallel()
	secs := func(cost string, used int64) []Section {
		return []Section{
			{Title: "Session", Rows: []Row{{Text: "s"}, {Gauge: &Gauge{Used: used, Limit: 100}}, {Text: cost, Tone: Muted}}},
			{Title: "Files", Rows: []Row{{Icon: "M", Text: "a.go", Tone: Warning}}},
		}
	}
	m := New(WithStyles(pinnedStyles()))
	m.SetSize(30, 10)
	m.SetSections(secs("$0.10", 10))
	_ = m.View()
	steps := []struct {
		name   string
		do     func()
		render bool
	}{
		{"equal sections, fresh slices", func() { m.SetSections(secs("$0.10", 10)) }, false},
		{"same size", func() { m.SetSize(30, 10) }, false},
		{"a row's text", func() { m.SetSections(secs("$0.20", 10)) }, true},
		{"a gauge's value", func() { m.SetSections(secs("$0.20", 20)) }, true},
		{"the width", func() { m.SetSize(31, 10) }, true},
		{"the height", func() { m.SetSize(31, 11) }, true},
		{"the styles", func() { m.SetStyles(DefaultStyles()) }, true},
	}
	for _, s := range steps {
		before := m.renders
		s.do()
		got := m.View()
		if rendered := m.renders != before; rendered != s.render {
			t.Errorf("%s: rendered = %v, want %v", s.name, rendered, s.render)
		}
		fresh := New(WithStyles(m.styles))
		fresh.SetSize(m.width, m.height)
		fresh.SetSections(m.sections)
		if want := fresh.View(); got != want {
			t.Errorf("%s: View() = %q, want a fresh render's %q", s.name, got, want)
		}
	}
}

// TestSidebar_SectionsAreCopied: mutating a slice after SetSections never
// changes what the sidebar shows (it would otherwise go stale unseen).
func TestSidebar_SectionsAreCopied(t *testing.T) {
	t.Parallel()
	g := &Gauge{Used: 10, Limit: 100}
	secs := []Section{{Title: "Session", Rows: []Row{{Text: "before"}, {Gauge: g}}}}
	m := New()
	m.SetSize(30, 4)
	m.SetSections(secs)
	want := m.View()
	secs[0].Rows[0].Text = "after"
	g.Used = 90
	m.SetSections([]Section{{Title: "Session", Rows: []Row{{Text: "before"}, {Gauge: &Gauge{Used: 10, Limit: 100}}}}})
	if got := m.View(); got != want {
		t.Errorf("View() changed after the caller mutated its slice:\n got %q\nwant %q", got, want)
	}
}

func TestSidebar_EmptyWithoutSize(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSections([]Section{{Title: "Session", Rows: []Row{{Text: "x"}}}})
	if got := m.View(); got != "" {
		t.Fatalf("View() without SetSize = %q, want \"\"", got)
	}
}

func TestGolden_SidebarFull(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetSize(32, 16)
	m.SetSections([]Section{
		{Title: "Session", Rows: []Row{
			{Text: "fix the parser"},
			{Gauge: &Gauge{Used: 12000, Limit: 200000}},
			{Text: "$0.42", Tone: Muted},
			{Text: "coder · sonnet", Tone: Muted},
		}},
		{Title: "Todos", Rows: []Row{
			{Icon: "✓", Text: "read the spec", Tone: Success},
			{Icon: "●", Text: "write the code", Tone: Accent},
			{Icon: "○", Text: "ship it", Tone: Muted},
		}},
		{Title: "Files", Rows: []Row{
			{Icon: "M", Text: "main.go", Tone: Warning},
			{Icon: "A", Text: "new.go", Tone: Success},
		}},
		{Title: "Subagents", Rows: nil},
	})
	golden.Assert(t, "sidebar_full", m.View())
}
