package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestChips_PasteCollapsesAndExpands(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	long := strings.Repeat("x", 600)
	m, _ = m.Update(tea.PasteMsg{Content: long})

	if !strings.Contains(m.View(), "[pasted 600 chars]") {
		t.Errorf("View() does not contain the chip token:\n%s", m.View())
	}
	if got := m.Value(); got != long {
		t.Errorf("Value() = %d runes, want the original %d runes", len([]rune(got)), len([]rune(long)))
	}

	// A short paste is inserted verbatim, not collapsed. Widen the
	// textarea so this doesn't also trip the (separate) visual-line growth
	// cap: it's the chip threshold under test here, not wrapping.
	m2 := New(nil)
	m2.SetWidth(600)
	m2.Focus()
	short := strings.Repeat("y", 500)
	m2, _ = m2.Update(tea.PasteMsg{Content: short})
	if got := m2.Value(); got != short {
		t.Errorf("short paste: Value() = %d runes, want the original %d runes (no chip)", len([]rune(got)), len([]rune(short)))
	}
}

func TestChips_BackspaceRemovesWholeChip(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	long := strings.Repeat("y", 600)
	m, _ = m.Update(tea.PasteMsg{Content: long})
	m, _ = m.Update(keyMsg("backspace"))
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want empty after one backspace removes the whole chip", got)
	}

	// A backspace not right after a chip only removes the last character.
	m2 := New(nil)
	m2.Focus()
	m2, _ = m2.Update(tea.PasteMsg{Content: long})
	m2, _ = m2.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	m2, _ = m2.Update(keyMsg("backspace"))
	if got := m2.Value(); got != long {
		t.Errorf("Value() = %d runes, want the chip's %d runes restored", len([]rune(got)), len([]rune(long)))
	}
}

func TestChips_DistinctPastesOfSameLengthStayDistinct(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	a := strings.Repeat("a", 600)
	b := strings.Repeat("b", 600)
	m, _ = m.Update(tea.PasteMsg{Content: a})
	m, _ = m.Update(tea.PasteMsg{Content: b})
	if got, want := m.Value(), a+b; got != want {
		t.Errorf("Value() = %q, want %q", got, want)
	}

	// Removing the second (rightmost) chip must not disturb the first.
	m, _ = m.Update(keyMsg("backspace"))
	if got := m.Value(); got != a {
		t.Errorf("after removing the second chip: Value() = %d runes, want %d (just the first paste)", len([]rune(got)), len([]rune(a)))
	}
}
