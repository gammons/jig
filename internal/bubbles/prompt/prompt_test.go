package prompt

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Fill:        lipgloss.NewStyle().Background(lipgloss.Color("#303030")),
		Bar:         lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaf00")),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "shift+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
	case "alt+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	case "@":
		return tea.KeyPressMsg{Code: '@', Text: "@"}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+h":
		return tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+t":
		return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	case "alt+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	case "alt+delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt}
	case "alt+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt}
	case "alt+l":
		return tea.KeyPressMsg{Code: 'l', Mod: tea.ModAlt}
	case "alt+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModAlt}
	case "alt+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// mustMsg runs cmd (which must be non-nil) and asserts its message has
// type T.
func mustMsg[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected a cmd, got nil")
	}
	msg := cmd()
	v, ok := msg.(T)
	if !ok {
		t.Fatalf("cmd yielded %T, want %T", msg, v)
	}
	return v
}

func TestPrompt_SubmitAndBlank(t *testing.T) {
	t.Parallel()

	blank := New(nil)
	blank.Focus()
	blank = typeText(blank, "   ")
	if _, cmd := blank.Update(keyMsg("enter")); cmd != nil {
		t.Errorf("blank submit: got a cmd, want none")
	}

	m := New(nil)
	m.Focus()
	m = typeText(m, "hello")
	_, cmd := m.Update(keyMsg("enter"))
	msg := mustMsg[SubmitMsg](t, cmd)
	if msg.Text != "hello" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello")
	}
}

func TestPrompt_NewlineKeys(t *testing.T) {
	t.Parallel()

	for _, k := range []string{"shift+enter", "alt+enter"} {
		m := New(nil)
		m.Focus()
		m = typeText(m, "a")
		m, cmd := m.Update(keyMsg(k))
		if cmd != nil {
			t.Errorf("%s: got a cmd, want none", k)
		}
		m = typeText(m, "b")
		if got, want := m.Value(), "a\nb"; got != want {
			t.Errorf("%s: Value() = %q, want %q", k, got, want)
		}
	}
}

func TestPrompt_GrowsToEightLines(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetWidth(40)

	check := func(step string, want int) {
		t.Helper()
		if got := m.Height(); got != want {
			t.Errorf("%s: Height() = %d, want %d", step, got, want)
		}
	}
	check("1 line", 5) // 1 content line + 2 fill rows + 2 edges

	for range 4 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}
	check("5 lines", 9)

	for range 7 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}
	check("12 lines requested", 12) // the visible viewport clamps to 8 content lines + 4
}

// TestPrompt_NeverDropsPastHeightCap covers the review's critical #1: past
// the visible cap, content must keep growing (and stay fully present in
// Value/View, scrolled into view), never be dropped or merged.
func TestPrompt_NeverDropsPastHeightCap(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetWidth(40)

	for range 14 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}
	// Check the scroll offset here, before typing "last" below: a real
	// key press's own Update call would reposition the viewport on its
	// own and mask a regression in the Newline case's own repositioning
	// (see prompt.go's reposition helper).
	if got, want := m.ta.ScrollYOffset(), 15-maxContentLines; got != want {
		t.Errorf("after growing past the cap: ScrollYOffset() = %d, want %d (the cursor's line scrolled into view)", got, want)
	}
	m = typeText(m, "last")

	lines := strings.Split(m.Value(), "\n")
	if len(lines) != 15 {
		t.Fatalf("Value() has %d lines, want 15 (no lines dropped or merged): %q", len(lines), m.Value())
	}
	for i, l := range lines {
		if i < 14 && l != "" {
			t.Errorf("line %d = %q, want empty (only the last line was typed into)", i, l)
		}
	}
	if lines[14] != "last" {
		t.Errorf("last line = %q, want %q", lines[14], "last")
	}

	if got := m.ta.Height(); got != maxContentLines {
		t.Errorf("ta.Height() = %d, want the visible cap %d", got, maxContentLines)
	}
	if got, want := m.Height(), maxContentLines+4; got != want {
		t.Errorf("Height() = %d, want %d (%d content lines + fill rows + edges)", got, want, maxContentLines)
	}
	if !strings.Contains(m.View(), "last") {
		t.Errorf("cursor line not scrolled into view:\n%s", m.View())
	}
}

// TestPrompt_PasteAtCapNeverDropped covers the review's critical #1 for a
// single paste (not incremental typing): content already at the visible
// cap must not cause a subsequent paste to be silently discarded.
func TestPrompt_PasteAtCapNeverDropped(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetWidth(80)
	for range 10 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}

	paste := strings.Repeat("q", 70)
	m, _ = m.Update(tea.PasteMsg{Content: paste})
	if got := m.Value(); !strings.Contains(got, paste) {
		t.Errorf("paste at the height cap was dropped: Value() = %q", got)
	}
}

// TestPrompt_PanelShape: column 0 is the bar column (blank while
// blurred); the panel fills the rest: a ▄ edge, a blank fill row, the text
// with one column of padding each side, a blank fill row, a ▀ edge.
func TestPrompt_PanelShape(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.SetWidth(20)
	m.Focus()
	m = typeText(m, "hi")
	m.Blur()
	rows := strings.Split(xansi.Strip(m.View()), "\n")
	blank := strings.Repeat(" ", 20)
	want := []string{
		" " + strings.Repeat("▄", 19),
		blank,
		"  hi" + strings.Repeat(" ", 16),
		blank,
		" " + strings.Repeat("▀", 19),
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
}

// TestPrompt_BarOnlyWhileFocused: in INSERT (focused) column 0 is the bar
// in the Bar color, exactly as tall as the panel: ▌ on the full rows, and
// ▖/▘ (the lower/upper half of ▌) on the ▄/▀ edge rows, which the panel
// only half fills. Blurred, column 0 is blank.
func TestPrompt_BarOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	st := pinnedStyles()
	m := New(nil, WithStyles(st))
	m.SetWidth(20)
	m.Focus()
	view := m.View()
	rows := strings.Split(xansi.Strip(view), "\n")
	want := []string{"▖", "▌", "▌", "▌", "▘"}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for y, r := range rows {
		if !strings.HasPrefix(r, want[y]) {
			t.Errorf("focused row %d = %q, want it to start with %s", y, r, want[y])
		}
		if got := cellFg(t, view, 0, y); !sameColor(got, st.Bar.GetForeground()) {
			t.Errorf("focused row %d bar fg = %v, want %v", y, got, st.Bar.GetForeground())
		}
	}
	m.Blur()
	for y, r := range strings.Split(xansi.Strip(m.View()), "\n") {
		if !strings.HasPrefix(r, " ") {
			t.Errorf("blurred row %d = %q, want a blank bar column", y, r)
		}
	}
}

// cellColors returns the foreground and background of the cell at (x, y)
// of a rendered view.
func cellColors(t *testing.T, view string, x, y int) (fg, bg color.Color) {
	t.Helper()
	c := lipgloss.NewCanvas(lipgloss.Width(view), lipgloss.Height(view))
	c.Compose(lipgloss.NewLayer(view))
	cell := c.CellAt(x, y)
	if cell == nil {
		t.Fatalf("no cell at (%d, %d)", x, y)
	}
	return cell.Style.Fg, cell.Style.Bg
}

func cellBg(t *testing.T, view string, x, y int) color.Color {
	t.Helper()
	_, bg := cellColors(t, view, x, y)
	return bg
}

func cellFg(t *testing.T, view string, x, y int) color.Color {
	t.Helper()
	fg, _ := cellColors(t, view, x, y)
	return fg
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// TestPrompt_FillIsTheSameFocusedOrNot: the panel always uses Fill (the
// bar, not the fill, shows focus): behind every content cell, padding and
// fill rows included, and as the ▄/▀ edges' color.
func TestPrompt_FillIsTheSameFocusedOrNot(t *testing.T) {
	t.Parallel()

	st := pinnedStyles()
	fill := st.Fill.GetBackground()
	m := New(nil, WithStyles(st))
	m.SetWidth(20)
	check := func(label string) {
		t.Helper()
		view := m.View()
		for _, y := range []int{1, 2, 3} {
			for _, x := range []int{1, 19} {
				if got := cellBg(t, view, x, y); !sameColor(got, fill) {
					t.Errorf("%s: cell (%d,%d) bg = %v, want %v", label, x, y, got, fill)
				}
			}
		}
		for _, y := range []int{0, 4} {
			if got := cellFg(t, view, 1, y); !sameColor(got, fill) {
				t.Errorf("%s: edge row %d fg = %v, want %v", label, y, got, fill)
			}
		}
	}

	check("blurred")
	m.Focus()
	check("focused")
}

func TestPrompt_QueuedLabelOnTopEdge(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.SetWidth(30)
	m.SetQueued(true)
	rows := strings.Split(xansi.Strip(m.View()), "\n")
	if !strings.HasSuffix(rows[0], " ⏳ queued ▄") {
		t.Errorf("top edge = %q, want the queued label right-aligned", rows[0])
	}
	if w := xansi.StringWidth(rows[0]); w != 30 {
		t.Errorf("top edge width = %d, want 30", w)
	}
}

// TestPrompt_BlurredIgnoresInput covers the review's item 4: Paste and
// KeyPressMsg must be no-ops while the prompt is blurred.
func TestPrompt_BlurredIgnoresInput(t *testing.T) {
	t.Parallel()

	m := New(nil) // starts blurred; Focus is never called
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd != nil {
		t.Errorf("KeyPressMsg while blurred: got a cmd, want none")
	}
	if got := m.Value(); got != "" {
		t.Errorf("KeyPressMsg while blurred: Value() = %q, want empty", got)
	}

	m, cmd = m.Update(tea.PasteMsg{Content: "pasted text"})
	if cmd != nil {
		t.Errorf("PasteMsg while blurred: got a cmd, want none")
	}
	if got := m.Value(); got != "" {
		t.Errorf("PasteMsg while blurred: Value() = %q, want empty", got)
	}
}

func TestPrompt_EditorRoundTrip(t *testing.T) {
	t.Parallel()

	edit := func(text string) tea.Cmd {
		if text != "old" {
			t.Errorf("edit called with %q, want %q", text, "old")
		}
		return func() tea.Msg { return EditedMsg{Text: "new"} }
	}
	m := New(edit)
	m.Focus()
	m = typeText(m, "old")
	_, cmd := m.Update(keyMsg("ctrl+e"))
	edited := mustMsg[EditedMsg](t, cmd)
	if edited.Text != "new" {
		t.Fatalf("EditedMsg.Text = %q, want %q", edited.Text, "new")
	}
	m, _ = m.Update(edited)
	if got := m.Value(); got != "new" {
		t.Errorf("Value() = %q, want %q", got, "new")
	}
}

func TestPrompt_AtEmitsMention(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	_, cmd := m.Update(keyMsg("@"))
	mustMsg[MentionMsg](t, cmd)
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want empty (nothing inserted)", got)
	}
}

func TestPrompt_AtMidWordIsLiteral(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		before, after string
		mention       bool
	}{
		{"user", "example.com", false}, // an email address
		{"npm i foo", "types", false},  // mid-word after "foo"
		{"hi ", "", true},              // at a word start
		{"line one\n", "", true},       // at the start of a line
		{"", "", true},                 // at the start of the input
	} {
		m := New(nil)
		m.Focus()
		m = typeText(m, strings.ReplaceAll(tt.before, "\n", ""))
		if strings.Contains(tt.before, "\n") {
			m, _ = m.Update(keyMsg("shift+enter"))
		}
		m, cmd := m.Update(keyMsg("@"))
		var got tea.Msg
		if cmd != nil {
			got = cmd()
		}
		if _, ok := got.(MentionMsg); ok != tt.mention {
			t.Errorf("@ after %q: mention = %v, want %v", tt.before, ok, tt.mention)
		}
		if tt.mention {
			continue
		}
		m = typeText(m, tt.after)
		if want := tt.before + "@" + tt.after; m.Value() != want {
			t.Errorf("Value() = %q, want %q (a literal @)", m.Value(), want)
		}
	}
}

// TestPrompt_GoldenPlaceholder renders at width 80 (a realistic terminal
// width) so the full placeholder — "Message coder…  (ctrl+p actions · @
// files)" — fits on one line untouched (review item 3).
func TestPrompt_GoldenPlaceholder(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(xansi.Strip(view), "Message coder…  (ctrl+p actions · @ files)") {
		t.Errorf("View() does not contain the full placeholder at width 80:\n%s", view)
	}
	golden.Assert(t, "prompt_placeholder", view)
}

func TestPrompt_GoldenQueued(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(80)
	m.SetQueued(true)
	golden.Assert(t, "prompt_queued", m.View())
}

func TestPrompt_GoldenChip(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetWidth(40)
	m.Focus()
	m, _ = m.Update(tea.PasteMsg{Content: strings.Repeat("z", 600)})
	golden.Assert(t, "prompt_chip", m.View())
}

// TestPrompt_PlaceholderTruncatesNarrow covers review item 3's other half:
// a width too narrow for the full placeholder truncates gracefully to one
// line ending in "…", rather than bubbles/textarea's own multi-line
// wrapping silently cutting it off with no ellipsis.
func TestPrompt_PlaceholderTruncatesNarrow(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(20)
	view := m.View()

	lines := strings.Split(view, "\n")
	if len(lines) != 1+chromeRows { // 1 content line + fill rows + edges
		t.Fatalf("View() has %d lines, want %d (placeholder must stay on one line):\n%s", len(lines), 1+chromeRows, view)
	}
	content := lines[1+padY]
	if !strings.Contains(xansi.Strip(content), "…") {
		t.Errorf("content line does not end in an ellipsis: %q", content)
	}
	if strings.Contains(xansi.Strip(view), "files)") {
		t.Errorf("full placeholder text leaked through at a too-narrow width:\n%s", view)
	}
	golden.Assert(t, "prompt_placeholder_narrow", view)
}
