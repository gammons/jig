// Package ui is jig's TUI app: the Bubble Tea model that wires the
// transcript projection, the widgets in internal/bubbles, and themes
// together. It does no I/O of its own (AGENTS.md): every port it needs
// comes from internal/core, and every third-party import is
// charm.land/... or one of internal/bubbles' helper packages.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/mdrender"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// spinnerFrames are the braille dots used for a running tool's or
// subagent's indicator (spec §5.3), matching statusbar's own.
const spinnerFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// rightPad is the number of blank columns every transcript block leaves
// before the scrollbar, so text never runs flush against it.
const rightPad = 2

// blockData is blocklist.Item.Data for every item the App puts in the
// transcript's blocklist. The App bumps Item.Version whenever any field
// here changes (R22: Duration; the spinner Frame; Card from
// permcard.View()).
type blockData struct {
	Block    transcript.Block
	Card     string
	Duration time.Duration
	Frame    int
	Thinking bool // a reasoning block the model is still thinking in (Projection.Thinking)
}

// renderer renders blockData items for the transcript's blocklist.
type renderer struct {
	md  *mdrender.Renderer
	set *theme.Set
}

// newRenderer builds a renderer styled from set.
func newRenderer(set *theme.Set) *renderer {
	return &renderer{md: mdrender.New(mdrender.WithStyles(set.Markdown)), set: set}
}

// render is a blocklist.RenderFunc: it dispatches on the block's Kind to
// the one-line formatters (§5.3) or mdrender, then appends the item's
// permission card, if any. Every string taken from the Block passes
// ansi.SanitizeLine (one-liners) or ansi.Sanitize (bodies) before styling.
// Content is laid out rightPad columns narrower than width, and every line
// is cut to that, so nothing reaches the scrollbar column.
func (r *renderer) render(it blocklist.Item, width int, _ blocklist.Styles) []string {
	data, ok := it.Data.(blockData)
	if !ok {
		return nil
	}
	b := data.Block
	width = max(width-rightPad, 1)

	var lines []string
	switch b.Kind {
	case transcript.KindUser:
		lines = r.renderUser(b, width)
	case transcript.KindText:
		lines = r.md.Render(ansi.Sanitize(b.Text), width)
	case transcript.KindReasoning:
		lines = []string{r.fitLine(r.renderReasoning(data.Thinking, data.Duration, data.Frame), width)}
	case transcript.KindTool:
		lines = []string{r.fitLine(r.renderTool(b, data.Duration, data.Frame), width)}
	case transcript.KindSubagent:
		lines = []string{r.fitLine(r.renderSubagent(b, data.Frame), width)}
	case transcript.KindNotice:
		lines = []string{r.fitLine(r.renderNotice(b), width)}
	}

	if data.Card != "" {
		lines = append(lines, strings.Split(data.Card, "\n")...)
	}
	return lines
}

// fitLine cuts a one-line block (tool, reasoning, subagent, notice) to
// width with "…", so it ends rightPad columns before the scrollbar like
// wrapped bodies do. Multi-line bodies are laid out at width already.
func (r *renderer) fitLine(s string, width int) string {
	if ansi.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// renderUser renders a User block: "› <text>", wrapped in full, with
// a dim attachments line (R26) when present.
func (r *renderer) renderUser(b transcript.Block, width int) []string {
	// Expand tabs before wrapping, so the wrap measures what is drawn.
	text := strings.ReplaceAll(ansi.Sanitize(b.Text), "\t", "    ")
	wrapped := ansi.Wrap("› "+text, max(width, 1))
	lines := []string{r.set.Render.User.Render(wrapped)}
	if len(b.Attachments) > 0 {
		atts := make([]string, len(b.Attachments))
		for i, a := range b.Attachments {
			atts[i] = ansi.SanitizeLine(a)
		}
		lines = append(lines, r.set.Render.Dim.Render("  + "+strings.Join(atts, ", ")))
	}
	return lines
}

// renderReasoning renders "∴ thought for <dur>" once the model has
// finished thinking in this block, or "∴ thinking" while it still is and
// for a block with no measured duration (durations are live-only, so a
// resumed session has none). The reasoning text itself is never counted:
// Claude models return it empty unless a summary is requested. While
// thinking (Projection.Thinking: the model is still adding to this
// block), the running spinner replaces "∴". The Streaming flag is never
// consulted: transcript leaves it set on a block that is no longer the
// message's open block (until the step ends), so using it here would
// show a stale spinner on a superseded block.
func (r *renderer) renderReasoning(thinking bool, dur time.Duration, frame int) string {
	if thinking {
		return r.set.Render.Dim.Render(string(spinnerGlyph(frame)) + " thinking")
	}
	if dur <= 0 {
		return r.set.Render.Dim.Render("∴ thinking")
	}
	return r.set.Render.Dim.Render("∴ thought for " + thoughtFor(dur))
}

// thoughtFor formats a thinking duration: "4.2s" under a minute (bash's
// format), else whole seconds as "1m15s".
func thoughtFor(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Truncate(time.Second).String()
}

// renderTool renders one Tool block's line via toolLine, then applies the
// running spinner and the finished-state suffix and color (spec §5.3).
// The spinner replaces the default icon, or, for an icon toolLine itself
// picked (agent-browser's 🌐), is shown right after it instead.
func (r *renderer) renderTool(b transcript.Block, dur time.Duration, frame int) string {
	icon, name, summary, hasStatus := toolLine(b, dur)
	icon = ansi.SanitizeLine(icon)
	name = ansi.SanitizeLine(name)
	summary = ansi.SanitizeLine(summary)

	if b.State == transcript.StateRunning {
		switch icon {
		case defaultIcon:
			icon = string(spinnerGlyph(frame))
		default:
			summary = string(spinnerGlyph(frame)) + " " + summary
		}
	}

	line := icon + " "
	if name != "" {
		line += name + "  "
	}
	line += summary

	if b.Call.Name == "bash" && hasStatus && (b.State == transcript.StateError || bashFailed(b.Result)) {
		// bashLine already embedded "✗ exit N" or "✗ timed out" (it had
		// a real result to read); the generic error suffix styleState
		// would add is redundant. A non-zero exit is an OK tool result
		// (bash reports it in the output), but it still failed, so it
		// renders as an error too. Without hasStatus — no result yet
		// (settled by RunFailed/a failed message with no answer), or an
		// agent-browser call, which never embeds a status itself — the
		// generic suffix is the only indicator, so it must still apply.
		return r.set.Render.Error.Render(line)
	}
	return r.styleState(line, b.State)
}

// renderSubagent renders "↳ <agent>  <description>  <spinner> N tools ·
// <current>", or the finished state's glyph in place of the spinner
// portion once the task call is settled (spec §5.4).
func (r *renderer) renderSubagent(b transcript.Block, frame int) string {
	sub := b.Sub
	prefix := "↳ " + ansi.SanitizeLine(sub.Agent) + "  " + ansi.SanitizeLine(sub.Description) + "  "

	switch b.State {
	case transcript.StateOK:
		return r.set.Render.OK.Render(prefix + "✓")
	case transcript.StateError:
		return r.set.Render.Error.Render(prefix + "✗")
	case transcript.StateDenied:
		return r.set.Render.Denied.Render(prefix + "⊘")
	case transcript.StateCancelled:
		return r.set.Render.Dim.Render(prefix + "⊘")
	}

	status := fmt.Sprintf("%c %d tools", spinnerGlyph(frame), sub.Tools)
	if sub.Current != "" {
		status += " · " + ansi.SanitizeLine(sub.Current)
	}
	if b.State == transcript.StateAwaiting {
		return r.set.Render.Warn.Render(prefix + status + " ⚠")
	}
	if b.State == transcript.StatePending {
		return r.set.Render.Dim.Render(prefix + status)
	}
	return r.set.Render.Tool.Render(prefix + status)
}

// renderNotice renders a notice's headline (Title if set, else Text) dim,
// or red for LevelError. A compaction notice's full summary (Text, when
// Title is set) is shown in the details pane instead, not here.
func (r *renderer) renderNotice(b transcript.Block) string {
	text := b.Text
	if b.Title != "" {
		text = b.Title
	}
	text = ansi.SanitizeLine(text)
	if b.Level == transcript.LevelError {
		return r.set.Render.Error.Render(text)
	}
	return r.set.Render.Dim.Render(text)
}

// styleState colors line by state, appending the state's suffix glyph for
// every state but ok (spec §5.3): ok uses the tool color with no suffix,
// running uses the base tool color (its spinner already replaced the
// icon), and pending (loaded, unanswered) falls back to Dim.
func (r *renderer) styleState(line string, state transcript.ToolState) string {
	rs := r.set.Render
	switch state {
	case transcript.StateOK:
		return rs.OK.Render(line)
	case transcript.StateRunning:
		return rs.Tool.Render(line)
	case transcript.StateError:
		return rs.Error.Render(line + " ✗")
	case transcript.StateDenied:
		return rs.Denied.Render(line + " ⊘")
	case transcript.StateCancelled:
		return rs.Dim.Render(line + " ⊘")
	case transcript.StateAwaiting:
		return rs.Warn.Render(line + " ⚠")
	default: // pending
		return rs.Dim.Render(line)
	}
}

// spinnerGlyph returns the animated running-indicator glyph for frame,
// wrapping around spinnerFrames.
func spinnerGlyph(frame int) rune {
	runes := []rune(spinnerFrames)
	n := len(runes)
	i := frame % n
	if i < 0 {
		i += n
	}
	return runes[i]
}
