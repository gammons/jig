// Package prompt is jig's growing message box: a 1-8 line textarea with
// enter-to-submit, shift+enter/alt+enter for a literal newline, an
// external-$EDITOR round trip, prompt history walked with ↑/↓, and paste
// chips that collapse a large paste into a short "[pasted N chars]" token.
package prompt

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// minContentLines and maxContentLines are the exact spec numbers: the
// prompt grows 1-8 lines.
const (
	minContentLines = 1
	maxContentLines = 8
)

// EditFunc opens $EDITOR (or similar) over text. It must yield an
// EditedMsg via its returned Cmd; the widget never does I/O itself.
type EditFunc func(text string) tea.Cmd

// EditedMsg carries the result of an EditFunc's editor round trip.
type EditedMsg struct {
	Text string
	Err  error
}

// SubmitMsg is emitted on enter over non-blank text, with any paste chips
// expanded to the full text they stand for.
type SubmitMsg struct{ Text string }

// MentionMsg is emitted when '@' is typed. Nothing is inserted into the
// textarea; the caller (App) drives the file picker itself.
type MentionMsg struct{}

// Styles holds the prompt's look.
type Styles struct {
	Border      lipgloss.Style // border line color
	Title       lipgloss.Style // border title text (e.g. "⏳ queued")
	Text        lipgloss.Style // typed/pasted text
	Placeholder lipgloss.Style // placeholder text
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Border:      lipgloss.NewStyle(),
		Title:       lipgloss.NewStyle().Bold(true),
		Text:        lipgloss.NewStyle(),
		Placeholder: lipgloss.NewStyle().Faint(true),
	}
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// WithKeyMap sets the key bindings.
func WithKeyMap(km KeyMap) Option { return func(m *Model) { m.keys = km } }

// Model is the prompt. Use the Model most recently returned by Update; it
// is not safe for concurrent use.
type Model struct {
	ta     textarea.Model
	edit   EditFunc
	keys   KeyMap
	styles Styles
	agent  string
	queued bool
	width  int
	chips  chips
	hist   historyState
}

// New builds a Model that opens $EDITOR through edit on ctrl+e. edit may
// be nil, in which case ctrl+e is a no-op.
func New(edit EditFunc, opts ...Option) Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.DynamicHeight = true
	ta.MinHeight = minContentLines
	// bubbles/textarea's MaxContentHeight budget check double-counts the
	// first inserted fragment when a single "\n" grows the content by one
	// line at a time (the common case: the user pressing shift+enter
	// repeatedly), so it stops one line short of the configured cap.
	// Setting the cap one higher compensates, so growth by typing (and by
	// a single multi-line paste) both actually reach maxContentLines.
	ta.MaxHeight = maxContentLines + 1
	ta.MaxContentHeight = maxContentLines + 1
	ta.CharLimit = 0

	m := Model{
		ta:     ta,
		edit:   edit,
		keys:   DefaultKeyMap(),
		styles: DefaultStyles(),
		chips:  newChips(),
		hist:   newHistoryState(),
	}
	for _, o := range opts {
		o(&m)
	}
	m.ta.SetStyles(taStyles(m.styles))
	m.SetAgent(m.agent)
	return m
}

// SetWidth sets the outer width, including the border.
func (m *Model) SetWidth(w int) {
	m.width = w
	m.ta.SetWidth(w - 2)
}

// Height returns the total height: the textarea's current content height
// (1-8 lines) plus the top and bottom border.
func (m Model) Height() int {
	return m.ta.Height() + 2
}

// SetAgent sets the agent name shown in the placeholder.
func (m *Model) SetAgent(name string) {
	m.agent = name
	m.ta.Placeholder = "Message " + name + "…  (ctrl+p actions · @ files)"
}

// SetQueued sets whether the border title shows "⏳ queued".
func (m *Model) SetQueued(q bool) { m.queued = q }

// SetHistory sets the prompt history, oldest first, and cancels any walk
// in progress.
func (m *Model) SetHistory(entries []string) { m.hist.setEntries(entries) }

// Insert inserts s at the cursor.
func (m *Model) Insert(s string) { m.ta.InsertString(s) }

// Value returns the textarea's text with every live paste chip expanded
// back to the text it replaced.
func (m Model) Value() string { return m.chips.expand(m.ta.Value()) }

// Reset clears the text, any paste chips, and any history walk in
// progress. The App decides when to call this; the widget never resets
// itself on submit.
func (m *Model) Reset() {
	m.ta.Reset()
	m.chips = newChips()
	m.hist.idx = -1
}

// Focus focuses the textarea.
func (m *Model) Focus() tea.Cmd { return m.ta.Focus() }

// Blur blurs the textarea.
func (m *Model) Blur() { m.ta.Blur() }

// Update handles a paste, an EditedMsg from an EditFunc round trip, and
// key presses. It is a no-op for key presses and pastes while blurred.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case EditedMsg:
		return m.handleEdited(msg)
	case tea.PasteMsg:
		if !m.ta.Focused() {
			return m, nil
		}
		return m.handlePaste(msg)
	case tea.KeyPressMsg:
		if !m.ta.Focused() {
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches a key press: '@' emits MentionMsg without
// inserting; enter submits (unless blank); shift+enter/alt+enter insert a
// literal newline; ctrl+e opens the editor; ↑/↓ on the first/last line
// walk history; backspace removes a whole chip token when the cursor
// sits right after one. Anything else is forwarded to the textarea.
func (m Model) handleKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case k.String() == "@":
		return m, func() tea.Msg { return MentionMsg{} }
	case key.Matches(k, m.keys.Submit):
		return m.submit()
	case key.Matches(k, m.keys.Newline):
		m.ta.InsertRune('\n')
		m.hist.idx = -1
		return m, nil
	case key.Matches(k, m.keys.Editor):
		if m.edit == nil {
			return m, nil
		}
		return m, m.edit(m.Value())
	case key.Matches(k, m.keys.HistoryPrev):
		if m.ta.Line() == 0 {
			return m.walkHistory(true)
		}
	case key.Matches(k, m.keys.HistoryNext):
		if m.ta.LineCount() > 0 && m.ta.Line() == m.ta.LineCount()-1 {
			return m.walkHistory(false)
		}
	case k.String() == "backspace":
		return m.handleBackspace()
	}
	m.hist.idx = -1
	ta, cmd := m.ta.Update(k)
	m.ta = ta
	return m, cmd
}

// submit emits SubmitMsg with the chip-expanded text, unless it is blank
// or whitespace-only.
func (m Model) submit() (Model, tea.Cmd) {
	text := m.Value()
	if strings.TrimSpace(text) == "" {
		return m, nil
	}
	return m, func() tea.Msg { return SubmitMsg{Text: text} }
}

// walkHistory replaces the text with the next history entry in the given
// direction (back = older), saving/restoring the draft at the ends.
func (m Model) walkHistory(back bool) (Model, tea.Cmd) {
	var text string
	var ok bool
	if back {
		text, ok = m.hist.prev(m.Value())
	} else {
		text, ok = m.hist.next()
	}
	if !ok {
		return m, nil
	}
	m.chips = newChips()
	m.ta.SetValue(text)
	return m, nil
}

// handleBackspace deletes the whole chip token when the cursor sits right
// after one, otherwise forwards a single backspace to the textarea.
func (m Model) handleBackspace() (Model, tea.Cmd) {
	if tok, ok := m.chips.suffixToken(lineBeforeCursor(m.ta)); ok {
		for range utf8.RuneCountInString(tok) {
			m.ta, _ = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
		m.chips = m.chips.withoutToken(tok)
		return m, nil
	}
	m.hist.idx = -1
	ta, cmd := m.ta.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.ta = ta
	return m, cmd
}

// handlePaste sanitizes the pasted content (dropping C0 controls other
// than \n/\t) and, over pasteChipThreshold runes, collapses it into a
// chip token instead of inserting it verbatim.
func (m Model) handlePaste(msg tea.PasteMsg) (Model, tea.Cmd) {
	text := ansi.Sanitize(msg.Content)
	n := utf8.RuneCountInString(text)
	insert := text
	if n > pasteChipThreshold {
		var tok string
		m.chips, tok = m.chips.withToken(n, text)
		insert = tok
	}
	m.ta.InsertString(insert)
	m.hist.idx = -1
	return m, nil
}

// handleEdited applies an EditFunc's result, replacing the text outright.
// The editor was handed the chip-expanded Value, so its result never
// contains chip tokens.
func (m Model) handleEdited(msg EditedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		return m, nil
	}
	m.chips = newChips()
	m.ta.SetValue(msg.Text)
	m.hist.idx = -1
	return m, nil
}

// View renders the bordered box: the top border carries the "⏳ queued"
// title when queued.
func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = m.ta.Width() + 2
	}
	b := lipgloss.RoundedBorder()
	title := ""
	if m.queued {
		title = "⏳ queued"
	}

	lines := strings.Split(m.ta.View(), "\n")
	rows := make([]string, 0, len(lines)+2)
	rows = append(rows, borderRow(w, b.TopLeft, b.Top, b.TopRight, title, m.styles.Border, m.styles.Title))
	for _, l := range lines {
		rows = append(rows, m.styles.Border.Render(b.Left)+l+m.styles.Border.Render(b.Right))
	}
	rows = append(rows, borderRow(w, b.BottomLeft, b.Bottom, b.BottomRight, "", m.styles.Border, m.styles.Title))
	return strings.Join(rows, "\n")
}

// lineBeforeCursor returns ta's current logical line, up to the cursor
// column.
func lineBeforeCursor(ta textarea.Model) string {
	lines := strings.Split(ta.Value(), "\n")
	row := ta.Line()
	if row < 0 || row >= len(lines) {
		return ""
	}
	line := []rune(lines[row])
	col := ta.Column()
	if col > len(line) {
		col = len(line)
	}
	return string(line[:col])
}

// borderRow renders one border line of width w, embedding title (if any)
// centered with one fill rune of lead padding.
func borderRow(w int, left, fill, right, title string, borderStyle, titleStyle lipgloss.Style) string {
	inner := max(0, w-2)
	var mid string
	switch title {
	case "":
		mid = borderStyle.Render(strings.Repeat(fill, inner))
	default:
		label := " " + title + " "
		lw := ansi.Width(label)
		if lw >= inner {
			mid = titleStyle.Render(ansi.Truncate(label, inner, ""))
		} else {
			lead, trail := 1, inner-lw-1
			mid = borderStyle.Render(strings.Repeat(fill, lead)) +
				titleStyle.Render(label) +
				borderStyle.Render(strings.Repeat(fill, trail))
		}
	}
	return borderStyle.Render(left) + mid + borderStyle.Render(right)
}

// taStyles builds the textarea's Styles from st, with no cursor-line
// highlight, prompt, or line-number styling (the prompt widget draws its
// own border and has none of those).
func taStyles(st Styles) textarea.Styles {
	state := textarea.StyleState{
		Base:        lipgloss.NewStyle(),
		Text:        st.Text,
		Placeholder: st.Placeholder,
		CursorLine:  lipgloss.NewStyle(),
		EndOfBuffer: lipgloss.NewStyle(),
		Prompt:      lipgloss.NewStyle(),
		Selection:   lipgloss.NewStyle(),
	}
	return textarea.Styles{Focused: state, Blurred: state, Cursor: textarea.CursorStyle{Blink: false}}
}
