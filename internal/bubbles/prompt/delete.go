package prompt

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// deleteKind distinguishes the three shapes of mutation handleDeleteKey
// covers, each with its own rule for when it would partially damage a
// chip it doesn't fully contain.
type deleteKind int

const (
	deleteBack    deleteKind = iota // bubbles/textarea's DeleteCharacterBackward (backspace, ctrl+h): deletes the rune just before the cursor
	deleteForward                   // bubbles/textarea's DeleteCharacterForward (delete, ctrl+d): deletes the rune at the cursor
	deleteWhole                     // every other key capable of mutating the buffer (see classify): deletes or rearranges a range this package hasn't classified more precisely, so any touch is unsafe
)

// handleDeleteKey handles bubbles/textarea's two single-character
// deletion bindings precisely, and treats every other mutation-capable
// key (deleteWhole — see classify) conservatively. Each mutates a range
// that starts or ends at the cursor; when the cursor touches a live chip
// token in a way that would land the mutation inside it, this deletes
// the whole token atomically instead of forwarding k to the textarea and
// risking a partial/damaged token — deleteWhole always substitutes a
// whole-chip delete on any touch, since a word/line deletion (or a
// transpose, or a case change, ...) can start its edit anywhere inside
// the token and there's no single boundary rule for it, at the cost of
// not also touching whatever lies beyond the chip on that keystroke (the
// user can press the key again). Otherwise k is forwarded to the
// textarea as usual.
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

// mutationClass classifies a key reaching handleKey's default forward
// path, so it can be guarded appropriately.
type mutationClass int

const (
	classSafe    mutationClass = iota // pure navigation/selection/copy (safeKeys): never mutates the buffer
	classInsert                       // types text (k.Text != ""): guarded by exitChipInterior, not handleDeleteKey
	classBack                         // bubbles/textarea's DeleteCharacterBackward
	classForward                      // bubbles/textarea's DeleteCharacterForward
	classOther                        // every other key: conservatively treated as capable of mutating the buffer anywhere from the cursor
)

// classify determines k's mutationClass.
//
// The two single-character deletions are matched against bubbles/
// textarea's own DefaultKeyMap bindings with key.Matches, not a
// hand-typed key string, so every alias bubbles/textarea recognizes for
// them — "ctrl+h" for backspace, "ctrl+d" for forward-delete, not just
// their primary key — is covered.
//
// safeKeys covers the bindings that only move the cursor or the
// selection, never the buffer. Anything that's neither one of those two
// deletions, in safeKeys, nor typed text is classOther: a catch-all, not
// an enumerated list of "risky" bindings, so a bubbles/textarea binding
// this package has never named (its word/line deletion bindings,
// transpose, case changes, or a future addition) is still protected by
// default instead of needing to be added to a list first — which is
// exactly the gap that let ctrl+h/ctrl+d through when this package
// instead kept a list of specifically-risky keys.
func classify(k tea.KeyPressMsg) mutationClass {
	tm := textarea.DefaultKeyMap()
	switch {
	case key.Matches(k, tm.DeleteCharacterBackward):
		return classBack
	case key.Matches(k, tm.DeleteCharacterForward):
		return classForward
	case key.Matches(k, safeKeys(tm)...):
		return classSafe
	case k.Text != "":
		return classInsert
	}
	return classOther
}

// safeKeys returns tm's bindings for operations that only move the
// cursor or the selection, never the buffer's content.
func safeKeys(tm textarea.KeyMap) []key.Binding {
	return []key.Binding{
		tm.CharacterForward, tm.CharacterBackward,
		tm.WordForward, tm.WordBackward,
		tm.LineNext, tm.LinePrevious,
		tm.LineStart, tm.LineEnd,
		tm.PageUp, tm.PageDown,
		tm.InputBegin, tm.InputEnd,
		tm.SelectCharacterForward, tm.SelectCharacterBackward,
		tm.SelectWordForward, tm.SelectWordBackward,
		tm.SelectLineUp, tm.SelectLineDown,
		tm.SelectAll, tm.CopySelection,
	}
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
//
// Note for anyone tempted to instead try applying an edit speculatively
// and reverting m.ta if it turns out to have damaged a chip: don't.
// textarea.Model holds its text as [][]rune; Go's shallow struct copy
// (as happens implicitly whenever a textarea.Model is passed by value,
// e.g. to a function or a closure) shares that backing array, and
// bubbles/textarea's own key handlers mutate rows in place with plain
// index assignment (m.value[m.row] = ...), not by allocating a new
// backing array. Calling Update on a copy of m.ta can therefore mutate
// m.ta's own data out from under it, making "revert to the pre-edit
// m.ta" unsound — confirmed the hard way, by an earlier version of this
// file that tried exactly that and silently corrupted an unrelated chip
// two pastes away from the one actually being edited.
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
