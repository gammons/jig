package coderender

import (
	"strings"

	"charm.land/lipgloss/v2"
	udiff "github.com/aymanbagabas/go-udiff"
)

// diffOldLabel and diffNewLabel are the file labels go-udiff puts on a
// unified diff's "---"/"+++" header lines. Diff and DiffText both drop or
// ignore those two lines, so the exact label text never reaches a
// caller; it only has to be stable between the two functions.
const (
	diffOldLabel = "before"
	diffNewLabel = "after"
)

// Diff returns a unified diff of before to after, with context lines of
// surrounding unchanged context, as styled lines: an st.Hunk-styled "@@"
// header per hunk, then one line per "+" (added), "-" (removed), and " "
// (context) line, each with its content highlighted for path (see
// Highlight) and its background set to st.Added, st.Removed, or left
// unstyled for context lines. hunks is the number of hunks found. A
// before/after pair with no differences returns (nil, 0).
func Diff(path, before, after string, context int, st Styles) ([]string, int) {
	edits := udiff.Lines(before, after)
	if len(edits) == 0 {
		return nil, 0
	}
	text, err := udiff.ToUnified(diffOldLabel, diffNewLabel, before, edits, context)
	if err != nil || text == "" {
		return nil, 0
	}

	body := unifiedBody(text)
	lines := make([]string, 0, len(body))
	hunks := 0
	for _, l := range body {
		switch {
		case strings.HasPrefix(l, "@@"):
			hunks++
			lines = append(lines, st.Hunk.Render(l))
		case strings.HasPrefix(l, "+"):
			lines = append(lines, diffLine(path, "+", l[1:], st, st.Added))
		case strings.HasPrefix(l, "-"):
			lines = append(lines, diffLine(path, "-", l[1:], st, st.Removed))
		case strings.HasPrefix(l, " "):
			lines = append(lines, diffLine(path, " ", l[1:], st, st.Plain))
		default:
			// e.g. "\ No newline at end of file".
			lines = append(lines, st.Plain.Render(l))
		}
	}
	return lines, hunks
}

// DiffText returns the plain (unstyled) unified diff text of before to
// after, with context lines of surrounding context, for callers that
// need to yank or otherwise reuse the raw diff rather than render it.
func DiffText(before, after string, context int) string {
	edits := udiff.Lines(before, after)
	if len(edits) == 0 {
		return ""
	}
	text, err := udiff.ToUnified(diffOldLabel, diffNewLabel, before, edits, context)
	if err != nil {
		return ""
	}
	return text
}

// unifiedBody splits a udiff.ToUnified result into its hunk lines,
// dropping the trailing empty element strings.Split leaves for the final
// "\n" and the leading "--- .../+++ ..." file-label lines.
func unifiedBody(text string) []string {
	raw := strings.Split(text, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	if len(raw) >= 2 && strings.HasPrefix(raw[0], "--- ") && strings.HasPrefix(raw[1], "+++ ") {
		raw = raw[2:]
	}
	return raw
}

// diffLine renders one unified-diff content line: marker (one of "+",
// "-", " ") styled with bg, followed by content highlighted for path with
// bg's background color merged into every token's style.
func diffLine(path, marker, content string, st Styles, bg lipgloss.Style) string {
	prefix := bg.Render(marker)
	if content == "" {
		return prefix
	}
	withBG := func(base lipgloss.Style) lipgloss.Style {
		return base.Background(bg.GetBackground())
	}
	return prefix + highlightLines(path, content, st, withBG)[0]
}
