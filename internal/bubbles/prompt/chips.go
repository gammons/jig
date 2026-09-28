package prompt

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// pasteChipThreshold is the exact spec number: a paste over this many
// (sanitized) runes collapses into a chip instead of being inserted
// verbatim.
const pasteChipThreshold = 500

// tokenPrefix opens every chip token's visible text; it never appears in
// a live token at any other position, so a scan for it finds every chip
// (intact or damaged) in a line.
const tokenPrefix = "[pasted "

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
	tok := tokenPrefix + strconv.Itoa(n) + " chars]" + chipMarker(c.seq)
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

// stripMarkers removes every chip marker rune from s.
func stripMarkers(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= markerLow && r <= markerHigh {
			return -1
		}
		return r
	}, s)
}

// reconcile is Value's backstop against a damaged chip token: it removes
// every leftover fragment of a token that survived past delete.go's
// classify/handleDeleteKey guard (a multi-row selection delete, say —
// that guard only ever acts on the cursor's own row, since chips never
// span lines, so an edit that damages a chip while also moving off its
// row, or one delivered by something other than a keyboard entirely —
// e.g. a mouse-driven selection — is outside its scope), then expands
// every token that's still intact. In practice reconcile is a no-op,
// since classify's catch-all default (delete.go's classOther) protects
// every key capable of mutating the buffer that isn't one this package
// has classified more precisely, not a list of specifically-risky ones;
// it exists so Value's "never a partial token" guarantee doesn't depend
// on that coverage being exhaustive.
func (c chips) reconcile(s string) string {
	return c.expand(stripDamagedTokens(c, s))
}

// stripDamagedTokens removes every occurrence of tokenPrefix in s that
// isn't the start of one of c's own live, fully intact tokens. A scan
// for tokenPrefix finds every chip in s, live or damaged (tokenPrefix
// never appears anywhere else in a live token, and typed text starting
// with it is vanishingly unlikely — accepted as a limitation of a
// backstop that only runs after an edit this widget doesn't otherwise
// guard against); each damaged one is removed through its closing "]"
// (plus marker runes, if any survive right after it) or, if even the
// "]" is gone, to the end of the line — since a deletion command can
// leave the visible prefix and the closing bracket on either side of
// its cut, there's no shorter fragment guaranteed to be exactly the
// stray remains and nothing more.
func stripDamagedTokens(c chips, s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], tokenPrefix)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		j += i
		b.WriteString(s[i:j])
		if live, n := liveTokenAt(c, s[j:]); live {
			b.WriteString(s[j : j+n])
			i = j + n
			continue
		}
		i = j + damagedTokenLen(s[j:])
	}
	return b.String()
}

// liveTokenAt reports whether s starts with one of c's live tokens, and
// its byte length.
func liveTokenAt(c chips, s string) (bool, int) {
	for tok := range c.byToken {
		if strings.HasPrefix(s, tok) {
			return true, len(tok)
		}
	}
	return false, 0
}

// damagedTokenLen returns the byte length of the damaged token fragment
// at the start of s (which starts with tokenPrefix): through its
// closing "]" and any marker runes right after it, or to the next
// newline (or the end of s) if there's no closing "]" on this line.
func damagedTokenLen(s string) int {
	end := strings.IndexAny(s, "]\n")
	if end < 0 {
		return len(s)
	}
	if s[end] == '\n' {
		return end
	}
	end++ // include the "]"
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if r < markerLow || r > markerHigh {
			break
		}
		end += size
	}
	return end
}
