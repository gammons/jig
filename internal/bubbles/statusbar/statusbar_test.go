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

// Pinned colors for goldens and exact-output assertions.
const (
	pinNormal = "#5fafff"
	pinInsert = "#5fff5f"
	pinPicker = "#ffaf00"
	pinModeFg = "#101010"
	pinBBg    = "#404040"
	pinCBg    = "#202020"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	mode := func(bg string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(pinModeFg)).Background(lipgloss.Color(bg))
	}
	return Styles{
		ModeNormal: mode(pinNormal),
		ModeInsert: mode(pinInsert),
		ModePicker: mode(pinPicker),
		B:          lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")).Background(lipgloss.Color(pinBBg)),
		C:          lipgloss.NewStyle().Foreground(lipgloss.Color("#c0c0c0")).Background(lipgloss.Color(pinCBg)),
		Running:    lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Idle:       lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Hint:       lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
	}
}

func runningState() State {
	return State{
		Mode: "NORMAL", Branch: "main", Agent: "coder", Model: "sonnet",
		Running: true, Elapsed: 12 * time.Second, Frame: 0,
		CtxUsed: 12000, CtxLimit: 200000, CostUSD: 0.42,
		Pending: 2, Queued: true, Untrusted: true,
		Hint: "⚠ permission pending · esc gp",
	}
}

// sep renders a powerline separator glyph from one block color to another.
func sep(glyph, fg, bg string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg)).Render(glyph)
}

func TestStatus_SegmentsAndTruncation(t *testing.T) {
	t.Parallel()

	t.Run("140 cols shows every segment", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetWidth(140)
		m.Set(runningState())
		got := xansi.Strip(m.View())

		if w := ansi.Width(m.View()); w != 140 {
			t.Fatalf("View() width = %d, want 140", w)
		}
		for _, want := range []string{
			" NORMAL ", branchIcon + " main", "coder · sonnet", "⠋ running 12s",
			"ctx 12k/200k · $0.42", "⚠ 2", "⏳", "untrusted",
			"⚠ permission pending · esc gp", "ctrl+p",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("View() at 140 cols = %q, want it to contain %q", got, want)
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
		for _, want := range []string{"NORMAL", "main", "coder · sonnet", "⠋ running 12s", "ctrl+p"} {
			if !strings.Contains(got, want) {
				t.Errorf("View() at 60 cols = %q, want it to contain %q", got, want)
			}
		}
		for _, notWant := range []string{"ctx 12k/200k", "⚠ 2", "permission pending"} {
			if strings.Contains(got, notWant) {
				t.Errorf("View() at 60 cols = %q, want it to NOT contain %q", got, notWant)
			}
		}
	})

	t.Run("segments drop in priority order as the bar narrows", func(t *testing.T) {
		t.Parallel()
		// Most droppable first; a segment may only be shown if every
		// segment after it in this list is shown too.
		order := []string{"ctx 12k/200k", "untrusted", "permission pending", "main", "running 12s", "coder · sonnet"}
		for w := 140; w >= 1; w-- {
			m := New()
			m.SetWidth(w)
			m.Set(runningState())
			v := m.View()
			if got := ansi.Width(v); got != w {
				t.Fatalf("width %d: View() width = %d", w, got)
			}
			got := xansi.Strip(v)
			for i, s := range order {
				if !strings.Contains(got, s) {
					continue
				}
				for _, later := range order[i+1:] {
					if !strings.Contains(got, later) {
						t.Errorf("width %d: %q shown but %q dropped: %q", w, s, later, got)
					}
				}
			}
		}
	})

	t.Run("ctrl+p stays in the right-hand mode block", func(t *testing.T) {
		t.Parallel()
		m := New(WithStyles(pinnedStyles()))
		m.SetWidth(80)
		m.Set(runningState())
		v := m.View()
		if !strings.HasSuffix(v, pinnedStyles().ModeNormal.Render(" ctrl+p ")) {
			t.Fatalf("View() does not end with the ctrl+p mode block: %q", v)
		}
	})
}

func TestStatus_ModeColors(t *testing.T) {
	t.Parallel()
	st := pinnedStyles()
	for _, tc := range []struct {
		mode  string
		style lipgloss.Style
		bg    string
	}{
		{"NORMAL", st.ModeNormal, pinNormal},
		{"INSERT", st.ModeInsert, pinInsert},
		{"PICKER", st.ModePicker, pinPicker},
	} {
		m := New(WithStyles(st))
		m.SetWidth(140)
		s := runningState()
		s.Mode = tc.mode
		m.Set(s)
		v := m.View()
		if !strings.HasPrefix(v, tc.style.Render(" "+tc.mode+" ")) {
			t.Errorf("%s: View() does not start with its mode block: %q", tc.mode, v)
		}
		if !strings.Contains(v, sep(sepRight, tc.bg, pinBBg)) {
			t.Errorf("%s: no  from the mode color into section b: %q", tc.mode, v)
		}
		if !strings.Contains(v, sep(sepLeft, tc.bg, pinBBg)) {
			t.Errorf("%s: no  from section y into the mode color: %q", tc.mode, v)
		}
	}
}

func TestStatus_NoBranchJoinsModeToC(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetWidth(100)
	s := runningState()
	s.Branch = ""
	m.Set(s)
	v := m.View()
	if strings.Contains(xansi.Strip(v), branchIcon) {
		t.Errorf("View() shows the branch icon with no branch: %q", v)
	}
	if !strings.Contains(v, sep(sepRight, pinNormal, pinCBg)) {
		t.Errorf("View() lacks a  from the mode color straight into section c: %q", v)
	}
}

func TestGolden_StatusIdle(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetWidth(80)
	m.Set(State{
		Mode: "NORMAL", Branch: "main", Agent: "coder", Model: "sonnet",
		Running: false, CtxUsed: 12000, CtxLimit: 200000, CostUSD: 0.42,
	})
	golden.Assert(t, "status_idle", m.View())
}

func TestGolden_StatusRunning(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetWidth(140)
	s := runningState()
	s.Mode = "INSERT"
	m.Set(s)
	golden.Assert(t, "status_running", m.View())
}

func TestStatus_EffortFollowsModel(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetWidth(140)
	st := runningState()
	st.Effort = "high"
	m.Set(st)
	if got := xansi.Strip(m.View()); !strings.Contains(got, "coder · sonnet · high") {
		t.Errorf("view = %q, want coder · sonnet · high", got)
	}
	st.Effort = ""
	m.Set(st)
	if got := xansi.Strip(m.View()); strings.Contains(got, "sonnet ·") {
		t.Errorf("view = %q, want no effort after the model", got)
	}
}
