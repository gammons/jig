package ui

import (
	"slices"
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// reasoningIndent is the blank columns before each line of a thinking
// block's streamed text, setting it off under the spinner line.
const reasoningIndent = 2

// reasoningCache is a thinking block's finished source lines (prefix, the
// raw text up to and including its last "\n") and their wrapped, styled
// lines at width. Reasoning only grows while the model thinks, so each
// render wraps only the lines finished since the last one and the
// unfinished tail: a tick's cost doesn't grow with the thinking's length.
type reasoningCache struct {
	width  int
	prefix string
	lines  []string
}

// renderReasoningBlock renders a reasoning block: its one-line label and,
// while the model is still thinking in it (unless r.hideReasoning), its
// text below, dim and indented. The block's cache entry is dropped once
// it collapses back to the label.
func (r *renderer) renderReasoningBlock(data blockData, width int) []string {
	lines := []string{r.fitLine(r.renderReasoning(data.Thinking, data.Duration, data.Frame), width)}
	if !data.Thinking || r.hideReasoning {
		delete(r.thinking, string(data.Block.ID))
		return lines
	}
	return append(lines, r.reasoningBody(string(data.Block.ID), data.Block.Text, width)...)
}

// reasoningBody returns text's lines wrapped to width less the indent,
// trailing blank lines dropped, reusing key's cached finished lines.
func (r *renderer) reasoningBody(key, text string, width int) []string {
	cut := strings.LastIndexByte(text, '\n') + 1
	c := r.thinking[key]
	if c == nil || c.width != width || len(c.prefix) > cut || !strings.HasPrefix(text, c.prefix) {
		c = &reasoningCache{width: width}
		r.thinking[key] = c
	}
	if cut > len(c.prefix) {
		c.lines = append(c.lines, r.wrapReasoning(text[len(c.prefix):cut-1], width)...)
		c.prefix = text[:cut]
	}
	var tail []string
	if cut < len(text) {
		tail = r.wrapReasoning(text[cut:], width)
	}
	out := slices.Concat(c.lines, tail)
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// wrapReasoning sanitizes src, expands its tabs, and wraps each of its
// lines to width less the indent: a blank line stays blank, and every
// other line is indented and Dim-styled.
func (r *renderer) wrapReasoning(src string, width int) []string {
	inner := max(width-reasoningIndent, 1)
	pad := strings.Repeat(" ", reasoningIndent)
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(ansi.Sanitize(src), "\t", "    "), "\n") {
		if strings.TrimSpace(l) == "" {
			out = append(out, "")
			continue
		}
		for _, w := range strings.Split(ansi.Wrap(l, inner), "\n") {
			out = append(out, pad+r.set.Render.Dim.Render(w))
		}
	}
	return out
}
