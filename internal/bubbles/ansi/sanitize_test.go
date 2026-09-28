package ansi

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// assertInert fails if out could still drive the terminal: an ESC, a C1
// control, a C0 control other than '\n' and '\t', DEL, the kitty placeholder
// rune, or invalid UTF-8.
func assertInert(t *testing.T, in, out string) {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Errorf("Sanitize(%q) = %q: invalid UTF-8", in, out)
	}
	for _, r := range out {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20, r == 0x7f:
			t.Errorf("Sanitize(%q) = %q: contains C0/DEL %U", in, out, r)
		case r >= 0x80 && r <= 0x9f:
			t.Errorf("Sanitize(%q) = %q: contains C1 %U", in, out, r)
		case r == PlaceholderRune:
			t.Errorf("Sanitize(%q) = %q: contains PlaceholderRune", in, out)
		case (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			t.Errorf("Sanitize(%q) = %q: contains bidi control %U", in, out, r)
		}
	}
}

func TestSanitize_StripsHostileSequences(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"OSC 52 BEL-terminated", "a\x1b]52;c;aGk=\x07b", "ab"},
		{"OSC 52 ST-terminated", "a\x1b]52;c;aGk=\x1b\\b", "ab"},
		{"kitty APC", "x\x1b_Ga=T,f=100;AAAA\x1b\\y", "xy"},
		{"CSI", "\x1b[2J\x1b[Hhi", "hi"},
		{"8-bit CSI", "a\u009b31mb", "ab"},
		{"DCS sixel", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"lone trailing ESC", "a\x1b", "a"},
		{"bare CR", "r\rn", "rn"},
		{"CRLF", "l1\r\nl2", "l1\nl2"},
		{"BEL and DEL", "bell\x07del\x7f", "belldel"},
		{"tab and newline kept", "tab\there\nnext", "tab\there\nnext"},
		{"unicode kept", "ünï 日本 🎉", "ünï 日本 🎉"},
		{"placeholder rune", "fake" + string(PlaceholderRune) + "\u0305", "fake\u0305"},
		{"unterminated OSC", "a\x1b]0;title", "a"},

		// Additional hostile cases.
		{"OSC 8 hyperlink", "\x1b]8;;http://evil/\x1b\\click\x1b]8;;\x1b\\", "click"},
		{"8-bit OSC BEL", "a\u009d0;title\x07b", "ab"},
		{"8-bit OSC 8-bit ST", "a\u009d0;title\u009cb", "ab"},
		{"8-bit DCS", "a\u0090q#0\u009cb", "ab"},
		{"8-bit APC", "a\u009fGa=T;AAAA\u009cb", "ab"},
		{"8-bit PM", "a\u009esecret\u009cb", "ab"},
		{"8-bit SOS", "a\u0098secret\u009cb", "ab"},
		{"ESC PM", "a\x1b^secret\x1b\\b", "ab"},
		{"ESC SOS", "a\x1bXsecret\x1b\\b", "ab"},
		{"unterminated 8-bit CSI", "a\u009b31", "a"},
		{"unterminated CSI", "a\x1b[31;", "a"},
		{"unterminated DCS", "a\x1bPq#0;2", "a"},
		{"unterminated APC", "a\x1b_Gf=100;AAAA", "a"},
		{"other C1 dropped", "a\u0085b\u008ec\u008fd\u0084e", "abcde"},
		{"ESC c reset", "a\x1bcb", "ab"},
		{"ESC 7 save", "a\x1b7b", "ab"},
		{"SS2", "a\x1bNxb", "axb"},
		{"SS3", "a\x1bOxb", "axb"},
		{"charset designation", "a\x1b(0qqq\x1b(Bb", "aqqqb"},
		{"DECALN", "a\x1b#8b", "ab"},
		{"ESC ESC", "a\x1b\x1b[31mb", "ab"},
		{"ESC before newline keeps newline", "a\x1b\nb", "a\nb"},
		{"ESC before multibyte rune keeps the rune", "a\x1bébc", "aébc"},
		{"ESC before invalid byte", "a\x1b\xffb", "a\uFFFDb"},
		{"CSI private params", "a\x1b[?1049hb", "ab"},
		{"CSI intermediate", "a\x1b[1 qb", "ab"},
		{"CSI aborted by ESC", "a\x1b[31\x1b[0mb", "ab"},
		{"CSI aborted by multibyte rune", "a\x1b[3é1mb", "aé1mb"},
		{"CSI aborted by newline", "a\x1b[3\nb", "a\nb"},
		{"CSI aborted by CAN", "a\x1b[3\x18b", "ab"},
		{"OSC aborted by ESC", "a\x1b]0;t\x1b[31mb", "ab"},
		{"OSC payload multibyte with 0x9c continuation", "a\x1b]0;xќy\x07b", "ab"},
		{"OSC payload with 8-bit OSC inside", "a\x1b]0;x\u009dy\x07b", "ab"},
		{"OSC payload invalid UTF-8", "a\x1b]0;\xff\xfe\x07b", "ab"},
		{"multibyte with 0x9b continuation kept", "ћ", "ћ"},
		{"multibyte with 0x9d continuation kept", "ѝ", "ѝ"},
		{"raw 8-bit CSI byte is invalid UTF-8", "a\x9b31mb", "a\uFFFD31mb"},
		{"raw 8-bit OSC byte is invalid UTF-8", "a\x9d0;t\x07b", "a\uFFFD0;tb"},
		{"truncated lead byte then ESC", "a\xc3\x1b[31mb", "a\uFFFDb"},
		{"truncated C1 lead byte at end", "a\xc2", "a\uFFFD"},
		{"overlong ESC", "a\xc0\x9b31mb", "a\uFFFD\uFFFD31mb"},
		{"surrogate half", "a\xed\xa0\x80b", "a\uFFFD\uFFFD\uFFFDb"},
		{"literal U+FFFD kept", "a\uFFFDb", "a\uFFFDb"},
		{"NUL and backspace", "a\x00b\x08c", "abc"},
		{"vertical tab and form feed", "a\x0bb\x0cc", "abc"},
		{"C0 SO/SI", "a\x0eb\x0fc", "abc"},
		{"placeholder with diacritics", string(PlaceholderRune) + "\u0305\u030d", "\u0305\u030d"},
		{"placeholder UTF-8 bytes", "a\xf4\x8e\xbb\xaeb", "ab"},
		{"ZWJ emoji kept", "👨\u200d👩\u200d👧", "👨\u200d👩\u200d👧"},
		{"LRE dropped", "a\u202ab", "ab"},
		{"RLE dropped", "a\u202bb", "ab"},
		{"PDF dropped", "a\u202cb", "ab"},
		{"LRO dropped", "a\u202db", "ab"},
		{"RLO dropped", "a\u202eb", "ab"},
		{"LRI dropped", "a\u2066b", "ab"},
		{"RLI dropped", "a\u2067b", "ab"},
		{"FSI dropped", "a\u2068b", "ab"},
		{"PDI dropped", "a\u2069b", "ab"},
		{"Trojan Source command", "rm -rf \u202e/ tsil\u2066 # safe\u2069", "rm -rf / tsil # safe"},
		{"ZWJ technologist kept", "👩\u200d💻", "👩\u200d💻"},
		{"ZWNJ kept", "می\u200cخواهم", "می\u200cخواهم"},
		{"variation selector kept", "❤\ufe0f", "❤\ufe0f"},
		{"LRM kept", "abc\u200e123", "abc\u200e123"},
		{"RLM kept", "abc\u200f123", "abc\u200f123"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.in)
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
			assertInert(t, tt.in, got)
			if again := Sanitize(got); again != got {
				t.Errorf("Sanitize not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestSanitizeLine_FlattensNewlinesAndTabs(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a\nb\tc", "a b c"},
		{"a\r\nb", "a b"},
		{"t\x1b]0;x\ny\x07z", "tz"},
	}
	for _, tt := range tests {
		if got := SanitizeLine(tt.in); got != tt.want {
			t.Errorf("SanitizeLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWrap_LongWordHardWraps(t *testing.T) {
	got := Wrap("aaaaaaaaaa", 4)
	for _, line := range strings.Split(got, "\n") {
		if w := Width(line); w > 4 {
			t.Errorf("Wrap line %q has width %d > 4 (full: %q)", line, w, got)
		}
	}
	if strings.ReplaceAll(got, "\n", "") != "aaaaaaaaaa" {
		t.Errorf("Wrap lost text: %q", got)
	}
}

func TestWidthTruncateCut(t *testing.T) {
	if got := Width("\x1b[31m日本\x1b[0m"); got != 4 {
		t.Errorf("Width = %d, want 4", got)
	}
	if got := Truncate("hello world", 5, "…"); got != "hell…" {
		t.Errorf("Truncate = %q, want %q", got, "hell…")
	}
	if got := Cut("hello world", 6, 11); got != "world" {
		t.Errorf("Cut = %q, want %q", got, "world")
	}
}

func FuzzSanitize(f *testing.F) {
	seeds := []string{
		"a\x1b]52;c;aGk=\x07b", "\x1b[2J", "a\u009b31mb", "a\x1bPq\x1b\\",
		"\xc2", "\xc2\x9b", "\x1b\xc2\x9b", "ћ\x1b", string(PlaceholderRune),
		"\x1b[\xff", "\x1b]\xc2\x9c", "\r\n\t", "\u202e\u2066\u2069",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Sanitize(in)
		assertInert(t, in, out)
		if again := Sanitize(out); again != out {
			t.Errorf("Sanitize not idempotent: %q -> %q -> %q", in, out, again)
		}
		line := SanitizeLine(in)
		if strings.ContainsAny(line, "\n\t") {
			t.Errorf("SanitizeLine(%q) = %q contains newline or tab", in, line)
		}
	})
}
