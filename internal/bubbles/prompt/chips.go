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

// suffixToken returns the live chip token that s ends with, if any.
func (c chips) suffixToken(s string) (string, bool) {
	for tok := range c.byToken {
		if strings.HasSuffix(s, tok) {
			return tok, true
		}
	}
	return "", false
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
