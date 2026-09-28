package prompt

import (
	"strconv"
	"strings"
)

// pasteChipThreshold is the exact spec number: a paste over this many
// (sanitized) runes collapses into a chip instead of being inserted
// verbatim.
const pasteChipThreshold = 500

// chipMarkerLen is the number of invisible runes appended after a chip's
// visible "[pasted N chars]" text.
//
// Two chips can render an identical token — the same N, or even text the
// user happens to type by hand — so the visible token text alone can't
// identify a chip. Every chip token therefore gets a marker of
// chipMarkerLen Unicode variation selectors (U+FE00-U+FE09, one per
// decimal digit of the chip's sequence number). Variation selectors are
// zero-width combining marks: bubbles/textarea measures and wraps with
// uniseg.StringWidth (a Unicode-width-aware function that assigns them no
// cell width, since they attach to the preceding grapheme), and terminals
// render nothing for them, so the token still *displays* as exactly
// "[pasted N chars]" while remaining unique underneath.
const chipMarkerLen = 6

// chips tracks live paste chips by their full token text (the visible
// "[pasted N chars]" plus its marker), so Value can expand each token
// back to the text it replaced.
type chips struct {
	byToken map[string]string
	seq     uint32
}

// newChips returns an empty chips.
func newChips() chips {
	return chips{byToken: map[string]string{}}
}

// withToken returns chips with a new, unique token recorded for text (n
// runes), plus that token.
func (c chips) withToken(n int, text string) (chips, string) {
	tok := "[pasted " + strconv.Itoa(n) + " chars]" + chipMarker(c.seq)
	next := make(map[string]string, len(c.byToken)+1)
	for k, v := range c.byToken {
		next[k] = v
	}
	next[tok] = text
	return chips{byToken: next, seq: c.seq + 1}, tok
}

// withoutToken returns chips with tok forgotten.
func (c chips) withoutToken(tok string) chips {
	if _, ok := c.byToken[tok]; !ok {
		return c
	}
	next := make(map[string]string, len(c.byToken))
	for k, v := range c.byToken {
		if k != tok {
			next[k] = v
		}
	}
	return chips{byToken: next, seq: c.seq}
}

// expand replaces every live chip token still present in s with the text
// it stands for.
func (c chips) expand(s string) string {
	for tok, text := range c.byToken {
		if strings.Contains(s, tok) {
			s = strings.ReplaceAll(s, tok, text)
		}
	}
	return s
}

// spanContaining returns the rune range [start, end) of the live chip
// token in line that contains col, where col == start or col == end also
// counts as "containing" (the cursor touching either edge of the token).
// If more than one live token's range contains col, which one is
// returned is unspecified but every candidate span in that position
// would be handled identically by callers.
func (c chips) spanContaining(line []rune, col int) (tok string, start, end int, ok bool) {
	for t := range c.byToken {
		tr := []rune(t)
		for i := 0; i+len(tr) <= len(line); i++ {
			if string(line[i:i+len(tr)]) != t {
				continue
			}
			if col >= i && col <= i+len(tr) {
				return t, i, i + len(tr), true
			}
		}
	}
	return "", 0, 0, false
}

// chipMarker encodes id as chipMarkerLen decimal digits, each rendered as
// an invisible variation selector.
func chipMarker(id uint32) string {
	digits := make([]rune, chipMarkerLen)
	for i := chipMarkerLen - 1; i >= 0; i-- {
		digits[i] = rune(0xFE00 + id%10)
		id /= 10
	}
	return string(digits)
}

// markerLow and markerHigh bound the Unicode variation-selector block
// (U+FE00-U+FE0F) chipMarker draws from. stripMarkers uses the whole
// block, a superset of what chipMarker actually emits (digits 0-9, so
// U+FE00-U+FE09), so it also catches any marker rune left over from a
// chip token a future change encodes differently.
const (
	markerLow  = 0xFE00
	markerHigh = 0xFE0F
)

// stripMarkers removes every chip marker rune from s. Value calls this
// unconditionally: an edit this widget doesn't otherwise guard against
// (see exitChipInterior/handleBackspace/handleDelete in prompt.go) could
// still slice through a token via some other bubbles/textarea editing
// command (ctrl+k, ctrl+w, a mouse selection, ...), leaving a damaged
// token that no longer matches any live entry in chips.byToken. expand
// then leaves it as plain text — stripMarkers is the last line of
// defense that keeps its invisible marker runes out of Value,
// SubmitMsg, and (since history is built from submitted Value text)
// history, even though the damaged token's visible text, if any
// survives, is left alone as ordinary text the user can see and fix.
func stripMarkers(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= markerLow && r <= markerHigh {
			return -1
		}
		return r
	}, s)
}
