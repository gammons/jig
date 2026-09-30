package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/ui/transcript"
)

// nestIndent is the blank columns before each line of a member of an
// expanded group, setting it off under its header.
const nestIndent = 2

// groupData is a group header's render input (tool-call groups spec §4):
// its members (calls and absorbed reasoning, display order), their
// durations, and whether the group is shown expanded.
type groupData struct {
	Members []transcript.Block
	Durs    []time.Duration
	Open    bool
}

// renderGroup renders a group header: "<spinner> exploring · <tally>"
// while live, followed by " · <current call>" when collapsed; otherwise
// "▸ explored · <tally>" (▾ when expanded) followed by its failed,
// denied, and cancelled counts, each in its state's style.
func (r *renderer) renderGroup(g *groupData, frame int) string {
	rs := r.set.Render
	tally := groupTally(g.Members)
	if groupLive(g.Members) {
		line := string(spinnerGlyph(frame)) + " exploring · " + tally
		if cur := currentCall(g.Members); cur != "" && !g.Open {
			line += " · " + cur
		}
		return rs.Tool.Render(line)
	}
	icon := "▸"
	if g.Open {
		icon = "▾"
	}
	return rs.OK.Render(icon+" explored · "+tally) + r.groupProblems(g.Members)
}

// groupProblems renders the settled header's " · N failed ✗",
// " · N denied ⊘", and " · N cancelled ⊘" suffixes, each only when N > 0.
// A pending call (loaded, unanswered) adds nothing. A bash call that
// exited non-zero or timed out is an ok result but counts as failed, as
// its own line renders it.
func (r *renderer) groupProblems(members []transcript.Block) string {
	var failed, denied, cancelled int
	for _, m := range members {
		switch {
		case m.State == transcript.StateError,
			m.State == transcript.StateOK && m.Call != nil && m.Call.Name == "bash" && bashFailed(m.Result):
			failed++
		case m.State == transcript.StateDenied:
			denied++
		case m.State == transcript.StateCancelled:
			cancelled++
		}
	}
	rs := r.set.Render
	var out string
	if failed > 0 {
		out += rs.Error.Render(fmt.Sprintf(" · %d failed ✗", failed))
	}
	if denied > 0 {
		out += rs.Denied.Render(fmt.Sprintf(" · %d denied ⊘", denied))
	}
	if cancelled > 0 {
		out += rs.Dim.Render(fmt.Sprintf(" · %d cancelled ⊘", cancelled))
	}
	return out
}

// groupTally counts members' calls as "N reads, N greps, N globs, N
// commands" (bash), in that order, singular for one, leaving out a tool
// with no calls.
func groupTally(members []transcript.Block) string {
	counts := map[string]int{}
	for _, m := range members {
		if m.Kind == transcript.KindTool && m.Call != nil {
			counts[m.Call.Name]++
		}
	}
	kinds := [...][3]string{
		{"read", "read", "reads"}, {"grep", "grep", "greps"}, {"glob", "glob", "globs"}, {"bash", "command", "commands"},
	}
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		switch n := counts[k[0]]; {
		case n == 1:
			parts = append(parts, "1 "+k[1])
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, k[2]))
		}
	}
	return strings.Join(parts, ", ")
}

// groupLive reports whether a group is still working: a member call is
// running or awaiting permission, or a member is still thinking.
func groupLive(members []transcript.Block) bool {
	for _, m := range members {
		if m.Thinking || m.State == transcript.StateRunning || m.State == transcript.StateAwaiting {
			return true
		}
	}
	return false
}

// currentCall is "<tool> <summary>" for the last running member call in
// display order (several run at once in a parallel batch), or "". An
// agent-browser command has no tool name, so it is just its summary.
func currentCall(members []transcript.Block) string {
	for i := len(members) - 1; i >= 0; i-- {
		m := members[i]
		if m.Kind == transcript.KindTool && m.Call != nil && m.State == transcript.StateRunning {
			_, name, summary, _ := toolLine(m, 0)
			if name == "" {
				return ansi.SanitizeLine(summary)
			}
			return ansi.SanitizeLine(name + " " + summary)
		}
	}
	return ""
}

// groupDetails is a group's details content (tool-call groups spec
// §5.2): the header "group · <tally>", then one line per member in order,
// a call's plain one-liner or a reasoning block's label. It needs no port.
func groupDetails(members []transcript.Block, durs []time.Duration) details.Content {
	lines := make([]string, 0, len(members))
	for i, m := range members {
		if m.Kind == transcript.KindReasoning {
			lines = append(lines, reasoningLabel(durs[i]))
			continue
		}
		lines = append(lines, ansi.SanitizeLine(oneLiner(m)))
	}
	return details.Content{Header: header("group", groupTally(members)), Lines: lines}
}

// reasoningLabel is a finished reasoning block's line: "∴ thought for
// <dur>", or "∴ thinking" with no measured duration.
func reasoningLabel(d time.Duration) string {
	if d <= 0 {
		return "∴ thinking"
	}
	return "∴ thought for " + thoughtFor(d)
}

// groupYank is what y copies for a group: each member call's subject, one
// per line in order (a read's path, a grep's or glob's pattern, a bash
// call's command with its lines joined by spaces), sanitized. Reasoning
// is skipped.
func groupYank(members []transcript.Block) string {
	subjects := make([]string, 0, len(members))
	for _, m := range members {
		if m.Kind != transcript.KindTool || m.Call == nil {
			continue
		}
		if m.Call.Name == "bash" {
			subjects = append(subjects, ansi.SanitizeLine(yankTool(m)))
			continue
		}
		var in struct {
			Path    string `json:"path"`
			Pattern string `json:"pattern"`
		}
		_ = json.Unmarshal(m.Call.Input, &in)
		subject := in.Pattern
		if m.Call.Name == "read" {
			subject = in.Path
		}
		subjects = append(subjects, ansi.SanitizeLine(subject))
	}
	return strings.Join(subjects, "\n")
}
