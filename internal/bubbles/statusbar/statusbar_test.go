package statusbar

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Mode:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fff5f")),
		Text:    lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Running: lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Idle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Warn:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Hint:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
	}
}

func runningState() State {
	return State{
		Mode: "NORMAL", Agent: "coder", Model: "sonnet",
		Running: true, Elapsed: 12 * time.Second, Frame: 0,
		CtxUsed: 12000, CtxLimit: 200000, CostUSD: 0.42,
		Pending: 2, Queued: true, Untrusted: true,
		Hint: "⚠ permission pending · esc gp",
	}
}

func TestStatus_SegmentsAndTruncation(t *testing.T) {
	t.Parallel()

	t.Run("120 cols shows every segment", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetWidth(120)
		m.Set(runningState())
		got := xansi.Strip(m.View())

		if w := ansi.Width(m.View()); w != 120 {
			t.Fatalf("View() width = %d, want 120", w)
		}
		for _, want := range []string{
			"[NORMAL]", "coder · sonnet", "⠋ running 12s",
			"ctx 12k/200k · $0.42", "⚠ 2", "⏳", "untrusted",
			"⚠ permission pending · esc gp", "ctrl+p",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("View() at 120 cols = %q, want it to contain %q", got, want)
			}
		}
	})

	t.Run("60 cols drops middle segments first", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetWidth(60)
		m.Set(runningState())
		v := m.View()
		got := xansi.Strip(v)

		if w := ansi.Width(v); w != 60 {
			t.Fatalf("View() width = %d, want 60", w)
		}
		// The mode badge, agent/model, and run state survive.
		for _, want := range []string{"[NORMAL]", "coder · sonnet", "⠋ running 12s", "ctrl+p"} {
			if !strings.Contains(got, want) {
				t.Errorf("View() at 60 cols = %q, want it to contain %q", got, want)
			}
		}
		// ctx/cost, the indicators, and the hint are dropped before those.
		for _, notWant := range []string{"ctx 12k/200k", "⚠ 2", "permission pending"} {
			if strings.Contains(got, notWant) {
				t.Errorf("View() at 60 cols = %q, want it to NOT contain %q", got, notWant)
			}
		}
	})

	t.Run("ctrl+p hint is dim and right-aligned", func(t *testing.T) {
		t.Parallel()
		m := New(WithStyles(pinnedStyles()))
		m.SetWidth(80)
		m.Set(runningState())
		v := m.View()
		if !strings.HasSuffix(v, pinnedStyles().Dim.Render("ctrl+p")) {
			t.Fatalf("View() does not end with the dim ctrl+p hint: %q", v)
		}
	})
}

func TestGolden_StatusIdle(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetWidth(80)
	m.Set(State{
		Mode: "NORMAL", Agent: "coder", Model: "sonnet",
		Running: false, CtxUsed: 12000, CtxLimit: 200000, CostUSD: 0.42,
	})
	golden.Assert(t, "status_idle", m.View())
}

func TestGolden_StatusRunning(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetWidth(80)
	m.Set(runningState())
	golden.Assert(t, "status_running", m.View())
}
