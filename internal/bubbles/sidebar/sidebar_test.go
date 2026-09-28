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
