package ansi

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
)

// visibleText is the lowercased visible runes of a string, with each rune's
// byte span in the original string.
type visibleText struct {
	runes        []rune
	starts, ends []int
}

// Highlight wraps every case-insensitive occurrence of query in the
// *visible* text of s with on/off. Existing escape sequences are copied
// byte-for-byte and never matched inside (e.g. an OSC 8 URL).
//
// on goes immediately before a match's first visible rune and off
// immediately after its last, so escapes inside a match stay where they
// are. Matches are found left to right and do not overlap. Case folding is
// per rune (unicode.ToLower).
func Highlight(s, query, on, off string) string {
	q := []rune(query)
	for i, r := range q {
		q[i] = unicode.ToLower(r)
	}
	if len(q) == 0 || s == "" {
		return s
	}
	v := visible(s, unicode.ToLower)
	var b strings.Builder
	last := 0
	for k := 0; k+len(q) <= len(v.runes); {
		if !slices.Equal(v.runes[k:k+len(q)], q) {
			k++
			continue
		}
		start, end := v.starts[k], v.ends[k+len(q)-1]
		b.WriteString(s[last:start])
		b.WriteString(on)
		b.WriteString(s[start:end])
		b.WriteString(off)
		last = end
		k += len(q)
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// HighlightTokens wraps every occurrence of one of toks in the visible
// text of s with on/off, where it stands as a whole whitespace-delimited
// token (case-sensitive; "@a.go" never colors part of "@a.goo"). Escapes
// are copied byte-for-byte, as in Highlight; when two tokens could start
// at the same place the longest wins.
func HighlightTokens(s string, toks []string, on, off string) string {
	if len(toks) == 0 || s == "" {
		return s
	}
	want := make([][]rune, 0, len(toks))
	for _, t := range toks {
		if t != "" {
			want = append(want, []rune(t))
		}
	}
	v := visible(s, func(r rune) rune { return r })
	space := func(k int) bool { return k < 0 || k >= len(v.runes) || unicode.IsSpace(v.runes[k]) }
	var b strings.Builder
	last := 0
	for k := 0; k < len(v.runes); k++ {
		if !space(k - 1) {
			continue
		}
		n := 0
		for _, q := range want {
			if len(q) > n && k+len(q) <= len(v.runes) && slices.Equal(v.runes[k:k+len(q)], q) && space(k+len(q)) {
				n = len(q)
			}
		}
		if n == 0 {
			continue
		}
		start, end := v.starts[k], v.ends[k+n-1]
		b.WriteString(s[last:start])
		b.WriteString(on)
		b.WriteString(s[start:end])
		b.WriteString(off)
		last = end
		k += n - 1
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// visible segments s with xansi.DecodeSequence and collects the runes that
// are not part of an escape sequence or control, each mapped through fold.
func visible(s string, fold func(rune) rune) visibleText {
	var v visibleText
	var state byte
	for i := 0; i < len(s); {
		_, _, n, newState := xansi.DecodeSequence(s[i:], state, nil)
		state = newState
		if n <= 0 {
			n = 1
		}
		n = min(n, len(s)-i)
		seg := s[i : i+n]
		if !isEscape(seg) {
			for j := 0; j < len(seg); {
				r, size := utf8.DecodeRuneInString(seg[j:])
				v.runes = append(v.runes, fold(r))
				v.starts = append(v.starts, i+j)
				v.ends = append(v.ends, i+j+size)
				j += size
			}
		}
		i += n
	}
	return v
}

// isEscape reports whether seg, one DecodeSequence segment, is an escape
// sequence or a control other than '\n' and '\t'.
func isEscape(seg string) bool {
	c := seg[0]
	switch {
	case c == esc, c == del:
		return true
	case c < 0x20:
		return c != '\n' && c != '\t'
	case c >= 0x80 && c < 0xc0:
		return true
	}
	return false
}
