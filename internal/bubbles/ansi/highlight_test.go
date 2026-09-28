package ansi

import (
	"strings"
	"testing"
)

// FuzzHighlight checks that Highlight only ever inserts on/off: removing
// them gives back s.
func FuzzHighlight(f *testing.F) {
	f.Add("\x1b[31mHello\x1b[0m world", "hello")
	f.Add("ab\x1b[1mcd", "bc")
	f.Add("\x1b]8;;http://x/foo\x1b\\link\x1b]8;;\x1b\\", "foo")
	f.Add("İstanbul \xff\u009b1m", "i")
	f.Fuzz(func(t *testing.T, s, query string) {
		const on, off = "\U000F0001", "\U000F0002"
		if strings.Contains(s, on) || strings.Contains(s, off) {
			return
		}
		got := Highlight(s, query, on, off)
		if back := strings.NewReplacer(on, "", off, "").Replace(got); back != s {
			t.Errorf("Highlight(%q, %q) = %q; without markers %q", s, query, got, back)
		}
	})
}

func TestHighlight_VisibleTextOnly(t *testing.T) {
	tests := []struct {
		name, s, query, want string
	}{
		{
			"styled word",
			"\x1b[31mHello\x1b[0m world", "hello",
			"\x1b[31m[Hello]\x1b[0m world",
		},
		{
			"query only inside OSC 8 URL",
			"\x1b]8;;http://x/foo\x1b\\link\x1b]8;;\x1b\\", "foo",
			"\x1b]8;;http://x/foo\x1b\\link\x1b]8;;\x1b\\",
		},
		{
			"query only inside CSI params",
			"\x1b[38;5;123mx\x1b[0m", "123",
			"\x1b[38;5;123mx\x1b[0m",
		},
		{
			"every occurrence, case-insensitive",
			"Foo foo FOO", "foo",
			"[Foo] [foo] [FOO]",
		},
		{
			"non-overlapping",
			"aaaa", "aa",
			"[aa][aa]",
		},
		{
			"multibyte runes",
			"日本語 ÜBER über", "über",
			"日本語 [ÜBER] [über]",
		},
		{"empty query", "abc", "", "abc"},
		{"no match", "abc", "x", "abc"},
		{"empty string", "", "x", ""},
		{
			"no match across newline",
			"a\nb", "ab",
			"a\nb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Highlight(tt.s, tt.query, "[", "]"); got != tt.want {
				t.Errorf("Highlight(%q, %q) = %q, want %q", tt.s, tt.query, got, tt.want)
			}
		})
	}
}

func TestHighlight_MatchAcrossEscape(t *testing.T) {
	tests := []struct {
		s, query, want string
	}{
		{"ab\x1b[1mcd", "bc", "a[b\x1b[1mc]d"},
		{"a\x1b[1mb\x1b[0mc", "abc", "[a\x1b[1mb\x1b[0mc]"},
		{"x\x1b]8;;u\x1b\\yz", "xy", "[x\x1b]8;;u\x1b\\y]z"},
	}
	for _, tt := range tests {
		if got := Highlight(tt.s, tt.query, "[", "]"); got != tt.want {
			t.Errorf("Highlight(%q, %q) = %q, want %q", tt.s, tt.query, got, tt.want)
		}
	}
}
