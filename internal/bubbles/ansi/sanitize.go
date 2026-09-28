package ansi

import (
	"strings"
	"unicode/utf8"
)

const (
	bel = 0x07
	esc = 0x1b
	del = 0x7f

	c1CSI = 0x9b
	c1ST  = 0x9c
)

// Sanitize makes untrusted text inert. It keeps printable runes, '\n', and
// '\t', and drops every escape sequence (7-bit and C1, including their
// payloads), every other C0/C1 control, DEL, '\r', and PlaceholderRune.
// Invalid UTF-8 bytes become U+FFFD. The result is valid UTF-8 and
// Sanitize(Sanitize(s)) == Sanitize(s).
//
// Sequences are skipped the way a terminal would parse them: a CSI ends at
// its final byte or is aborted by any byte outside 0x20–0x7e; a string
// sequence (OSC, DCS, APC, PM, SOS) ends at BEL, ST, or an ESC that starts
// another sequence. An unterminated sequence runs to the end of s. Aborting
// bytes are then handled as ordinary input.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		i = sanitizeStep(&b, s, i)
	}
	return b.String()
}

// SanitizeLine is Sanitize with '\n' and '\t' turned into spaces, for text
// that must stay on one line.
func SanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, Sanitize(s))
}

// sanitizeStep consumes the input at s[i], writes whatever of it is kept,
// and returns the index of the next unconsumed byte.
func sanitizeStep(b *strings.Builder, s string, i int) int {
	c := s[i]
	if c == esc {
		return skipEsc(s, i+1)
	}
	if c < utf8.RuneSelf {
		if c == '\n' || c == '\t' || (c >= 0x20 && c != del) {
			b.WriteByte(c)
		}
		return i + 1
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	switch {
	case r == utf8.RuneError && size == 1:
		b.WriteRune(utf8.RuneError)
	case r == c1CSI:
		return skipCSI(s, i+size)
	case isC1StringIntro(r):
		return skipString(s, i+size)
	case r >= 0x80 && r <= 0x9f, r == PlaceholderRune:
	default:
		b.WriteString(s[i : i+size])
	}
	return i + size
}

// isC1StringIntro reports whether r is an 8-bit DCS, SOS, OSC, PM, or APC.
func isC1StringIntro(r rune) bool {
	switch r {
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
		return true
	}
	return false
}

// skipEsc skips the sequence whose ESC was just before s[i]. Besides CSI and
// the string sequences, it skips an ESC sequence's intermediates (0x20–0x2f)
// and final byte (0x30–0x7e). Any other byte after ESC is left in place.
func skipEsc(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		return skipCSI(s, i+1)
	case ']', 'P', '_', '^', 'X':
		return skipString(s, i+1)
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
		return i + 1
	}
	return i
}

// skipCSI skips a CSI's parameter and intermediate bytes and its final
// byte. Any other byte aborts the sequence and is left in place.
func skipCSI(s string, i int) int {
	for i < len(s) {
		c := s[i]
		switch {
		case c >= 0x40 && c <= 0x7e:
			return i + 1
		case c >= 0x20 && c <= 0x3f:
			i++
		default:
			return i
		}
	}
	return i
}

// skipString skips a string sequence's payload through its BEL, ESC \, or
// 8-bit ST terminator. Any other ESC ends it and is left in place.
func skipString(s string, i int) int {
	for i < len(s) {
		c := s[i]
		switch {
		case c == bel:
			return i + 1
		case c == esc:
			if i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			return i
		case c < utf8.RuneSelf:
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == c1ST {
				return i + size
			}
			i += size
		}
	}
	return i
}
