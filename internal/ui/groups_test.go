package ui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/transcript"
)

// gm is group member call id of tool name with input in state st; an ok
// call gets a one-line result (a read's "1: package a", a search's one
// match).
func gm(id, name, input string, st transcript.ToolState) transcript.Block {
	b := transcript.Block{ID: transcript.BlockID("t/" + id), Kind: transcript.KindTool, State: st, Call: toolCall(id, name, input)}
	if st == transcript.StateOK {
		out := "a.go:1: match"
		if name == "read" {
			out = "1: package a"
		}
		b.Result = toolResult(id, name, out, false)
	}
	return b
}

// headerOf is the blockData of group g1's header showing g.
func headerOf(g groupData, frame int) blockData {
	return blockData{Block: transcript.Block{ID: "g/c1"}, Group: &g, Frame: frame}
}

func TestRender_GroupHeader(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok, run := transcript.StateOK, transcript.StateRunning
	readA := gm("c1", "read", `{"path":"a.go"}`, ok)
	readB := gm("c2", "read", `{"path":"b.go"}`, ok)
	thinking := transcript.Block{ID: "m/x/0", Kind: transcript.KindReasoning, Thinking: true}
	tests := []struct {
		name string
		g    groupData
		want string
	}{
		{"live, collapsed, shows the running call",
			groupData{Members: []transcript.Block{readA, gm("c3", "grep", `{"pattern":"TODO"}`, run)}},
			`⠋ exploring · 1 read, 1 grep · grep "TODO"`},
		{"live, expanded: no current call",
			groupData{Members: []transcript.Block{readA, gm("c3", "grep", `{"pattern":"TODO"}`, run)}, Open: true},
			`⠋ exploring · 1 read, 1 grep`},
		{"live with nothing running: no current call",
			groupData{Members: []transcript.Block{readA, readB, thinking}},
			`⠋ exploring · 2 reads`},
		{"parallel batch shows the last running call",
			groupData{Members: []transcript.Block{gm("c1", "read", `{"path":"a.go"}`, run), gm("c2", "read", `{"path":"b.go"}`, run)}},
			`⠋ exploring · 2 reads · read b.go`},
		{"awaiting permission counts as live",
			groupData{Members: []transcript.Block{readA, gm("c2", "read", `{"path":"/etc/hosts"}`, transcript.StateAwaiting)}, Open: true},
			`⠋ exploring · 2 reads`},
		{"settled, collapsed",
			groupData{Members: []transcript.Block{readA, readB, gm("c3", "grep", `{"pattern":"x"}`, ok), gm("c4", "glob", `{"pattern":"*.go"}`, ok)}},
			`▸ explored · 2 reads, 1 grep, 1 glob`},
		{"settled, expanded",
			groupData{Members: []transcript.Block{readA, readB}, Open: true},
			`▾ explored · 2 reads`},
		{"problems follow the tally",
			groupData{Members: []transcript.Block{
				gm("c1", "read", `{"path":"a.go"}`, transcript.StateError), gm("c2", "grep", `{"pattern":"x"}`, transcript.StateDenied),
				gm("c3", "glob", `{"pattern":"*"}`, transcript.StateCancelled), readB}},
			`▸ explored · 2 reads, 1 grep, 1 glob · 1 failed ✗ · 1 denied ⊘ · 1 cancelled ⊘`},
		{"interrupted (pending) calls add nothing",
			groupData{Members: []transcript.Block{gm("c1", "read", `{"path":"a.go"}`, transcript.StatePending), readB}},
			`▸ explored · 2 reads`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := xansi.Strip(renderOne(t, r, headerOf(tt.g, 0), 120)); got != tt.want {
				t.Errorf("header = %q\nwant     %q", got, tt.want)
			}
		})
	}
}

func TestRender_GroupHeaderStyles(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	rs := set.Render
	ok := transcript.StateOK
	readA, readB := gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "read", `{"path":"b.go"}`, ok)

	if got, want := renderOne(t, r, headerOf(groupData{Members: []transcript.Block{readA, readB}}, 0), 120),
		rs.OK.Render("▸ explored · 2 reads"); got != want {
		t.Errorf("settled header = %q, want the OK style %q", got, want)
	}
	failed := gm("c1", "read", `{"path":"a.go"}`, transcript.StateError)
	if got, want := renderOne(t, r, headerOf(groupData{Members: []transcript.Block{failed, readB}}, 0), 120),
		rs.OK.Render("▸ explored · 2 reads")+rs.Error.Render(" · 1 failed ✗"); got != want {
		t.Errorf("header with a failure = %q, want %q", got, want)
	}
	running := gm("c2", "read", `{"path":"b.go"}`, transcript.StateRunning)
	if got, want := renderOne(t, r, headerOf(groupData{Members: []transcript.Block{readA, running}}, 0), 120),
		rs.Tool.Render("⠋ exploring · 2 reads · read b.go"); got != want {
		t.Errorf("live header = %q, want the Tool style %q", got, want)
	}
}

func TestRender_GroupHeaderFitsAndSanitizes(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok := transcript.StateOK
	wide := headerOf(groupData{Members: []transcript.Block{gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "grep", `{"pattern":"x"}`, ok)}}, 0)
	out := renderOne(t, r, wide, 24)
	if w := ansi.Width(out); w > 24-rightPad || !strings.HasSuffix(xansi.Strip(out), "…") {
		t.Errorf("header at width 24 = %q (width %d), want ≤ %d cells ending in …", xansi.Strip(out), w, 24-rightPad)
	}
	hostile := headerOf(groupData{Members: []transcript.Block{
		gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "read", `{"path":"a\u001b[2Jb.go"}`, transcript.StateRunning)}}, 0)
	out = renderOne(t, r, hostile, 120)
	if strings.Contains(out, "\x1b[2J") || !strings.Contains(xansi.Strip(out), "read ab.go") {
		t.Errorf("header = %q, want the escape removed and \"read ab.go\" kept", out)
	}
}

func TestRender_NestedMemberIsIndented(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok := transcript.StateOK
	out := renderOne(t, r, blockData{Block: gm("c1", "read", `{"path":"a.go"}`, ok), Nested: true}, 80)
	if got := xansi.Strip(out); got != "  ▸ read  a.go · 1 lines" {
		t.Errorf("nested member = %q, want %q", got, "  ▸ read  a.go · 1 lines")
	}
	long := blockData{Block: gm("c2", "read", `{"path":"`+strings.Repeat("x", 200)+`"}`, ok), Nested: true}
	for _, line := range strings.Split(renderOne(t, r, long, 40), "\n") {
		if ansi.Width(line) > 40-rightPad || !strings.HasPrefix(xansi.Strip(line), "  ") {
			t.Errorf("nested line %q: want indented and ≤ %d cells", xansi.Strip(line), 40-rightPad)
		}
	}
	carded := blockData{Block: gm("c3", "read", `{"path":"a.go"}`, transcript.StateAwaiting), Nested: true, Card: "CARD"}
	lines := strings.Split(renderOne(t, r, carded, 80), "\n")
	if lines[len(lines)-1] != "CARD" {
		t.Errorf("nested member's card line = %q, want it unindented", lines[len(lines)-1])
	}
}

func TestRender_GoldenGroups(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok := transcript.StateOK
	readA, readB := gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "read", `{"path":"b.go"}`, ok)
	items := []blockData{
		headerOf(groupData{Members: []transcript.Block{readA, gm("c3", "grep", `{"pattern":"TODO"}`, transcript.StateRunning)}}, 3),
		headerOf(groupData{Members: []transcript.Block{readA, readB, gm("c3", "grep", `{"pattern":"TODO"}`, ok)}}, 0),
		headerOf(groupData{Members: []transcript.Block{
			gm("c1", "read", `{"path":"a.go"}`, transcript.StateError), gm("c2", "grep", `{"pattern":"x"}`, transcript.StateDenied),
			gm("c3", "glob", `{"pattern":"*"}`, transcript.StateCancelled), readB}}, 0),
		headerOf(groupData{Members: []transcript.Block{readA, readB}, Open: true}, 0),
		{Block: readA, Nested: true},
		{Block: transcript.Block{ID: "m/x/0", Kind: transcript.KindReasoning}, Nested: true},
		{Block: readB, Nested: true},
	}
	lines := make([]string, 0, len(items))
	for _, it := range items {
		lines = append(lines, renderOne(t, r, it, 80))
	}
	golden.Assert(t, "render_groups", strings.Join(lines, "\n"))
}
