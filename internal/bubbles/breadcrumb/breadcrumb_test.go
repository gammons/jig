package breadcrumb

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

func TestView_Full(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSegments([]string{"main", "↳ explore: find the bug"})
	m.SetHint("esc back")
	m.SetFocused(true)
	m.SetWidth(80)

	got := xansi.Strip(m.View())
	if w := ansi.Width(m.View()); w != 80 {
		t.Fatalf("View() width = %d, want 80", w)
	}
	if !strings.HasPrefix(got, "main › ↳ explore: find the bug") {
		t.Fatalf("View() = %q, want prefix %q", got, "main › ↳ explore: find the bug")
	}
	if !strings.HasSuffix(got, "esc back") {
		t.Fatalf("View() = %q, want suffix %q", got, "esc back")
	}
}

func TestView_TruncationOrder(t *testing.T) {
	t.Parallel()

	segments := []string{
		"main",
		"↳ explore: a",
		"↳ general: b",
		"edit · foo.go · 2 hunks",
	}

	const fullRow = "main › ↳ explore: a › ↳ general: b › edit · foo.go · 2 hunks"

	tests := []struct {
		name   string
		width  int
		prefix string // when set, checked as a prefix (padding follows)
		suffix string // when set, checked as a suffix (the hint)
		want   string // when set, checked as the exact right-trimmed row
	}{
		{
			name:   "100 shows everything including the hint",
			width:  100,
			prefix: fullRow,
			suffix: "esc back",
		},
		{
			name:  "60 drops the hint",
			width: 60,
			want:  fullRow,
		},
		{
			name:  "50 collapses one middle segment",
			width: 50,
			want:  "main › … › ↳ general: b › edit · foo.go · 2 hunks",
		},
		{
			name:  "36 collapses all middle segments",
			width: 36,
			want:  "main › … › edit · foo.go · 2 hunks",
		},
		{
			name:  "20 truncates the last segment",
			width: 20,
			want:  "main › … › edit · f…",
		},
		{
			name:  "5 hard-cuts the whole row",
			width: 5,
			want:  "main ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New()
			m.SetSegments(segments)
			m.SetHint("esc back")
			m.SetWidth(tt.width)

			stripped := xansi.Strip(m.View())
			switch {
			case tt.prefix != "" || tt.suffix != "":
				if !strings.HasPrefix(stripped, tt.prefix) {
					t.Errorf("View() at width %d = %q, want prefix %q", tt.width, stripped, tt.prefix)
				}
				if !strings.HasSuffix(stripped, tt.suffix) {
					t.Errorf("View() at width %d = %q, want suffix %q", tt.width, stripped, tt.suffix)
				}
			case tt.width == 5:
				// The hard-cut case keeps its trailing space ("main ").
				if stripped != tt.want {
					t.Errorf("View() at width %d = %q, want %q", tt.width, stripped, tt.want)
				}
			default:
				got := strings.TrimRight(stripped, " ")
				if got != tt.want {
					t.Errorf("View() at width %d = %q, want %q", tt.width, got, tt.want)
				}
			}
			if w := ansi.Width(m.View()); w != tt.width {
				t.Fatalf("View() width = %d, want %d", w, tt.width)
			}
		})
	}
}

func TestView_SanitizesNothingButMeasuresWide(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSegments([]string{"main", "↳ 日本: find the bug"})
	m.SetWidth(20)

	if w := ansi.Width(m.View()); w != 20 {
		t.Fatalf("View() width = %d, want 20", w)
	}
}

func TestView_ZeroWidth(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSegments([]string{"main"})
	m.SetWidth(0)
	if got := m.View(); got != "" {
		t.Fatalf("View() at width 0 = %q, want \"\"", got)
	}
}

// TestView_SingleSegmentOverflow covers a single segment wider than the
// available width: View must hard-cut it, not panic (the "no middle
// segments to collapse" path must not slice an empty middle out of range).
func TestView_SingleSegmentOverflow(t *testing.T) {
	t.Parallel()
	for _, w := range []int{2, 3} {
		m := New()
		m.SetSegments([]string{"main"})
		m.SetWidth(w)
		got := m.View()
		if gotW := ansi.Width(got); gotW != w {
			t.Errorf("width %d: View() width = %d, want %d (got %q)", w, gotW, w, got)
		}
	}
}

// TestView_NilSegments covers no segments at all with a positive width:
// View must return w blank cells, not panic.
func TestView_NilSegments(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSegments(nil)
	m.SetWidth(10)
	got := m.View()
	if gotW := ansi.Width(got); gotW != 10 {
		t.Fatalf("View() width = %d, want 10 (got %q)", gotW, got)
	}
	if stripped := xansi.Strip(got); strings.TrimRight(stripped, " ") != "" {
		t.Fatalf("View() = %q, want all blank", got)
	}
}

func TestRuleView(t *testing.T) {
	t.Parallel()

	t.Run("unfocused uses Rule", func(t *testing.T) {
		t.Parallel()
		m := New(WithStyles(pinnedStyles()))
		m.SetWidth(10)
		m.SetFocused(false)
		got := m.RuleView()
		if w := ansi.Width(got); w != 10 {
			t.Fatalf("RuleView() width = %d, want 10", w)
		}
		if stripped := xansi.Strip(got); stripped != strings.Repeat("─", 10) {
			t.Fatalf("RuleView() stripped = %q, want 10 dashes", stripped)
		}
		want := pinnedStyles().Rule.Render(strings.Repeat("─", 10))
		if got != want {
			t.Fatalf("RuleView() = %q, want %q (Rule style)", got, want)
		}
	})

	t.Run("focused uses RuleFocused", func(t *testing.T) {
		t.Parallel()
		m := New(WithStyles(pinnedStyles()))
		m.SetWidth(10)
		m.SetFocused(true)
		got := m.RuleView()
		want := pinnedStyles().RuleFocused.Render(strings.Repeat("─", 10))
		if got != want {
			t.Fatalf("RuleView() = %q, want %q (RuleFocused style)", got, want)
		}
	})

	t.Run("zero width", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetWidth(0)
		if got := m.RuleView(); got != "" {
			t.Fatalf("RuleView() at width 0 = %q, want \"\"", got)
		}
	})
}

func TestGolden_BreadcrumbFocused80(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(DefaultStyles()))
	m.SetSegments([]string{"main", "↳ explore: find the bug"})
	m.SetHint("esc back")
	m.SetFocused(true)
	m.SetWidth(80)
	golden.Assert(t, "breadcrumb_focused_80", m.View())
}

func TestGolden_BreadcrumbUnfocused80(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(DefaultStyles()))
	m.SetSegments([]string{"main", "↳ explore: find the bug"})
	m.SetHint("esc back")
	m.SetFocused(false)
	m.SetWidth(80)
	golden.Assert(t, "breadcrumb_unfocused_80", m.View())
}

func TestGolden_BreadcrumbCollapsed36(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(DefaultStyles()))
	m.SetSegments([]string{
		"main",
		"↳ explore: a",
		"↳ general: b",
		"edit · foo.go · 2 hunks",
	})
	m.SetHint("esc back")
	m.SetFocused(true)
	m.SetWidth(36)
	golden.Assert(t, "breadcrumb_collapsed_36", m.View())
}
