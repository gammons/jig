package mdrender

import (
	"slices"
	"strings"
)

// Streaming render (spec: a growing reply must not cost a full re-render
// per tick). Rendering a whole reply costs ~7 ms per KB, so re-rendering
// it every tick makes a long reply block the UI. A paragraph followed by a
// blank line (outside a code fence) will never change again, so
// RenderStreaming renders each finished paragraph once, caches its lines,
// and re-renders only the unfinished tail. Rendering paragraphs apart can
// differ from rendering them together in blank-line spacing (around lists
// and headings); the caller renders the finished text once with Render.

// stream is one key's cache: the finished paragraphs rendered so far at
// width, and the source text they cover (prefix), so a call whose text no
// longer starts with it (replaced, not extended) starts over.
type stream struct {
	width  int
	prefix string
	chunks [][]string
}

// RenderStreaming renders md, a reply still being streamed under key, at
// width: every finished paragraph (cached per key across calls) and then
// the unfinished tail, one blank line apart. width <= 0 returns nil.
func (r *Renderer) RenderStreaming(key, md string, width int) []string {
	if width <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	s := r.streams[key]
	if s == nil || s.width != width || !strings.HasPrefix(md, s.prefix) {
		s = &stream{width: width}
		r.streams[key] = s
	}
	stable, tail := stableSplit(md[len(s.prefix):])
	for _, c := range stable {
		s.chunks = append(s.chunks, r.render(c, width))
	}
	s.prefix = md[:len(md)-len(tail)]

	var out []string
	for _, lines := range append(slices.Clone(s.chunks), r.render(tail, width)) {
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	return out
}

// Forget drops key's streaming cache (its reply finished or was removed).
func (r *Renderer) Forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, key)
}

// stableSplit splits md into its finished paragraphs (each ended by a
// blank line outside a ``` or ~~~ fence, returned without it) and the
// unfinished tail after the last such blank line. Blank lines inside a
// fence, even an unclosed one, never split. Every blank line after a
// finished paragraph belongs to it, so the tail starts at content.
func stableSplit(md string) (stable []string, tail string) {
	start, fence, para := 0, "", -1 // para: where the current paragraph began, or -1
	for pos := 0; pos < len(md); {
		end := strings.IndexByte(md[pos:], '\n')
		if end < 0 {
			break // the last line has no newline yet: it is the tail's
		}
		line := strings.TrimSpace(md[pos : pos+end])
		next := pos + end + 1
		switch {
		case fence != "":
			if strings.HasPrefix(line, fence) {
				fence = ""
			}
		case strings.HasPrefix(line, "```"), strings.HasPrefix(line, "~~~"):
			fence = line[:3]
		}
		if line == "" && fence == "" {
			if para >= 0 {
				stable = append(stable, strings.TrimRight(md[para:pos], "\n"))
				para = -1
			}
			start = next
		} else if para < 0 {
			para = pos
		}
		pos = next
	}
	return stable, md[start:]
}
