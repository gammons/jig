package prompt

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// deleteKind distinguishes the three shapes of deletion handleDeleteKey
// covers, each with its own rule for when it would partially delete a
// chip it doesn't fully contain.
type deleteKind int

const (
	deleteBack    deleteKind = iota // backspace: deletes the rune just before the cursor
	deleteForward                   // forward-delete: deletes the rune at the cursor
	deleteWhole                     // bubbles/textarea's word/line deletion bindings: deletes a whole range starting or ending at the cursor, in either direction
)

// handleDeleteKey handles backspace, forward-delete, and bubbles/
// textarea's own word/line deletion bindings (ctrl+w, ctrl+k, ctrl+u,
// alt+backspace, ...). Each of these deletes a range that starts or ends
// exactly at the cursor; when the cursor touches a live chip token in a
// way that would land the deletion inside it, this deletes the whole
// token atomically instead of forwarding k to the textarea and risking a
// partial/damaged token — deleteWhole always substitutes a whole-chip
// delete on any touch, since a word/line deletion's range can start
// anywhere in the token and there's no single boundary rule for it, at
// the cost of not also touching whatever lies beyond the chip on that
// keystroke (the user can press the key again). Otherwise k is forwarded
// to the textarea as usual.
func (m Model) handleDeleteKey(k tea.KeyPressMsg, kind deleteKind) (Model, tea.Cmd) {
	line, col := currentLineRunes(m.ta), m.ta.Column()
	tok, start, end, ok := m.chips.spanContaining(line, col)
	touching := ok && (kind == deleteWhole || (kind == deleteBack && col > start) || (kind == deleteForward && col < end))
	if touching {
		ta, cs, cmd := deleteChip(m.ta, m.chips, tok, end)
		m.ta, m.chips, m.hist.idx = ta, cs, -1
		return m, cmd
	}
	m.hist.idx = -1
	ta, cmd := m.ta.Update(k)
	m.ta = ta
	return m, cmd
}

// isRiskyDeleteKey reports whether k is one of bubbles/textarea's own
// multi-character deletion bindings (DeleteWordBackward/Forward,
// DeleteAfterCursor, DeleteBeforeCursor — every deletion binding in its
// DefaultKeyMap besides the single-character backspace/forward-delete
// handleKey already matches directly).
func isRiskyDeleteKey(k tea.KeyPressMsg) bool {
	switch k.String() {
	case "ctrl+w", "alt+backspace", "ctrl+backspace", // DeleteWordBackward
		"alt+delete", "alt+d", "ctrl+delete", // DeleteWordForward
		"ctrl+k", // DeleteAfterCursor
		"ctrl+u": // DeleteBeforeCursor
		return true
	}
	return false
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
// Verified load-bearing (not a no-op) by
// TestPrompt_NeverDropsPastHeightCap, which checks the scroll offset
// immediately after growing the content — before any subsequent key
// press's own Update call could reposition the viewport and mask its
// absence.
func reposition(ta textarea.Model) textarea.Model {
	ta, _ = ta.Update(tea.KeyPressMsg{})
	return ta
}
