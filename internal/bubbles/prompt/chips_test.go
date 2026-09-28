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

// assertNoMarkerRunes fails if s contains any chip marker rune
// (U+FE00-U+FE0F) — Value must never leak them (review critical #2).
func assertNoMarkerRunes(t *testing.T, s string) {
	t.Helper()
	for _, r := range s {
		if r >= 0xFE00 && r <= 0xFE0F {
			t.Errorf("Value() contains a marker rune %U: %q", r, s)
		}
	}
}

// TestChips_EditingInsideChipIsAtomic covers the review's critical #2: a
// chip token is atomic. Moving into its interior and deleting removes it
// whole (never a partial/damaged token), and typing from its interior
// first jumps past it instead of splitting it apart.
func TestChips_EditingInsideChipIsAtomic(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 600)

	// left x3 (landing inside the ~25-rune token) then backspace: the
	// whole chip is removed, not damaged.
	m := New(nil)
	m.Focus()
	m, _ = m.Update(tea.PasteMsg{Content: long})
	for range 3 {
		m, _ = m.Update(keyMsg("left"))
	}
	m, _ = m.Update(keyMsg("backspace"))
	if got := m.Value(); got != "" {
		t.Errorf("left x3 + backspace: Value() = %q, want empty (the whole chip removed)", got)
	}
	assertNoMarkerRunes(t, m.Value())

	// left x3 then type 'z': the cursor jumps past the chip first, so the
	// paste survives intact with 'z' appended after it.
	m2 := New(nil)
	m2.Focus()
	m2, _ = m2.Update(tea.PasteMsg{Content: long})
	for range 3 {
		m2, _ = m2.Update(keyMsg("left"))
	}
	m2, _ = m2.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if got, want := m2.Value(), long+"z"; got != want {
		t.Errorf("left x3 + type 'z': Value() = %d runes, want %d runes (the intact paste plus 'z')", len([]rune(got)), len([]rune(want)))
	}
	assertNoMarkerRunes(t, m2.Value())

	// left x3 then forward-delete: symmetric to backspace, also removes
	// the whole chip.
	m3 := New(nil)
	m3.Focus()
	m3, _ = m3.Update(tea.PasteMsg{Content: long})
	for range 3 {
		m3, _ = m3.Update(keyMsg("left"))
	}
	m3, _ = m3.Update(keyMsg("delete"))
	if got := m3.Value(); got != "" {
		t.Errorf("left x3 + delete: Value() = %q, want empty (the whole chip removed)", got)
	}
	assertNoMarkerRunes(t, m3.Value())
}
