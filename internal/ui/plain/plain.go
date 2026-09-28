// Package plain renders bus events as plain text for headless runs: the
// root session's assistant text goes to stdout, while tool activity,
// subagent spawns, and failures go to stderr.
package plain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// inputPreviewRunes caps how much of a tool call's JSON input is shown.
const inputPreviewRunes = 80

// Renderer writes events to out (root text) and errw (everything else).
type Renderer struct {
	out      io.Writer
	errw     io.Writer
	children map[core.SessionID]int // child session -> nesting depth (>= 1)
	wrote    bool                   // any root text written to out
	lastNL   bool                   // the last byte written to out was '\n'
	newStep  bool                   // a root message started since root text was last written
}

// New returns a Renderer writing to out and errw.
func New(out, errw io.Writer) *Renderer {
	return &Renderer{out: out, errw: errw, children: make(map[core.SessionID]int)}
}

// Run renders events until the channel closes.
func (r *Renderer) Run(events <-chan event.Event) {
	for e := range events {
		r.handle(e)
	}
}

func (r *Renderer) handle(e event.Event) {
	depth, child := r.children[e.Session()]
	switch ev := e.(type) {
	case event.SubagentSpawned:
		r.children[ev.Child] = depth + 1
		r.line(depth, "↳ %s: %s", ev.Agent, ev.Description)
	case event.MessageStarted:
		if !child && r.wrote {
			r.newStep = true
		}
	case event.TextDelta:
		if !child {
			r.stepText(ev.Text)
		}
	case event.ToolCallStarted:
		r.line(depth, "→ %s", toolLine(ev.Call))
	case event.ToolCallFinished:
		if ev.Result.IsError {
			r.line(depth, "  ✗ %s", firstLine(ev.Result.Output))
		}
	case event.RunFinished:
		if !child && r.wrote && !r.lastNL {
			r.text("\n")
		}
	case event.RunFailed:
		if !child {
			r.line(0, "error: %s", ev.Err)
		}
	}
}

// stepText writes root text, first separating it from an earlier step's
// text by exactly one blank line.
func (r *Renderer) stepText(s string) {
	if s == "" {
		return
	}
	if r.newStep {
		r.newStep = false
		if r.lastNL {
			r.text("\n")
		} else {
			r.text("\n\n")
		}
	}
	r.text(ansi.Sanitize(s))
}

func (r *Renderer) text(s string) {
	if s == "" {
		return
	}
	io.WriteString(r.out, s)
	r.wrote = true
	r.lastNL = strings.HasSuffix(s, "\n")
}

// line writes one stderr line; its formatted content is untrusted
// (model, tool, and session text) and is sanitized to a single line.
func (r *Renderer) line(depth int, format string, args ...any) {
	io.WriteString(r.errw, strings.Repeat("  ", depth)+ansi.SanitizeLine(fmt.Sprintf(format, args...))+"\n")
}

// toolLine is "<tool> <input>", with the input compacted to one line and
// cut to its first inputPreviewRunes runes.
func toolLine(c core.ToolCall) string {
	var input string
	var buf bytes.Buffer
	if json.Compact(&buf, c.Input) == nil {
		input = buf.String()
	} else {
		input = strings.Join(strings.Fields(string(c.Input)), " ")
	}
	if runes := []rune(input); len(runes) > inputPreviewRunes {
		input = string(runes[:inputPreviewRunes])
	}
	if input == "" {
		return c.Name
	}
	return c.Name + " " + input
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
