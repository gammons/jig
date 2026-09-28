// Package picker is jig's ctrl+p picker: a fuzzy-filtered, drill-down list
// with groups, a recent-actions group, multi-mark, a text-entry level, and
// an optional preview callback. jig has no slash commands; every action
// goes through this widget.
package picker

import (
	"image/color"
	"slices"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Level describes one screen of the picker's drill-down stack.
type Level struct {
	ID, Title string
	Arg       string // opaque payload for the loader (e.g. a provider ID)
	Multi     bool   // tab marks items
	Input     bool   // single-line text entry instead of a list (rename)
	Initial   string // Input levels: prefilled text
	Actions   bool   // root action list: Recent group + recency tie-break
}

// Item is one row of a Level's list.
type Item struct {
	ID, Title, Detail, Group string
	Disabled, Current        bool   // Current shows ●; Disabled is dimmed and cannot be chosen
	Drill                    *Level // enter opens this level
}

// LoadFunc loads a Level's items. It must yield an ItemsMsg (via its
// returned Cmd); the widget never does I/O itself.
type LoadFunc func(level Level) tea.Cmd

// PreviewFunc is called whenever the highlighted item changes on a level
// where WithPreview is set. Its Cmd's message type is the caller's own.
type PreviewFunc func(level Level, item Item) tea.Cmd

// ItemsMsg carries a Level's loaded items. One for a level that is not on
// top of the stack is ignored (stale).
type ItemsMsg struct {
	Level string
	Items []Item
	Err   error
}

// ChosenMsg is emitted on enter over a non-drill item: the marked items on
// a Multi level (or, absent marks, just the current one), else the
// current item alone.
type ChosenMsg struct {
	Level Level
	Items []Item
}

// InputMsg is emitted on enter on an Input level.
type InputMsg struct {
	Level Level
	Text  string
}

// ClosedMsg is emitted on esc.
type ClosedMsg struct {
	Level Level
}

// Option configures a Model built by New.
type Option func(*Model)

// WithPreview sets the preview callback.
func WithPreview(p PreviewFunc) Option { return func(m *Model) { m.preview = p } }

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// WithKeyMap sets the key bindings.
func WithKeyMap(km KeyMap) Option { return func(m *Model) { m.keys = km } }

// frame is one level of the stack: its loaded items, the current
// display rows (grouped or filtered), the selection, and (for every
// level) the text input used either to filter or, on an Input level, to
// collect the entered text.
type frame struct {
	level   Level
	items   []Item
	rows    []row
	cursor  int // index into rows; -1 when nothing is selectable
	marks   map[string]bool
	input   textinput.Model
	loading bool
	err     error
}

// Model is the picker. Use the Model most recently returned by Update; it
// is not safe for concurrent use.
type Model struct {
	load    LoadFunc
	preview PreviewFunc
	styles  Styles
	keys    KeyMap
	boxW    int
	boxH    int
	stack   []frame
	recent  []string
	open    bool

	// previewed is "<levelID>\x00<itemID>" of the last item previewed, so
	// previewCmd fires only when the highlighted item actually changes.
	previewed string
}

// New builds a closed picker that loads each level's items with load.
func New(load LoadFunc, opts ...Option) Model {
	m := Model{
		load:   load,
		styles: DefaultStyles(),
		keys:   DefaultKeyMap(),
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// IsOpen reports whether the picker has an open level.
func (m Model) IsOpen() bool { return m.open }

// SetRecent sets the most-recent-first IDs used for the Recent group and
// the filter tie-break on Actions levels.
func (m *Model) SetRecent(ids []string) { m.recent = slices.Clone(ids) }

// SetSize sets the outer terminal size; the box clamps to
// width min(80, 70% termW), height <= 60% termH.
func (m *Model) SetSize(termW, termH int) {
	w := min(80, termW*7/10)
	h := termH * 6 / 10
	m.boxW, m.boxH = w, h
	for i := range m.stack {
		m.stack[i].input.SetWidth(inputWidth(w))
	}
}

// Open resets the stack to a single root level and starts loading it.
func (m *Model) Open(root Level) tea.Cmd {
	m.stack = nil
	m.open = true
	m.previewed = ""
	return m.push(root)
}

// Close discards the stack without emitting a message.
func (m *Model) Close() {
	m.stack = nil
	m.open = false
}

// push appends level as a new top frame and returns its load/focus Cmd.
func (m *Model) push(level Level) tea.Cmd {
	f := newFrame(level, m.boxW)
	m.stack = append(m.stack, f)
	cmds := []tea.Cmd{m.stack[len(m.stack)-1].input.Focus()}
	if !level.Input && m.load != nil {
		m.stack[len(m.stack)-1].loading = true
		cmds = append(cmds, m.load(level))
	}
	return tea.Batch(cmds...)
}

func inputWidth(boxW int) int { return max(1, boxW-4) }

func newFrame(level Level, boxW int) frame {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.SetWidth(inputWidth(boxW))
	if level.Input {
		ti.SetValue(level.Initial)
		ti.CursorEnd()
	} else {
		ti.Placeholder = "Filter…"
	}
	f := frame{level: level, input: ti, cursor: -1}
	if level.Multi {
		f.marks = map[string]bool{}
	}
	return f
}

// current returns the item at the cursor, if any.
func (f frame) current() (Item, bool) {
	if f.cursor < 0 || f.cursor >= len(f.rows) {
		return Item{}, false
	}
	if r := f.rows[f.cursor]; r.kind == rowItem {
		return r.item, true
	}
	return Item{}, false
}

// rebuild recomputes rows from items and the input's current value,
// keeping the selection on the same item ID when it still exists.
func (f *frame) rebuild(recent []string) {
	prevID, hadSel := f.current()
	query := f.input.Value()
	if query == "" {
		f.rows = groupedRows(f.level, f.items, recent)
	} else {
		f.rows = filteredRows(f.level, f.items, query, recent)
	}
	f.cursor = firstItemRow(f.rows)
	if hadSel {
		for i, r := range f.rows {
			if r.kind == rowItem && r.item.ID == prevID.ID {
				f.cursor = i
				break
			}
		}
	}
}

// Update handles ItemsMsg and key presses. It is a no-op while closed.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.open {
		return m, nil
	}
	switch msg := msg.(type) {
	case ItemsMsg:
		return m.onItems(msg)
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onItems(msg ItemsMsg) (Model, tea.Cmd) {
	if len(m.stack) == 0 {
		return m, nil
	}
	top := len(m.stack) - 1
	if m.stack[top].level.ID != msg.Level {
		return m, nil // stale: not the top level
	}
	stack := slices.Clone(m.stack)
	f := stack[top]
	f.items, f.err, f.loading = msg.Items, msg.Err, false
	f.rebuild(m.recent)
	stack[top] = f
	m.stack = stack
	return m, m.previewCmd()
}

func (m Model) onKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	if len(m.stack) == 0 {
		return m, nil
	}
	top := m.stack[len(m.stack)-1]
	switch {
	case k.String() == "esc":
		lvl := top.level
		m.Close()
		return m, closedCmd(lvl)
	case top.level.Input:
		return m.onInputKey(k, top)
	case key.Matches(k, m.keys.Down):
		return m.moveCursor(1)
	case key.Matches(k, m.keys.Up):
		return m.moveCursor(-1)
	case k.String() == "tab" && top.level.Multi:
		return m.toggleMark(), nil
	case k.String() == "enter":
		return m.choose()
	case k.String() == "backspace" && top.input.Value() == "":
		return m.pop()
	default:
		return m.editQuery(k)
	}
}

func (m Model) onInputKey(k tea.KeyPressMsg, top frame) (Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		text, lvl := top.input.Value(), top.level
		m.Close()
		return m, inputCmd(lvl, text)
	case "backspace":
		if top.input.Value() == "" && len(m.stack) > 1 {
			return m.pop()
		}
	}
	ti, cmd := top.input.Update(k)
	stack := slices.Clone(m.stack)
	f := stack[len(stack)-1]
	f.input = ti
	stack[len(stack)-1] = f
	m.stack = stack
	return m, cmd
}

func (m Model) editQuery(k tea.KeyPressMsg) (Model, tea.Cmd) {
	top := m.stack[len(m.stack)-1]
	prev := top.input.Value()
	ti, cmd := top.input.Update(k)
	stack := slices.Clone(m.stack)
	f := stack[len(stack)-1]
	f.input = ti
	if ti.Value() != prev {
		f.rebuild(m.recent)
	}
	stack[len(stack)-1] = f
	m.stack = stack
	return m, tea.Batch(cmd, m.previewCmd())
}

func (m Model) moveCursor(dir int) (Model, tea.Cmd) {
	stack := slices.Clone(m.stack)
	top := len(stack) - 1
	f := stack[top]
	f.cursor = moveRow(f.rows, f.cursor, dir)
	stack[top] = f
	m.stack = stack
	return m, m.previewCmd()
}

func (m Model) toggleMark() Model {
	stack := slices.Clone(m.stack)
	top := len(stack) - 1
	f := stack[top]
	if it, ok := f.current(); ok && !it.Disabled {
		marks := map[string]bool{}
		for id, v := range f.marks {
			marks[id] = v
		}
		if marks[it.ID] {
			delete(marks, it.ID)
		} else {
			marks[it.ID] = true
		}
		f.marks = marks
	}
	stack[top] = f
	m.stack = stack
	return m
}

func (m Model) choose() (Model, tea.Cmd) {
	top := m.stack[len(m.stack)-1]

	// A Multi level with marks chooses the marks regardless of what the
	// cursor currently rests on.
	if top.level.Multi && len(top.marks) > 0 {
		var chosen []Item
		for _, cand := range top.items {
			if top.marks[cand.ID] {
				chosen = append(chosen, cand)
			}
		}
		lvl := top.level
		m.Close()
		return m, chosenCmd(lvl, chosen)
	}

	it, ok := top.current()
	if !ok || it.Disabled {
		return m, nil
	}
	if it.Drill != nil {
		return m, m.push(*it.Drill)
	}
	lvl := top.level
	m.Close()
	return m, chosenCmd(lvl, []Item{it})
}

func (m Model) pop() (Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, nil
	}
	m.stack = m.stack[:len(m.stack)-1]
	return m, m.previewCmd()
}

// previewCmd calls preview when the top level's highlighted item changed
// since the last call, returning nil otherwise (or when there is no
// PreviewFunc, or the top level is an Input level).
func (m *Model) previewCmd() tea.Cmd {
	if m.preview == nil || len(m.stack) == 0 {
		return nil
	}
	top := m.stack[len(m.stack)-1]
	if top.level.Input {
		return nil
	}
	it, ok := top.current()
	pkey := ""
	if ok {
		pkey = top.level.ID + "\x00" + it.ID
	}
	if pkey == m.previewed {
		return nil
	}
	m.previewed = pkey
	if !ok {
		return nil
	}
	return m.preview(top.level, it)
}

func chosenCmd(lvl Level, items []Item) tea.Cmd {
	return func() tea.Msg { return ChosenMsg{Level: lvl, Items: items} }
}

func inputCmd(lvl Level, text string) tea.Cmd {
	return func() tea.Msg { return InputMsg{Level: lvl, Text: text} }
}

func closedCmd(lvl Level) tea.Cmd {
	return func() tea.Msg { return ClosedMsg{Level: lvl} }
}

// withMatch overrides base's foreground/bold/underline with match's,
// where match sets them, leaving everything else (including base's own
// foreground when match doesn't set one) untouched.
func withMatch(base, match lipgloss.Style) lipgloss.Style {
	out := base
	if fg := match.GetForeground(); !isNoColor(fg) {
		out = out.Foreground(fg)
	}
	if match.GetBold() {
		out = out.Bold(true)
	}
	if match.GetUnderline() {
		out = out.Underline(true)
	}
	return out
}

func isNoColor(c color.Color) bool {
	_, none := c.(lipgloss.NoColor)
	return c == nil || none
}
