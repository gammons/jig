package picker

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Title:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff0000")),
		Header:     lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Text:       lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Detail:     lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Match:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaf00")),
		Selected:   lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Disabled:   lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
		Current:    lipgloss.NewStyle().Foreground(lipgloss.Color("#5fff5f")),
		Mark:       lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Border:     lipgloss.NewStyle().Foreground(lipgloss.Color("#00ffff")),
		Background: lipgloss.Color("#202020"),
	}
}

// loadFunc builds a LoadFunc from a table of level ID -> items.
func loadFunc(items map[string][]Item) LoadFunc {
	return func(level Level) tea.Cmd {
		its := items[level.ID]
		return func() tea.Msg { return ItemsMsg{Level: level.ID, Items: its} }
	}
}

// flatten runs cmd and, recursively, every Cmd in a tea.BatchMsg, in the
// absence of a real Program to do it.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, flatten(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// open opens root and feeds back any ItemsMsg its Cmd produced.
func open(t *testing.T, m Model, root Level) Model {
	t.Helper()
	cmd := m.Open(root)
	for _, msg := range flatten(cmd) {
		if im, ok := msg.(ItemsMsg); ok {
			m, _ = m.Update(im)
		}
	}
	return m
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
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

func rows(m Model) []row {
	return m.stack[len(m.stack)-1].rows
}

func topLevel(m Model) Level {
	return m.stack[len(m.stack)-1].level
}

func itemIDs(m Model) []string {
	var ids []string
	for _, r := range rows(m) {
		if r.kind == rowItem {
			ids = append(ids, r.item.ID)
		}
	}
	return ids
}

func TestPicker_FilterRanksAndTieBreaks(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "nomatch", Title: "Close tab"},
		{ID: "best", Title: "Open"},
		{ID: "tie1", Title: "Reopen document"},
		{ID: "tie2", Title: "Reopen document"},
	}
	m := New(loadFunc(map[string][]Item{"root": items}))
	m.SetSize(100, 40)
	m.SetRecent([]string{"tie2"})
	m = open(t, m, Level{ID: "root", Title: "Root", Actions: true})

	m = typeText(m, "open")
	got := itemIDs(m)
	want := []string{"best", "tie2", "tie1"}
	if !slices.Equal(got, want) {
		t.Fatalf("filtered order = %v, want %v (best score first, then recency tie-break)", got, want)
	}
}

func TestPicker_RecentGroupAtRoot(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "s1", Title: "s1", Group: "Session"},
		{ID: "s2", Title: "s2", Group: "Session"},
		{ID: "t1", Title: "t1", Group: "Tools"},
	}
	m := New(loadFunc(map[string][]Item{"root": items}))
	m.SetSize(100, 40)
	m.SetRecent([]string{"t1", "s2", "s1"})
	m = open(t, m, Level{ID: "root", Title: "Root", Actions: true})

	rs := rows(m)
	if rs[0].kind != rowHeader || rs[0].header != "Recent" {
		t.Fatalf("rows[0] = %+v, want the Recent header", rs[0])
	}
	if rs[1].item.ID != "t1" || rs[2].item.ID != "s2" || rs[3].item.ID != "s1" {
		t.Fatalf("Recent group items = %+v, want t1, s2, s1 (most recent first)", rs[1:4])
	}
}

func TestPicker_DrillAndBackspacePops(t *testing.T) {
	t.Parallel()
	sub := Level{ID: "theme", Title: "Theme"}
	root := []Item{{ID: "pick-theme", Title: "Pick theme", Drill: &sub}}
	theme := []Item{{ID: "dark", Title: "Dark"}}
	m := New(loadFunc(map[string][]Item{"root": root, "theme": theme}))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root"})

	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg("enter"))
	for _, msg := range flatten(cmd) {
		if im, ok := msg.(ItemsMsg); ok {
			m, _ = m.Update(im)
		}
	}
	if got := topLevel(m).ID; got != "theme" {
		t.Fatalf("after drill, top level = %s, want theme", got)
	}
	if len(m.stack) != 2 {
		t.Fatalf("stack depth = %d, want 2", len(m.stack))
	}

	m, _ = m.Update(keyMsg("backspace"))
	if got := topLevel(m).ID; got != "root" {
		t.Fatalf("after backspace, top level = %s, want root", got)
	}
	if len(m.stack) != 1 {
		t.Fatalf("stack depth = %d, want 1", len(m.stack))
	}
	if got := itemIDs(m); len(got) != 1 || got[0] != "pick-theme" {
		t.Fatalf("root items after pop = %v, want the cached pick-theme item (no reload)", got)
	}

	// backspace at the root with an empty query is a no-op.
	m, _ = m.Update(keyMsg("backspace"))
	if len(m.stack) != 1 {
		t.Fatalf("root backspace popped past the root: stack depth = %d", len(m.stack))
	}
}

func TestPicker_MultiMark(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "a", Title: "a"},
		{ID: "b", Title: "b"},
		{ID: "c", Title: "c", Disabled: true},
	}
	m := New(loadFunc(map[string][]Item{"root": items}))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root", Multi: true})

	m, _ = m.Update(keyMsg("tab")) // mark a
	m, _ = m.Update(keyMsg("down"))
	m, _ = m.Update(keyMsg("tab")) // mark b
	m, _ = m.Update(keyMsg("down"))
	m, _ = m.Update(keyMsg("tab")) // c is disabled: no-op

	top := m.stack[len(m.stack)-1]
	if len(top.marks) != 2 || !top.marks["a"] || !top.marks["b"] {
		t.Fatalf("marks = %v, want a and b only", top.marks)
	}

	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg("enter"))
	msgs := flatten(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter produced %d messages, want 1", len(msgs))
	}
	chosen, ok := msgs[0].(ChosenMsg)
	if !ok {
		t.Fatalf("enter produced %T, want ChosenMsg", msgs[0])
	}
	if len(chosen.Items) != 2 || chosen.Items[0].ID != "a" || chosen.Items[1].ID != "b" {
		t.Fatalf("ChosenMsg.Items = %+v, want a then b", chosen.Items)
	}
	if m.IsOpen() {
		t.Fatalf("picker stayed open after choosing")
	}
}

func TestPicker_DisabledNotChosen(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "only", Title: "Disabled action", Disabled: true}}
	m := New(loadFunc(map[string][]Item{"root": items}))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root"})

	m2, cmd := m.Update(keyMsg("enter"))
	if cmd != nil {
		t.Fatalf("enter on a disabled item returned a Cmd, want nil")
	}
	if !m2.IsOpen() {
		t.Fatalf("enter on a disabled item closed the picker")
	}
}

func TestPicker_InputLevelEmitsInputMsg(t *testing.T) {
	t.Parallel()
	m := New(loadFunc(nil))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "rename", Title: "Rename", Input: true, Initial: "old-name"})

	if got := m.stack[0].input.Value(); got != "old-name" {
		t.Fatalf("initial input value = %q, want old-name", got)
	}

	for range 4 {
		m, _ = m.Update(keyMsg("backspace"))
	}
	m = typeText(m, "new")

	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg("enter"))
	msgs := flatten(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter produced %d messages, want 1", len(msgs))
	}
	got, ok := msgs[0].(InputMsg)
	if !ok {
		t.Fatalf("enter produced %T, want InputMsg", msgs[0])
	}
	if got.Text != "old-new" {
		t.Fatalf("InputMsg.Text = %q, want old-new", got.Text)
	}
	if got.Level.ID != "rename" {
		t.Fatalf("InputMsg.Level.ID = %q, want rename", got.Level.ID)
	}
	if m.IsOpen() {
		t.Fatalf("picker stayed open after InputMsg")
	}
}

func TestPicker_EscEmitsClosedMsg(t *testing.T) {
	t.Parallel()
	m := New(loadFunc(nil))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "rename", Title: "Rename", Input: true})

	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg("esc"))
	msgs := flatten(cmd)
	if len(msgs) != 1 {
		t.Fatalf("esc produced %d messages, want 1", len(msgs))
	}
	closed, ok := msgs[0].(ClosedMsg)
	if !ok || closed.Level.ID != "rename" {
		t.Fatalf("esc produced %+v, want ClosedMsg{Level.ID: rename}", msgs[0])
	}
	if m.IsOpen() {
		t.Fatalf("picker stayed open after esc")
	}
}

func TestPicker_StaleItemsIgnored(t *testing.T) {
	t.Parallel()
	sub := Level{ID: "theme", Title: "Theme"}
	root := []Item{{ID: "pick-theme", Title: "Pick theme", Drill: &sub}}
	m := New(loadFunc(map[string][]Item{"root": root})) // no "theme" entry
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root"})

	m, _ = m.Update(keyMsg("enter")) // drills into theme, still loading
	if topLevel(m).ID != "theme" {
		t.Fatalf("top level = %s, want theme", topLevel(m).ID)
	}
	if !m.stack[1].loading {
		t.Fatalf("theme frame not marked loading")
	}

	// A stale ItemsMsg for the root (no longer on top) must be ignored.
	m, _ = m.Update(ItemsMsg{Level: "root", Items: []Item{{ID: "leaked", Title: "leaked"}}})
	if len(m.stack) != 2 || topLevel(m).ID != "theme" {
		t.Fatalf("stale ItemsMsg changed the stack: %+v", m.stack)
	}
	if !m.stack[1].loading || m.stack[1].items != nil {
		t.Fatalf("stale ItemsMsg mutated the theme frame: %+v", m.stack[1])
	}

	// The correct ItemsMsg still applies.
	m, _ = m.Update(ItemsMsg{Level: "theme", Items: []Item{{ID: "dark", Title: "Dark"}}})
	if got := itemIDs(m); len(got) != 1 || got[0] != "dark" {
		t.Fatalf("theme items after its own ItemsMsg = %v, want [dark]", got)
	}
}

func TestPicker_PreviewOnMove(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "a", Title: "a"}, {ID: "b", Title: "b"}, {ID: "c", Title: "c"}}
	var previewed []string
	preview := func(_ Level, it Item) tea.Cmd {
		previewed = append(previewed, it.ID)
		return nil
	}
	m := New(loadFunc(map[string][]Item{"root": items}), WithPreview(preview))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root"})

	// The level's initial highlight is not previewed: it's whatever's
	// already applied (or, absent a Current item, just the first row),
	// not something the user asked to see a preview of.
	if len(previewed) != 0 {
		t.Fatalf("preview after load = %v, want none", previewed)
	}

	m, _ = m.Update(keyMsg("down")) // -> b
	m, _ = m.Update(keyMsg("down")) // -> c
	m, _ = m.Update(keyMsg("down")) // already on c: no movement, no new preview

	if !slices.Equal(previewed, []string{"b", "c"}) {
		t.Fatalf("preview calls = %v, want [b c] (once per highlight change)", previewed)
	}
}

func TestPicker_PreviewStartsOnCurrentNoInitialFire(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "a", Title: "a"}, {ID: "b", Title: "b", Current: true}, {ID: "c", Title: "c"}}
	var previewed []string
	preview := func(_ Level, it Item) tea.Cmd {
		previewed = append(previewed, it.ID)
		return nil
	}
	m := New(loadFunc(map[string][]Item{"root": items}), WithPreview(preview))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root"})

	if got, ok := m.stack[0].current(); !ok || got.ID != "b" {
		t.Fatalf("initial selection = %+v, %v, want the Current item b", got, ok)
	}
	if len(previewed) != 0 {
		t.Fatalf("preview after load = %v, want none", previewed)
	}

	m, _ = m.Update(keyMsg("down")) // -> c
	if !slices.Equal(previewed, []string{"c"}) {
		t.Fatalf("preview calls = %v, want [c]", previewed)
	}
	m, _ = m.Update(keyMsg("up")) // back to b
	if !slices.Equal(previewed, []string{"c", "b"}) {
		t.Fatalf("preview calls = %v, want [c b]", previewed)
	}
}

func TestPicker_SizeClamp(t *testing.T) {
	t.Parallel()
	var m Model

	m.SetSize(200, 50)
	if m.boxW != 80 {
		t.Errorf("SetSize(200,50): boxW = %d, want 80", m.boxW)
	}
	if m.boxH > 30 {
		t.Errorf("SetSize(200,50): boxH = %d, want <= 30 (60%% of 50)", m.boxH)
	}

	m.SetSize(60, 20)
	if m.boxW != 42 {
		t.Errorf("SetSize(60,20): boxW = %d, want 42", m.boxW)
	}
	if m.boxH > 12 {
		t.Errorf("SetSize(60,20): boxH = %d, want <= 12 (60%% of 20)", m.boxH)
	}
}

func TestGolden_PickerRoot(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "new", Title: "New session", Group: "Session"},
		{ID: "rename", Title: "Rename session", Group: "Session"},
		{ID: "quit", Title: "Quit", Group: "App", Disabled: true},
	}
	m := New(loadFunc(map[string][]Item{"root": items}), WithStyles(pinnedStyles()))
	m.SetSize(60, 20)
	m.SetRecent([]string{"rename"})
	m = open(t, m, Level{ID: "root", Title: "Actions", Actions: true})
	golden.Assert(t, "picker_root", m.View())
}

func TestGolden_PickerFiltered(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "new", Title: "New session", Group: "Session"},
		{ID: "rename", Title: "Rename session", Group: "Session"},
		{ID: "quit", Title: "Quit", Group: "App"},
	}
	m := New(loadFunc(map[string][]Item{"root": items}), WithStyles(pinnedStyles()))
	m.SetSize(60, 20)
	m = open(t, m, Level{ID: "root", Title: "Actions", Actions: true})
	m = typeText(m, "session")
	golden.Assert(t, "picker_filtered", m.View())
}

func TestGolden_PickerDrill(t *testing.T) {
	t.Parallel()
	sub := Level{ID: "theme", Title: "Theme"}
	root := []Item{{ID: "pick-theme", Title: "Pick theme", Drill: &sub}}
	theme := []Item{
		{ID: "dark", Title: "Dark", Current: true},
		{ID: "light", Title: "Light"},
	}
	m := New(loadFunc(map[string][]Item{"root": root, "theme": theme}), WithStyles(pinnedStyles()))
	m.SetSize(60, 20)
	m = open(t, m, Level{ID: "root", Title: "Actions"})

	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg("enter"))
	for _, msg := range flatten(cmd) {
		if im, ok := msg.(ItemsMsg); ok {
			m, _ = m.Update(im)
		}
	}
	golden.Assert(t, "picker_drill", m.View())
}

func TestPicker_BorderAndBackgroundCoverBox(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "new", Title: "New session", Detail: "ctrl+n"}}
	m := New(loadFunc(map[string][]Item{"root": items}), WithStyles(pinnedStyles()))
	m.SetSize(60, 20)
	m = open(t, m, Level{ID: "root", Title: "Actions"})
	out := m.View()

	w, h := lipgloss.Width(out), lipgloss.Height(out)
	if w != m.boxW || h != m.boxH {
		t.Fatalf("View size = %dx%d, want %dx%d", w, h, m.boxW, m.boxH)
	}
	canvas := lipgloss.NewCanvas(w, h)
	canvas.Compose(lipgloss.NewLayer(out))
	if c := canvas.CellAt(0, 0); c == nil || c.Content != "╭" {
		t.Errorf("top-left cell = %+v, want ╭", c)
	}
	if c := canvas.CellAt(w-1, h-1); c == nil || c.Content != "╯" {
		t.Errorf("bottom-right cell = %+v, want ╯", c)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if c := canvas.CellAt(x, y); c != nil && c.Width > 0 && c.Style.Bg == nil {
				t.Fatalf("cell (%d,%d) %q has no background", x, y, c.Content)
			}
		}
	}
}

func TestPicker_InputLevelBackspaceOnEmptyStays(t *testing.T) {
	t.Parallel()
	sub := Level{ID: "rename", Title: "Rename", Input: true, Initial: "ab"}
	root := []Item{{ID: "rename", Title: "Rename", Drill: &sub}}
	m := New(loadFunc(map[string][]Item{"root": root}))
	m.SetSize(100, 40)
	m = open(t, m, Level{ID: "root", Title: "Root"})
	m, _ = m.Update(keyMsg("enter"))
	if topLevel(m).ID != "rename" {
		t.Fatalf("top level = %s, want rename", topLevel(m).ID)
	}
	for range 4 { // two clear the text, two more on an empty input
		m, _ = m.Update(keyMsg("backspace"))
	}
	if topLevel(m).ID != "rename" || len(m.stack) != 2 {
		t.Fatalf("after backspace on empty input: top = %s, depth %d; want to stay on rename", topLevel(m).ID, len(m.stack))
	}
}

func TestPicker_PinnedLeadsBeforeRecent(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "new", Title: "New", Group: "Session"},
		{ID: "open", Title: "Open", Group: "Session", Pinned: true},
		{ID: "t1", Title: "t1", Group: "Tools"},
	}
	m := New(loadFunc(map[string][]Item{"root": items}))
	m.SetSize(100, 40)
	m.SetRecent([]string{"t1", "open"})
	m = open(t, m, Level{ID: "root", Title: "Root", Actions: true})

	got := itemIDs(m)
	want := []string{"open", "t1", "new", "t1"}
	if !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v (pinned first, not repeated)", got, want)
	}
	if rs := rows(m); rs[0].kind != rowItem || rs[1].kind != rowGap || rs[2].header != "Recent" {
		t.Fatalf("rows = %+v, want the pinned item, a gap, then Recent", rs[:3])
	}
	if it, _ := m.stack[0].current(); it.ID != "open" {
		t.Errorf("cursor on %q, want the pinned item", it.ID)
	}
}
