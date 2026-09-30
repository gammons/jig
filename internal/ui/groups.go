package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gammons/jig/internal/bubbles/ansi"
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
// A pending call (loaded, unanswered) adds nothing.
func (r *renderer) groupProblems(members []transcript.Block) string {
	var failed, denied, cancelled int
	for _, m := range members {
		switch m.State {
		case transcript.StateError:
			failed++
		case transcript.StateDenied:
			denied++
		case transcript.StateCancelled:
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

// groupTally counts members' calls as "N reads, N greps, N globs", in
// that order, singular for one, leaving out a tool with no calls.
func groupTally(members []transcript.Block) string {
	counts := map[string]int{}
	for _, m := range members {
		if m.Kind == transcript.KindTool && m.Call != nil {
			counts[m.Call.Name]++
		}
	}
	kinds := [...][3]string{{"read", "read", "reads"}, {"grep", "grep", "greps"}, {"glob", "glob", "globs"}}
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
// display order (several run at once in a parallel batch), or "".
func currentCall(members []transcript.Block) string {
	for i := len(members) - 1; i >= 0; i-- {
		m := members[i]
		if m.Kind == transcript.KindTool && m.Call != nil && m.State == transcript.StateRunning {
			_, name, summary, _ := toolLine(m, 0)
			return ansi.SanitizeLine(name + " " + summary)
		}
	}
	return ""
}
