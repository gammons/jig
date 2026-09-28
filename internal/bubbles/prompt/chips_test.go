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

// TestChips_RiskyDeleteBindingsAreAtomic covers the review's still-open
// critical #2: bubbles/textarea's own word/line deletion bindings
// (ctrl+w, ctrl+k, ctrl+u, alt+backspace, ...) must not leave a partial
// token behind either, whether the cursor sits at a chip's edge (right
// after pasting) or strictly inside it (after left x3).
func TestChips_RiskyDeleteBindingsAreAtomic(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 600)

	for _, key := range []string{"ctrl+w", "ctrl+k", "ctrl+u", "alt+backspace"} {
		for _, pos := range []string{"edge", "inside"} {
			t.Run(key+"/"+pos, func(t *testing.T) {
				m := New(nil)
				m.Focus()
				m, _ = m.Update(tea.PasteMsg{Content: long})
				if pos == "inside" {
					for range 3 {
						m, _ = m.Update(keyMsg("left"))
					}
				}
				m, _ = m.Update(keyMsg(key))
				if got := m.Value(); got != "" {
					t.Errorf("%s from the %s: Value() = %q, want empty (the whole chip removed)", key, pos, got)
				}
				assertNoMarkerRunes(t, m.Value())
			})
		}
	}
}

// soleToken returns the one live chip token in m.chips, failing the test
// if there isn't exactly one.
func soleToken(t *testing.T, m Model) string {
	t.Helper()
	if len(m.chips.byToken) != 1 {
		t.Fatalf("m.chips has %d live tokens, want 1", len(m.chips.byToken))
	}
	for tok := range m.chips.byToken {
		return tok
	}
	return ""
}

// TestChips_CtrlHCtrlDAreAtomic directly covers the gap the review
// found: ctrl+h (backspace's bubbles/textarea alias) and ctrl+d
// (forward-delete's alias) were reaching the textarea unguarded, since
// this package's earlier interception matched only "backspace"/"delete"
// by name. classify (delete.go) instead matches bubbles/textarea's own
// DeleteCharacterBackward/DeleteCharacterForward bindings, which cover
// every alias. Checked at a chip's leading edge, its trailing edge, and
// its interior.
func TestChips_CtrlHCtrlDAreAtomic(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 600)

	for _, key := range []string{"ctrl+h", "ctrl+d"} {
		for _, pos := range []string{"leading", "trailing", "inside"} {
			t.Run(key+"/"+pos, func(t *testing.T) {
				m := New(nil)
				m.Focus()
				m, _ = m.Update(tea.PasteMsg{Content: long})
				tokLen := len([]rune(soleToken(t, m)))
				switch pos {
				case "leading":
					m.ta.SetCursorColumn(0)
				case "trailing":
					m.ta.SetCursorColumn(tokLen)
				case "inside":
					m.ta.SetCursorColumn(tokLen / 2)
				}

				m, _ = m.Update(keyMsg(key))

				got := m.Value()
				if strings.Contains(got, "[pasted") {
					t.Errorf("%s at the %s: Value() = %q, contains a partial token fragment", key, pos, got)
				}
				assertNoMarkerRunes(t, got)
			})
		}
	}
}

// TestChips_EveryMutatingBindingIsAtomic loops over every bubbles/
// textarea DefaultKeyMap binding that can mutate the buffer (the two
// single-character deletions, the two word deletions, the two line
// deletions, the three case-changing bindings, and transpose) at five
// cursor positions relative to a chip embedded in a longer line —
// before it, at its leading edge, inside it, at its trailing edge, and
// after it — asserting Value never contains a partial token fragment or
// a marker rune for any combination. This is the review's structural
// ask: it doesn't matter whether a given (binding, position) pair
// actually damages the chip or leaves it untouched, only that none of
// the 70 combinations can ever produce a torn one.
func TestChips_EveryMutatingBindingIsAtomic(t *testing.T) {
	t.Parallel()

	bindings := []string{
		"backspace", "ctrl+h", // DeleteCharacterBackward
		"delete", "ctrl+d", // DeleteCharacterForward
		"ctrl+w", "alt+backspace", // DeleteWordBackward
		"alt+d", "alt+delete", // DeleteWordForward
		"ctrl+k",                  // DeleteAfterCursor
		"ctrl+u",                  // DeleteBeforeCursor
		"alt+l", "alt+u", "alt+c", // Lowercase/Uppercase/CapitalizeWordForward
		"ctrl+t", // TransposeCharacterBackward
	}
	positions := []string{"before", "start", "inside", "end", "after"}
	const pre, post = "PRE", "POST"

	for _, kb := range bindings {
		for _, pos := range positions {
			t.Run(kb+"/"+pos, func(t *testing.T) {
				m := New(nil)
				m.SetWidth(2000) // wide enough that this line never wraps
				m.Focus()
				m = typeText(m, pre)
				m, _ = m.Update(tea.PasteMsg{Content: strings.Repeat("x", 600)})
				m = typeText(m, post)

				tokLen := len([]rune(soleToken(t, m)))
				var col int
				switch pos {
				case "before":
					col = 0
				case "start":
					col = len(pre)
				case "inside":
					col = len(pre) + tokLen/2
				case "end":
					col = len(pre) + tokLen
				case "after":
					col = len(pre) + tokLen + len(post)
				}
				m.ta.SetCursorColumn(col)

				m, _ = m.Update(keyMsg(kb))

				got := m.Value()
				if strings.Contains(got, "[pasted") {
					t.Errorf("%s at %s: Value() = %q, contains a partial token fragment", kb, pos, got)
				}
				assertNoMarkerRunes(t, got)
			})
		}
	}
}

// TestChips_ReconcileStripsDamagedTokenFragments exercises chips.go's
// reconcile backstop directly (bypassing prompt.go's interception, which
// in normal use never lets a damaged token reach it — see reconcile's
// doc), against a hand-built fragment shaped like what a word-backward
// deletion from inside a token can leave: truncated visible text with no
// closing "]", plus a few orphaned marker runes.
func TestChips_ReconcileStripsDamagedTokenFragments(t *testing.T) {
	t.Parallel()

	c := newChips()
	c, _ = c.withToken(600, strings.Repeat("x", 600))

	markerRunes := []rune(chipMarker(0))
	damaged := "[pasted 600 " + string(markerRunes[:3])

	got := c.reconcile(damaged)
	if got != "" {
		t.Errorf("reconcile(%q) = %q, want empty (the fragment was the whole string)", damaged, got)
	}
}
