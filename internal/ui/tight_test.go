package ui

import (
	"slices"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// tightIDs returns the IDs of the transcript list's items laid out with
// no gap above them (they and the item above are both Compact), in order,
// walking a copy of the list.
func (ta *testApp) tightIDs() []string {
	l := ta.app.sess.main.list
	l.Top()
	var ids []string
	prev := false
	for i := range l.Len() {
		it, _ := l.Selected()
		if i > 0 && prev && it.Compact {
			ids = append(ids, it.ID)
		}
		prev = it.Compact
		l, _ = l.Update(keyPress("j"))
	}
	return ids
}

func TestTight_FinishedThinkingStacksWithToolLines(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "bash", `{"command":"ls"}`)
	ta.finishTool("m1", "c1", "bash", "a.go", false)
	ta.event(event.ReasoningDelta{Base: rootBase(), MessageID: "m2", Text: "hmm\nmore"})
	ta.fire()
	if got := ta.tightIDs(); len(got) != 0 {
		t.Fatalf("while thinking: tight = %v, want none (live thinking keeps its gaps)", got)
	}
	ta.startTool("m2", "e1", "edit", `{"path":"a.go","old_string":"a","new_string":"b"}`)
	if got, want := ta.tightIDs(), []string{"m/m2/0", "t/e1"}; !slices.Equal(got, want) {
		t.Errorf("after the edit starts: tight = %v, want %v", got, want)
	}
	rows := strings.Split(xansi.Strip(ta.view()), "\n")
	i := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, "bash  ls") })
	if i < 0 || i+2 >= len(rows) || !strings.Contains(rows[i+1], "∴ th") || !strings.Contains(rows[i+2], "edit  a.go") {
		t.Errorf("bash, thought, edit do not stack:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTight_ThinkingEndedByTheRunStacks(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "bash", `{"command":"ls"}`)
	ta.finishTool("m1", "c1", "bash", "a.go", false)
	ta.event(event.ReasoningDelta{Base: rootBase(), MessageID: "m2", Text: "hmm"})
	ta.fire()
	ta.event(event.RunFailed{Base: rootBase(), Err: "cancelled"})
	if got, want := ta.tightIDs(), []string{"m/m2/0"}; !slices.Equal(got, want) {
		t.Errorf("tight = %v, want %v", got, want)
	}
}

func TestTight_StoredThinkingBetweenToolsStacks(t *testing.T) {
	t.Parallel()
	msgs := groupMessages()             // u1, read c1, (∴, grep c2), text
	msgs[2].Parts[1].Call.Name = "edit" // a lone read, then ∴ and an edit: no group
	msgs[2].Parts[2].Result.Name = "edit"
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, msgs, nil))
	if got, want := ta.tightIDs(), []string{"m/a2/0", "t/c2"}; !slices.Equal(got, want) {
		t.Errorf("tight = %v, want %v (the reply keeps its gap)", got, want)
	}
}

func TestTight_LiveToolLinesStackWithoutGaps(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "w1", "write", `{"path":"a.go","content":"x"}`)
	ta.startTool("m1", "b1", "bash", `{"command":"ls"}`)
	if got, want := ta.tightIDs(), []string{"t/b1"}; !slices.Equal(got, want) {
		t.Fatalf("after write, bash: tight = %v, want %v (the write sits under the user block)", got, want)
	}
	ta.startTool("m1", "b2", "bash", `{"command":"pwd"}`)
	if got, want := ta.listIDs()[1:], []string{"t/w1", "g/b1"}; !slices.Equal(got, want) {
		t.Fatalf("after a second bash: list = %v, want <user> %v", got, want)
	}
	if got, want := ta.tightIDs(), []string{"g/b1"}; !slices.Equal(got, want) {
		t.Errorf("tight = %v, want %v", got, want)
	}
	ta.finishTool("m1", "w1", "write", "wrote a.go", false)
	ta.finishTool("m1", "b1", "bash", "a.go", false)
	ta.finishTool("m1", "b2", "bash", "/x", false)
	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "done"})
	ta.fire()
	if got, want := ta.tightIDs(), []string{"g/b1"}; !slices.Equal(got, want) {
		t.Errorf("after the reply: tight = %v, want %v (the reply keeps its gap)", got, want)
	}

	rows := strings.Split(xansi.Strip(ta.view()), "\n")
	i := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, "write  a.go") })
	if i < 0 || i+1 >= len(rows) || !strings.Contains(rows[i+1], "▸ explored · 2 commands") {
		t.Errorf("the group header is not right under the write line:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTight_ExpandedGroupMembersStack(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.sess.main.list.Select("g/c1")
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "t/c1", "m/a2/0", "t/c2", "m/a3/0"}; !slices.Equal(got, want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	if got, want := ta.tightIDs(), []string{"t/c1", "m/a2/0", "t/c2"}; !slices.Equal(got, want) {
		t.Errorf("tight = %v, want the members %v", got, want)
	}
}
