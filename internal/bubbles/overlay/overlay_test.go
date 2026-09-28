package overlay

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// TestCenterDropsKittyPlaceholders guards issue #18 (slk).
//
// Kitty's unicode-placeholder protocol encodes an image's ID in each
// placeholder cell's SGR foreground color (R = byte 2, G = byte 1,
// B = byte 0 of the 24-bit ID). The original bug: Center's per-cell FG
// darken mutated those IDs, surfacing whatever image happened to own the
// darkened ID — users saw avatars and image previews "go wonky" while any
// modal was open.
//
// The chosen remediation is to drop the kitty placement entirely for the
// duration of the overlay by blanking placeholder cells. The image data
// stays uploaded in the terminal; the next non-overlay frame re-emits the
// placeholder cells and the placement re-appears with no image-state
// plumbing. This test asserts that contract: after Center, the rendered
// output contains zero placeholder runes and zero "image ID-as-RGB" SGR
// escapes from the synthetic background we provided.
func TestCenterDropsKittyPlaceholders(t *testing.T) {
	const id uint32 = 42
	r := byte((id >> 16) & 0xFF)
	g := byte((id >> 8) & 0xFF)
	b := byte(id & 0xFF)
	idFG := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)

	placeholders := idFG +
		string(ansi.PlaceholderRune) +
		string(ansi.PlaceholderRune) +
		string(ansi.PlaceholderRune) +
		"\x1b[39m"

	const width, height = 12, 3
	bg := strings.Join([]string{
		strings.Repeat("a", width),
		placeholders + strings.Repeat(" ", width-3),
		strings.Repeat("b", width),
	}, "\n")

	// Narrow box so part of the placeholder row survives to the right
	// of the modal — that's the region the bug used to corrupt.
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Width(2).
		Height(1).
		Render("X")

	out := Center(bg, width, height, box, 0.5)

	if strings.ContainsRune(out, ansi.PlaceholderRune) {
		t.Fatalf("dim left kitty placeholder rune in output (image would render through dim):\n%q", out)
	}
	if strings.Contains(out, idFG) {
		t.Fatalf("dim left raw image-ID FG escape %q in output:\n%q", idFG, out)
	}
}

// TestCenterPreservesWideCharacter guards the fix for the
// reaction-picker-shows-no-color-emoji bug (slk). Composing a modal that
// contains a wide character (emoji 🔥, also CJK glyphs etc.) used to strip
// the glyph because the cell-copy loop called SetCell on the wide
// character's continuation column with an empty-content cell, overwriting
// the second column of the wide glyph.
func TestCenterPreservesWideCharacter(t *testing.T) {
	background := strings.Repeat(strings.Repeat(" ", 40)+"\n", 10)
	box := "🔥 fire"
	out := Center(background, 40, 10, box, 0.5)
	if !strings.Contains(out, "\U0001F525") {
		t.Errorf("Center output does NOT contain 🔥; cell-by-cell copy is dropping wide-character glyphs")
		t.Logf("output (first 200 bytes): %q", truncate(out, 200))
	}
}

// TestCenterPreservesWideCharsInBackground guards that the SAME bug doesn't
// bite the dim-background pass. Even if the modal doesn't overlap a wide
// char in the background, the dim loop used to call SetCell on every cell —
// which (lipgloss bug) destroys wide-char glyphs even when called with the
// same cell back at the same position. Fix: mutate cell colors in-place via
// the live pointer CellAt returns; don't call SetCell at all.
func TestCenterPreservesWideCharsInBackground(t *testing.T) {
	background := "🔥 burning\n" + strings.Repeat(strings.Repeat(" ", 40)+"\n", 9)
	box := "x" // tiny modal positioned in the middle; the emoji row stays uncovered
	out := Center(background, 40, 10, box, 0.5)
	if !strings.Contains(out, "\U0001F525") {
		t.Errorf("Center background-dim pass dropped the 🔥 glyph from the un-modaled row")
		t.Logf("output (first 200 bytes): %q", truncate(out, 200))
	}
}

// TestCenter_BoxLargerThanBackgroundIsClipped guards that a box that does
// not fit in the background does not panic and produces output clipped to
// the background's own dimensions.
func TestCenter_BoxLargerThanBackgroundIsClipped(t *testing.T) {
	const width, height = 6, 2
	background := strings.Repeat(strings.Repeat(".", width)+"\n", height)
	box := strings.Repeat(strings.Repeat("X", 10)+"\n", 3)
	box = strings.TrimSuffix(box, "\n")

	out := Center(background, width, height, box, 0.5)

	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Fatalf("expected %d lines; got %d: %q", height, len(lines), out)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d width = %d; want %d (%q)", i, w, width, ln)
		}
	}
}

// TestCenter_NegativeSizesDoNotPanic guards against a negative width or
// height reaching lipgloss.NewCanvas, which panics (makeslice: len out of
// range) on a negative length. Negative sizes should clamp to 0, the same
// as the already-graceful width == 0 / height == 0 case.
func TestCenter_NegativeSizesDoNotPanic(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
	}{
		{"negative width", -3, 2},
		{"negative height", 6, -2},
		{"both zero", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Center("bg", tt.width, tt.height, "X", 0.5)
			t.Logf("Center(%d, %d) = %q", tt.width, tt.height, out)
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
