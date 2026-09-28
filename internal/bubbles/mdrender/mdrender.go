// Package mdrender renders Markdown into styled, width-wrapped terminal
// lines for widgets that need to display Markdown text (model output, tool
// results) inside a fixed-width column. It wraps glamour, caching one
// glamour.TermRenderer per width it has been asked to render at.
package mdrender

import (
	"image/color"
	"strings"
	"sync"

	glamour "charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Styles holds the colors mdrender maps onto glamour's style configuration.
type Styles struct {
	Text, Muted, Heading, Link, Code, CodeBg, Quote, Rule color.Color
}

// DefaultStyles returns mdrender's built-in colors. They are fixed hex
// values, independent of any theme or terminal, so they are safe to use
// wherever a caller has not supplied its own Styles (via WithStyles).
func DefaultStyles() Styles {
	return Styles{
		Text:    lipgloss.Color("#d0d0d0"),
		Muted:   lipgloss.Color("#808080"),
		Heading: lipgloss.Color("#5fafff"),
		Link:    lipgloss.Color("#5fd7ff"),
		Code:    lipgloss.Color("#ffaf5f"),
		CodeBg:  lipgloss.Color("#303030"),
		Quote:   lipgloss.Color("#808080"),
		Rule:    lipgloss.Color("#585858"),
	}
}

// Option configures a Renderer built by New.
type Option func(*Renderer)

// WithStyles sets the Renderer's initial Styles, in place of DefaultStyles.
func WithStyles(st Styles) Option {
	return func(r *Renderer) { r.styles = st }
}

// Renderer renders Markdown to terminal lines at a given width. It is safe
// for concurrent use, though the App only ever calls Render from Update.
type Renderer struct {
	mu        sync.Mutex
	styles    Styles
	renderers map[int]*glamour.TermRenderer
}

// New builds a Renderer. With no options it uses DefaultStyles.
func New(opts ...Option) *Renderer {
	r := &Renderer{
		styles:    DefaultStyles(),
		renderers: map[int]*glamour.TermRenderer{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// SetStyles replaces r's Styles and drops every cached TermRenderer, so the
// next Render at any width rebuilds with the new colors.
func (r *Renderer) SetStyles(st Styles) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.styles = st
	r.renderers = map[int]*glamour.TermRenderer{}
}

// Render renders md at width, returning its lines with leading and trailing
// blank lines trimmed. A glamour error, including one building the
// TermRenderer, falls back to ansi.Wrap of md. width <= 0 has no cells to
// render into, so Render returns nil without invoking glamour, which would
// otherwise treat a non-positive word-wrap width as "disabled" and emit
// unwrapped lines.
func (r *Renderer) Render(md string, width int) []string {
	if width <= 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tr, err := r.rendererForWidth(width)
	if err != nil {
		return trimBlankLines(ansi.Wrap(md, width))
	}
	out, err := tr.Render(md)
	if err != nil {
		return trimBlankLines(ansi.Wrap(md, width))
	}
	return enforceWidth(trimBlankLines(out), width)
}

// rendererForWidth returns the TermRenderer cached for width, building and
// caching one first if needed. Callers must hold r.mu.
func (r *Renderer) rendererForWidth(width int) (*glamour.TermRenderer, error) {
	if tr, ok := r.renderers[width]; ok {
		return tr, nil
	}
	tr, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleConfig(r.styles)),
		glamour.WithWordWrap(width),
		glamour.WithChromaFormatter("terminal16m"),
	)
	if err != nil {
		return nil, err
	}
	r.renderers[width] = tr
	return tr, nil
}

// trimBlankLines splits s into lines and drops leading and trailing lines
// that have no visible content once escape sequences are stripped.
func trimBlankLines(s string) []string {
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) && isBlank(lines[start]) {
		start++
	}
	end := len(lines)
	for end > start && isBlank(lines[end-1]) {
		end--
	}
	return lines[start:end]
}

func isBlank(line string) bool {
	return strings.TrimSpace(xansi.Strip(line)) == ""
}

// enforceWidth guards against glamour's word-wrap occasionally leaving a
// line wider than width (observed at very small widths, where adjacent
// styled spans such as a run of text followed by inline code's padding
// space produce a run of spaces lipgloss.Wrap does not split). Any line
// that is still too wide is hard-wrapped with ansi.Wrap.
func enforceWidth(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if ansi.Width(line) <= width {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(line, width), "\n")...)
	}
	return out
}
