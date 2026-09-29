package overlay

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

func TestFill_NilBgUnchanged(t *testing.T) {
	t.Parallel()
	box := "ab\ncd"
	if got := Fill(box, nil); got != box {
		t.Fatalf("Fill(nil) = %q, want %q", got, box)
	}
}

func TestFill_PaintsUnsetCellsKeepsSetOnes(t *testing.T) {
	t.Parallel()
	own := lipgloss.NewStyle().Background(lipgloss.Color("#00ff00")).Render("X")
	box := "a " + own + "\n" + "   "
	out := Fill(box, lipgloss.Color("#ff0000"))

	canvas := lipgloss.NewCanvas(3, 2)
	canvas.Compose(lipgloss.NewLayer(out))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			c := canvas.CellAt(x, y)
			want := lipgloss.Color("#ff0000")
			if x == 2 && y == 0 {
				want = lipgloss.Color("#00ff00")
			}
			if c == nil || c.Style.Bg == nil {
				t.Fatalf("cell (%d,%d) has no background", x, y)
			}
			r1, g1, b1, _ := c.Style.Bg.RGBA()
			r2, g2, b2, _ := want.RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 {
				t.Errorf("cell (%d,%d) bg = %v, want %v", x, y, c.Style.Bg, want)
			}
		}
	}
}

func TestFill_KeepsWideCharacters(t *testing.T) {
	t.Parallel()
	box := "世界\nab  "
	out := Fill(box, lipgloss.Color("#ff0000"))
	if !strings.Contains(out, "世界") {
		t.Fatalf("Fill dropped wide characters: %q", out)
	}
	if w := ansi.Width(strings.Split(out, "\n")[0]); w != 4 {
		t.Errorf("row width = %d, want 4", w)
	}
}
