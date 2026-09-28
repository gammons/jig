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

// EditedMsg carries the result of an EditFunc's editor round trip. The
// widget itself ignores Err: it only ever replaces the text when Err is
// nil, leaving the text untouched otherwise. Surfacing the error (e.g. as
// a status line) is the App's job, not this widget's.
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
	// MaxHeight bounds only the *visible* viewport height: past it,
	// DynamicHeight's recalculateHeight clamps the rendered height to
	// MaxHeight and scrolls, per spec §7.3 ("grows to 8 lines, then
	// scrolls"). MaxContentHeight is deliberately left at its zero value:
	// when set (>0) it makes bubbles/textarea's insertRunesFromUserInput
	// silently drop (or, for a multi-line insert, truncate) any content
	// that would push total visual lines past it — content must never be
	// dropped, so that budget check has to stay disabled. The two knobs
	// are independent: content grows without limit (up to bubbles/
	// textarea's own hard 10,000-logical-line cap) while only the
	// viewport is capped at maxContentLines.
	ta.MaxHeight = maxContentLines
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
	m.syncPlaceholder()
}

// Height returns the total height: the textarea's current content height
// (1-8 lines) plus the top and bottom border.
func (m Model) Height() int {
	return m.ta.Height() + 2
}

// SetAgent sets the agent name shown in the placeholder.
func (m *Model) SetAgent(name string) {
	m.agent = name
	m.syncPlaceholder()
}

// syncPlaceholder rebuilds the textarea's placeholder text from the
// current agent name, truncated to one line with "…" when it doesn't fit
// the current width.
//
// bubbles/textarea's own placeholder rendering word-wraps the full
// placeholder and shows as many wrapped lines as the widget's current
// height — which, at the widget's minimum (1 content line, the common
// case before the user has typed anything), silently cuts off everything
// past the first wrapped line with no ellipsis. Pre-truncating here (with
// ansi.Truncate, which does add "…") keeps the placeholder on one line
// and makes the cut visible, at any width, including the full width-80
// case where it comfortably fits untouched.
func (m *Model) syncPlaceholder() {
	full := "Message " + m.agent + "…  (ctrl+p actions · @ files)"
	m.ta.Placeholder = ansi.Truncate(full, max(1, m.ta.Width()), "…")
}

// SetQueued sets whether the border title shows "⏳ queued".
func (m *Model) SetQueued(q bool) { m.queued = q }

// SetHistory sets the prompt history, oldest first, and cancels any walk
// in progress.
func (m *Model) SetHistory(entries []string) { m.hist.setEntries(entries) }

// Insert inserts s at the cursor, first moving the cursor to the end of
// any chip token it's inside so the insertion never splits one apart.
func (m *Model) Insert(s string) {
	m.ta = exitChipInterior(m.ta, m.chips)
	m.ta.InsertString(s)
	m.ta = reposition(m.ta)
}

// Value returns the textarea's text with every live paste chip expanded
// back to the text it replaced. Any chip marker runes left over from a
// token an edit damaged (see chips.go's stripMarkers) are stripped, so
// Value, SubmitMsg (built from Value), and history (built from submitted
// Value text) never contain them.
func (m Model) Value() string { return stripMarkers(m.chips.expand(m.ta.Value())) }

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
// walk history; backspace/delete remove a whole chip token when the
// cursor touches one. Anything else is forwarded to the textarea, first
// moving the cursor out of a chip it's inside if the key types text (so
// typing inside a chip appends after it instead of splitting it apart —
// a plain cursor move, by contrast, is left alone).
func (m Model) handleKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case k.String() == "@":
		return m, func() tea.Msg { return MentionMsg{} }
	case key.Matches(k, m.keys.Submit):
		return m.submit()
	case key.Matches(k, m.keys.Newline):
		m.ta = exitChipInterior(m.ta, m.chips)
		m.ta.InsertRune('\n')
		m.ta = reposition(m.ta)
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
	case k.String() == "delete":
		return m.handleDelete()
	}
	m.hist.idx = -1
	if k.Text != "" {
		m.ta = exitChipInterior(m.ta, m.chips)
	}
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
	m.ta = reposition(m.ta)
	return m, nil
}

// handleBackspace deletes the whole chip token when the cursor touches
// one — anywhere from just past its start through its end, i.e.
// wherever the character backspace would otherwise delete falls inside
// the token — otherwise forwards a single backspace to the textarea.
func (m Model) handleBackspace() (Model, tea.Cmd) {
	line, col := currentLineRunes(m.ta), m.ta.Column()
	if tok, start, end, ok := m.chips.spanContaining(line, col); ok && col > start {
		ta, cs, cmd := deleteChip(m.ta, m.chips, tok, end)
		m.ta, m.chips, m.hist.idx = ta, cs, -1
		return m, cmd
	}
	m.hist.idx = -1
	ta, cmd := m.ta.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.ta = ta
	return m, cmd
}

// handleDelete deletes the whole chip token when the cursor touches one
// — anywhere from its start through just before its end, i.e. wherever
// the character forward-delete would otherwise delete falls inside the
// token — otherwise forwards a single forward-delete to the textarea.
func (m Model) handleDelete() (Model, tea.Cmd) {
	line, col := currentLineRunes(m.ta), m.ta.Column()
	if tok, _, end, ok := m.chips.spanContaining(line, col); ok && col < end {
		ta, cs, cmd := deleteChip(m.ta, m.chips, tok, end)
		m.ta, m.chips, m.hist.idx = ta, cs, -1
		return m, cmd
	}
	m.hist.idx = -1
	ta, cmd := m.ta.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	m.ta = ta
	return m, cmd
}

// handlePaste sanitizes the pasted content (dropping C0 controls other
// than \n/\t) and, over pasteChipThreshold runes, collapses it into a
// chip token instead of inserting it verbatim. It's routed through
// bubbles/textarea's own tea.PasteMsg handling (rather than InsertString
// directly) so the usual height/scroll bookkeeping runs, after first
// moving the cursor out of any chip it's inside, so a paste inside an
// existing chip appends after it rather than splitting it apart.
func (m Model) handlePaste(msg tea.PasteMsg) (Model, tea.Cmd) {
	text := ansi.Sanitize(msg.Content)
	n := utf8.RuneCountInString(text)
	insert := text
	if n > pasteChipThreshold {
		var tok string
		m.chips, tok = m.chips.withToken(n, text)
		insert = tok
	}
	m.ta = exitChipInterior(m.ta, m.chips)
	ta, cmd := m.ta.Update(tea.PasteMsg{Content: insert})
	m.ta = ta
	m.hist.idx = -1
	return m, cmd
}

// handleEdited applies an EditFunc's result, replacing the text outright,
// unless it failed (see EditedMsg's doc: a non-nil Err just leaves the
// text as it was — surfacing it is the App's job). The editor was handed
// the chip-expanded Value, so its result never contains chip tokens.
func (m Model) handleEdited(msg EditedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		return m, nil
	}
	m.chips = newChips()
	m.ta.SetValue(msg.Text)
	m.ta = reposition(m.ta)
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

// currentLineRunes returns ta's current logical line as runes.
func currentLineRunes(ta textarea.Model) []rune {
	lines := strings.Split(ta.Value(), "\n")
	row := ta.Line()
	if row < 0 || row >= len(lines) {
		return nil
	}
	return []rune(lines[row])
}

// exitChipInterior moves ta's cursor to the end of the chip token it's
// strictly inside (touching neither edge), so the next edit appends
// after the token instead of splitting it apart. It's a no-op everywhere
// else, including when the cursor merely touches a token's edge — typing
// there is already safe, since it can't split any of the token's runes.
func exitChipInterior(ta textarea.Model, cs chips) textarea.Model {
	line, col := currentLineRunes(ta), ta.Column()
	if _, start, end, ok := cs.spanContaining(line, col); ok && col > start && col < end {
		ta.SetCursorColumn(end)
	}
	return ta
}

// deleteChip removes tok in full: it moves the cursor to end (the
// token's trailing edge) and backspaces its whole rune length, staying
// inside bubbles/textarea's own key handling so height/scroll
// bookkeeping happens exactly as it would for any other backspace.
func deleteChip(ta textarea.Model, cs chips, tok string, end int) (textarea.Model, chips, tea.Cmd) {
	ta.SetCursorColumn(end)
	var cmd tea.Cmd
	for range utf8.RuneCountInString(tok) {
		ta, cmd = ta.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	return ta, cs.withoutToken(tok), cmd
}

// reposition round-trips a message that matches none of bubbles/
// textarea's own key or paste handling, purely to run the
// recalculateHeight/repositionView bookkeeping every Update call
// performs — including scrolling the viewport to keep the cursor
// visible, which InsertRune/SetValue (called directly, bypassing Update,
// so this widget can insert a literal newline past bubbles/textarea's
// own atContentLimit-guarded enter handling, or replace the value
// outright for history/$EDITOR) recompute the height for but don't do.
func reposition(ta textarea.Model) textarea.Model {
	ta, _ = ta.Update(tea.KeyPressMsg{})
	return ta
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
