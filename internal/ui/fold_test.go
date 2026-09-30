package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

func foldTool(id, name string, st transcript.ToolState) transcript.Block {
	return transcript.Block{ID: transcript.BlockID("t/" + id), Kind: transcript.KindTool, State: st, Call: &core.ToolCall{ID: id, Name: name}}
}

// foldBlocks: a user block, a read, reasoning, a grep, and a reply. The
// read, reasoning, and grep form group g/c1.
func foldBlocks() []transcript.Block {
	return []transcript.Block{
		{ID: "u/u1", Kind: transcript.KindUser},
		foldTool("c1", "read", transcript.StateOK),
		{ID: "m/a2/0", Kind: transcript.KindReasoning},
		foldTool("c2", "grep", transcript.StateOK),
		{ID: "m/a3/0", Kind: transcript.KindText},
	}
}

// entryNames renders entries as "<kind> <id>" for readable comparisons.
func entryNames(es []foldEntry) []string {
	kinds := map[entryKind]string{entryPlain: "plain", entryHeader: "header", entryNested: "nested"}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = fmt.Sprintf("%s %s", kinds[e.kind], e.id)
	}
	return out
}

func wantEntries(t *testing.T, got []foldEntry, want ...string) {
	t.Helper()
	if names := entryNames(got); !slices.Equal(names, want) {
		t.Errorf("layout = %q\nwant     %q", names, want)
	}
}

func TestFold_CollapsedByDefault(t *testing.T) {
	t.Parallel()
	var f foldState
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_ToggleHeaderExpandsAndCollapses(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	if g, ok := f.toggle("g/c1"); !ok || g != "g/c1" {
		t.Fatalf("toggle(g/c1) = %q, %v, want g/c1, true", g, ok)
	}
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "nested t/c1", "nested m/a2/0", "nested t/c2", "plain m/a3/0")
	f.toggle("g/c1")
	got, _ = f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_ToggleOnAMemberCollapsesItsGroup(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	f.toggle("g/c1")
	f.regroup(foldBlocks())
	if g, ok := f.toggle("t/c2"); !ok || g != "g/c1" {
		t.Fatalf("toggle(t/c2) = %q, %v, want g/c1, true", g, ok)
	}
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_ToggleOutsideAGroupDoesNothing(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	for _, id := range []transcript.BlockID{"m/a3/0", "u/u1", "t/nope"} {
		if g, ok := f.toggle(id); ok {
			t.Errorf("toggle(%s) = %q, true, want false", id, g)
		}
	}
}

func TestFold_SearchForcesOpenAndRestores(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	f.search = true
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "nested t/c1", "nested m/a2/0", "nested t/c2", "plain m/a3/0")
	f.search = false
	got, _ = f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_RegroupReportsRestructure(t *testing.T) {
	t.Parallel()
	all := foldBlocks()
	var f foldState
	steps := []struct {
		n    int
		want bool
		why  string
	}{
		{2, false, "first layout"},
		{3, false, "reasoning appended after a lone read"},
		{4, true, "the grep forms a group: t/c1 and the reasoning leave the list"},
		{5, false, "a reply appended after the group"},
	}
	for _, s := range steps {
		if _, got := f.regroup(all[:s.n]); got != s.want {
			t.Errorf("%s: regroup = %v, want %v", s.why, got, s.want)
		}
	}
}

func TestFold_AbsorbedReasoningInAnOpenGroupRestructures(t *testing.T) {
	t.Parallel()
	blocks := []transcript.Block{
		foldTool("c1", "read", transcript.StateOK),
		foldTool("c2", "read", transcript.StateOK),
		{ID: "m/x/0", Kind: transcript.KindReasoning},
		foldTool("c3", "read", transcript.StateOK),
	}
	var f foldState
	f.regroup(blocks[:2])
	f.toggle("g/c1")
	if _, got := f.regroup(blocks[:3]); got {
		t.Error("reasoning after an open group: regroup = true, want false (it is appended as a plain item)")
	}
	if _, got := f.regroup(blocks); !got {
		t.Error("the next call absorbs the reasoning (plain → nested): regroup = false, want true")
	}
}

func TestFold_GroupOfAndMembers(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	if g, ok := f.groupOf("m/a2/0"); !ok || g != "g/c1" {
		t.Errorf("groupOf(m/a2/0) = %q, %v, want g/c1", g, ok)
	}
	if _, ok := f.groupOf("g/c1"); ok {
		t.Error("groupOf(a header) = true, want false")
	}
	if m, ok := f.members("g/c1"); !ok || !slices.Equal(m, []transcript.BlockID{"t/c1", "m/a2/0", "t/c2"}) {
		t.Errorf("members(g/c1) = %v, %v", m, ok)
	}
	if _, ok := f.members("t/c1"); ok {
		t.Error("members(a member) = true, want false")
	}
}
