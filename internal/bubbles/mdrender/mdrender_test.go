package mdrender

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

// sampleMD exercises a heading, a list, inline code, a fenced Go block, and
// a link, per the brief.
const sampleMD = `# Heading

- item one
- item two

Some ` + "`inline code`" + ` here and a [link](https://example.com).

` + "```go" + `
func main() {
	println("hi")
}
` + "```" + `
`

// testStyles is a pinned, terminal-independent Styles used by the golden
// tests, kept separate from DefaultStyles so a later change to
// DefaultStyles doesn't silently change the goldens.
func testStyles() Styles {
	return Styles{
		Text:    lipgloss.Color("#e0e0e0"),
		Muted:   lipgloss.Color("#888888"),
		Heading: lipgloss.Color("#4fc3f7"),
		Link:    lipgloss.Color("#29b6f6"),
		Code:    lipgloss.Color("#ffb74d"),
		CodeBg:  lipgloss.Color("#263238"),
		Quote:   lipgloss.Color("#9e9e9e"),
		Rule:    lipgloss.Color("#546e7a"),
	}
}

func TestRender_Golden(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	for _, width := range []int{40, 80} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			t.Parallel()
			lines := r.Render(sampleMD, width)
			golden.Assert(t, fmt.Sprintf("md_%d", width), strings.Join(lines, "\n"))
		})
	}
}

func TestRender_WidthRespected(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	for _, width := range []int{1, 2, 10, 40, 80} {
		lines := r.Render(sampleMD, width)
		for i, line := range lines {
			if w := ansi.Width(line); w > width {
				t.Errorf("width %d: line %d has width %d: %q", width, i, w, line)
			}
		}
	}
}

func TestRender_NonPositiveWidth(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	for _, width := range []int{0, -1} {
		if lines := r.Render(sampleMD, width); lines != nil {
			t.Errorf("width %d: Render(...) = %v, want nil", width, lines)
		}
	}
}

func TestRender_CachesPerWidth(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	r.Render(sampleMD, 60)
	r.Render(sampleMD, 60)
	if len(r.renderers) != 1 {
		t.Fatalf("len(r.renderers) = %d, want 1", len(r.renderers))
	}
}

func TestSetStyles_ChangesOutput(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	before := strings.Join(r.Render("# Heading\n", 40), "\n")

	r.SetStyles(Styles{
		Text:    lipgloss.Color("#000001"),
		Muted:   lipgloss.Color("#000002"),
		Heading: lipgloss.Color("#000003"),
		Link:    lipgloss.Color("#000004"),
		Code:    lipgloss.Color("#000005"),
		CodeBg:  lipgloss.Color("#000006"),
		Quote:   lipgloss.Color("#000007"),
		Rule:    lipgloss.Color("#000008"),
	})
	after := strings.Join(r.Render("# Heading\n", 40), "\n")

	if before == after {
		t.Fatalf("SetStyles did not change rendered output; got %q both times", before)
	}
	if len(r.renderers) != 1 {
		t.Fatalf("SetStyles should drop the cache; Render should have rebuilt exactly 1 renderer, got %d", len(r.renderers))
	}
}
