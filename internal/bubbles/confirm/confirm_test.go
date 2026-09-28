package confirm

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaf00")),
		Text:   lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Key:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fafff")),
		Label:  lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Scroll: lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	if k == "esc" {
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func trustEffects() []string {
	return []string{
		"permissions.bash → allow",
		"providers.anthropic.base_url → https://example.com",
		"instructions += docs/rules.md",
	}
}

func TestConfirm_Keys(t *testing.T) {
	t.Parallel()

	t.Run("a key equal to a Choice's Key emits ChosenMsg", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetSize(100, 30)
		m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}, {Key: "n", Label: "Don't trust"}})

		_, cmd := m.Update(keyMsg("t"))
		if cmd == nil {
			t.Fatalf("key %q produced no Cmd", "t")
		}
		msg, ok := cmd().(ChosenMsg)
		if !ok || msg.Key != "t" {
			t.Fatalf("cmd() = %#v, want ChosenMsg{Key: \"t\"}", msg)
		}
	})

	t.Run("esc matches a Choice with Key esc", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetSize(100, 30)
		m.Set("Continue?", nil, []Choice{{Key: "t", Label: "Trust"}, {Key: "esc", Label: "Not now"}})

		_, cmd := m.Update(keyMsg("esc"))
		if cmd == nil {
			t.Fatalf("esc produced no Cmd")
		}
		msg, ok := cmd().(ChosenMsg)
		if !ok || msg.Key != "esc" {
			t.Fatalf("cmd() = %#v, want ChosenMsg{Key: \"esc\"}", msg)
		}
	})

	t.Run("esc without a matching Choice is a no-op", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetSize(100, 30)
		m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}, {Key: "n", Label: "Don't trust"}})

		_, cmd := m.Update(keyMsg("esc"))
		if cmd != nil {
			t.Fatalf("esc without a matching Choice produced a Cmd")
		}
	})

	t.Run("a key that matches no Choice is a no-op", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetSize(100, 30)
		m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}, {Key: "n", Label: "Don't trust"}})

		_, cmd := m.Update(keyMsg("x"))
		if cmd != nil {
			t.Fatalf("unmatched key produced a Cmd")
		}
	})

	t.Run("j/k scroll the body lines when they overflow", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetSize(100, 6) // avail = 6-4 = 2 visible lines, 3 total -> overflow
		m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}})

		before := xansi.Strip(m.View())
		m, cmd := m.Update(keyMsg("j"))
		if cmd != nil {
			t.Fatalf("scroll key produced a Cmd, want nil")
		}
		after := xansi.Strip(m.View())
		if before == after {
			t.Fatalf("View() unchanged after j, want the visible window to shift")
		}
		if !strings.Contains(after, "providers.anthropic.base_url") {
			t.Fatalf("View() after j = %q, want the second line visible", after)
		}
		if strings.Contains(after, "permissions.bash") {
			t.Fatalf("View() after j = %q, want the first line scrolled out", after)
		}

		// k scrolls back up.
		m, _ = m.Update(keyMsg("k"))
		back := xansi.Strip(m.View())
		if back != before {
			t.Fatalf("View() after j then k = %q, want it back to %q", back, before)
		}
	})

	t.Run("j/k do nothing when lines fit", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.SetSize(100, 30)
		m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}})

		before := m.View()
		m, cmd := m.Update(keyMsg("j"))
		if cmd != nil {
			t.Fatalf("j with no overflow produced a Cmd")
		}
		if after := m.View(); after != before {
			t.Fatalf("View() changed after j with no overflow")
		}
	})
}

func TestConfirm_ViewSize(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(100, 30)
	m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}, {Key: "n", Label: "Don't trust"}})
	v := m.View()
	lines := strings.Split(v, "\n")
	w := ansi.Width(lines[0])
	for i, l := range lines {
		if ansi.Width(l) != w {
			t.Fatalf("line %d width = %d, want %d (every line the same box width)", i, ansi.Width(l), w)
		}
	}
}

func TestConfirm_EmptyWithoutSize(t *testing.T) {
	t.Parallel()
	m := New()
	m.Set("Trust project?", trustEffects(), []Choice{{Key: "t", Label: "Trust"}})
	if got := m.View(); got != "" {
		t.Fatalf("View() without SetSize = %q, want \"\"", got)
	}
}

func TestGolden_ConfirmTrust(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetSize(90, 20)
	m.Set("Trust /home/user/project?", []string{
		"permissions.bash → allow",
		"providers.anthropic.base_url → https://example.com",
		"agents.reviewer (from .claude/agents/reviewer.md) permissions → allow edit",
		"instructions += docs/rules.md",
	}, []Choice{{Key: "t", Label: "Trust"}, {Key: "n", Label: "Don't trust"}})
	golden.Assert(t, "confirm_trust", m.View())
}
