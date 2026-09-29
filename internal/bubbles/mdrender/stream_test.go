package mdrender

import (
	"slices"
	"strings"
	"testing"
)

// streamMD is a reply with paragraphs, a list, a heading, and a fenced
// block holding blank lines (which must not split it).
const streamMD = "Intro paragraph with `code` and **bold**.\n\n" +
	"- one\n- two\n\n" +
	"## A heading\n\n" +
	"```go\nfunc f() {\n\n\treturn\n}\n```\n\n" +
	"Closing words that wrap across the line at a narrow width."

func TestStableSplit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, md   string
		wantStable []string
		wantTail   string
	}{
		{"no blank line", "one\ntwo", nil, "one\ntwo"},
		{"one finished paragraph", "one\n\ntwo", []string{"one"}, "two"},
		{"trailing blank keeps the tail empty", "one\n\n", []string{"one"}, ""},
		{"multiple blanks", "a\n\n\n\nb", []string{"a"}, "b"},
		{"fence holds its blank lines", "a\n\n```\nx\n\ny\n```\n\nb", []string{"a", "```\nx\n\ny\n```"}, "b"},
		{"open fence stays in the tail", "a\n\n```\nx\n\ny", []string{"a"}, "```\nx\n\ny"},
		{"tilde fence", "~~~\nx\n\ny\n~~~\n\nb", []string{"~~~\nx\n\ny\n~~~"}, "b"},
		{"whitespace-only line is blank", "a\n  \nb", []string{"a"}, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stable, tail := stableSplit(tt.md)
			if !slices.Equal(stable, tt.wantStable) || tail != tt.wantTail {
				t.Errorf("stableSplit(%q) = %q, %q; want %q, %q", tt.md, stable, tail, tt.wantStable, tt.wantTail)
			}
		})
	}
}

// TestRenderStreaming_MatchesChunks: the streaming render is each finished
// paragraph rendered alone, then the tail, one blank line apart, at every
// prefix of the reply as it grows character by character.
func TestRenderStreaming_MatchesChunks(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	for n := 1; n <= len(streamMD); n++ {
		md := streamMD[:n]
		got := r.RenderStreaming("k", md, 40)
		stable, tail := stableSplit(md)
		var want []string
		for _, c := range append(stable, tail) {
			lines := r.Render(c, 40)
			if len(lines) == 0 {
				continue
			}
			if len(want) > 0 {
				want = append(want, "")
			}
			want = append(want, lines...)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("prefix %d (%q):\n got %q\nwant %q", n, md, got, want)
		}
	}
}

// TestRenderStreaming_RendersOnlyTheTail: once a paragraph is finished it is
// rendered once; later calls re-render only the growing tail.
func TestRenderStreaming_RendersOnlyTheTail(t *testing.T) {
	t.Parallel()
	r := New()
	md := strings.Repeat("A finished paragraph of prose.\n\n", 50)
	_ = r.RenderStreaming("k", md+"tail", 60)
	before := r.renders
	for i := range 10 {
		_ = r.RenderStreaming("k", md+"tail"+strings.Repeat(" more", i), 60)
	}
	if n := r.renders - before; n != 10 {
		t.Errorf("10 growing-tail calls made %d glamour renders, want 10 (the tail only)", n)
	}
}

// TestRenderStreaming_KeysAndWidthsAreSeparate: a different key, a width
// change, or text that no longer extends the cached paragraphs never
// reuses stale lines.
func TestRenderStreaming_KeysAndWidthsAreSeparate(t *testing.T) {
	t.Parallel()
	r := New(WithStyles(testStyles()))
	a := "first paragraph here\n\nsecond"
	b := "a different start\n\nsecond"
	check := func(key, md string, w int) {
		t.Helper()
		fresh := New(WithStyles(testStyles()))
		if got, want := r.RenderStreaming(key, md, w), fresh.RenderStreaming(key, md, w); !slices.Equal(got, want) {
			t.Errorf("RenderStreaming(%q, %q, %d) = %q, want %q", key, md, w, got, want)
		}
	}
	check("k1", a, 40)
	check("k2", b, 40) // another block
	check("k1", a, 20) // the same block at a new width
	check("k1", b, 20) // the same block, text replaced (not extended)
	check("k1", a, 40)
}

// TestRenderStreaming_Forget: Forget drops a key's cache, so the next call
// renders every paragraph again.
func TestRenderStreaming_Forget(t *testing.T) {
	t.Parallel()
	r := New()
	md := "p1\n\np2\n\ntail"
	_ = r.RenderStreaming("k", md, 60)
	r.Forget("k")
	before := r.renders
	_ = r.RenderStreaming("k", md, 60)
	if n := r.renders - before; n != 3 {
		t.Errorf("after Forget, %d renders, want 3 (two paragraphs and the tail)", n)
	}
}

// TestRenderStreaming_SetStylesInvalidates: a style change re-renders the
// cached paragraphs with the new colors.
func TestRenderStreaming_SetStylesInvalidates(t *testing.T) {
	t.Parallel()
	r := New()
	md := "# Heading\n\ntail"
	old := r.RenderStreaming("k", md, 60)
	r.SetStyles(testStyles())
	got := r.RenderStreaming("k", md, 60)
	want := New(WithStyles(testStyles())).RenderStreaming("k", md, 60)
	if !slices.Equal(got, want) || slices.Equal(got, old) {
		t.Errorf("after SetStyles got %q, want the new styles' %q", got, want)
	}
}
