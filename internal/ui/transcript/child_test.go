package transcript

import (
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

const (
	kid  core.SessionID = "ses_k"
	gkid core.SessionID = "ses_g"
	sib  core.SessionID = "ses_s"
)

func TestNewChild_OwnEventsBuildBlocks(t *testing.T) {
	p := NewChild(root, kid)
	if p.Self() != kid {
		t.Fatalf("Self() = %q, want %q", p.Self(), kid)
	}
	run(t, p, []step{
		{event.TextDelta{Base: sessBase(kid), MessageID: "k1", Text: "hello"}, []BlockID{"m/k1/0"}},
		{event.ReasoningDelta{Base: sessBase(kid), MessageID: "k1", Text: "hm"}, []BlockID{"m/k1/1"}},
		{event.ToolCallStarted{Base: sessBase(kid), MessageID: "k1", Call: *mkCall("c1", "bash", `{}`)}, []BlockID{"t/c1"}},
		{event.ToolCallFinished{Base: sessBase(kid), MessageID: "k1", Result: *mkResult("c1", "bash", "ok", false)}, []BlockID{"t/c1"}},
	})
	blocks := p.Blocks()
	if len(blocks) != 3 {
		t.Fatalf("len(Blocks) = %d, want 3: %+v", len(blocks), blocks)
	}
	wantKinds := []Kind{KindText, KindReasoning, KindTool}
	for i, k := range wantKinds {
		if blocks[i].Kind != k {
			t.Errorf("block %d Kind = %v, want %v", i, blocks[i].Kind, k)
		}
	}
	if blocks[2].State != StateOK {
		t.Errorf("tool block State = %q, want %q", blocks[2].State, StateOK)
	}
}

func TestNewChild_IgnoresRootAndSiblings(t *testing.T) {
	p := NewChild(root, kid)
	steps := []step{
		{event.TextDelta{Base: sessBase(root), MessageID: "m1", Text: "x"}, nil},
		{event.ToolCallStarted{Base: sessBase(sib), MessageID: "m1", Call: *mkCall("c9", "bash", `{}`)}, nil},
		{event.PermissionRequested{Base: sessBase(sib), RequestID: "p1", Tool: "bash", Subject: "ls", Call: core.ToolCall{ID: "c9", Name: "bash"}}, nil},
		{event.PermissionRequested{Base: sessBase(root), RequestID: "p2", Tool: "bash", Subject: "ls", Call: core.ToolCall{ID: "c8", Name: "bash"}}, nil},
	}
	run(t, p, steps)
	if n := len(p.Blocks()); n != 0 {
		t.Errorf("len(Blocks) = %d, want 0", n)
	}
	if got := p.Pending(); len(got) != 0 {
		t.Errorf("Pending() = %+v, want empty", got)
	}
}

func TestNewChild_GrandchildBecomesSubagentBlock(t *testing.T) {
	p := NewChild(root, kid)
	run(t, p, []step{
		{event.ToolCallStarted{Base: sessBase(kid), MessageID: "k1", Call: *mkCall("c2", "task", `{"agent":"general","description":"deeper"}`)}, []BlockID{"t/c2"}},
		{event.SubagentSpawned{Base: sessBase(kid), Child: gkid, CallID: "c2"}, []BlockID{"t/c2"}},
		{event.ToolCallStarted{Base: sessBase(gkid), MessageID: "g1", Call: *mkCall("c3", "bash", `{}`)}, []BlockID{"t/c2"}},
	})
	b := mustBlock(t, p, "t/c2")
	if b.Kind != KindSubagent {
		t.Fatalf("Kind = %v, want KindSubagent", b.Kind)
	}
	if b.Sub == nil || b.Sub.Child != gkid || b.Sub.Tools != 1 || b.Sub.Current != "bash" {
		t.Fatalf("Sub = %+v, want Child %q, Tools 1, Current bash", b.Sub, gkid)
	}
}

func TestNewChild_PermissionOnOwnToolBlock(t *testing.T) {
	p := NewChild(root, kid)
	run(t, p, []step{
		{event.ToolCallStarted{Base: sessBase(kid), MessageID: "k1", Call: *mkCall("c1", "bash", `{}`)}, []BlockID{"t/c1"}},
		{event.PermissionRequested{Base: sessBase(kid), RequestID: "p1", Tool: "bash", Subject: "ls", Call: core.ToolCall{ID: "c1"}}, []BlockID{"t/c1"}},
	})
	b := mustBlock(t, p, "t/c1")
	if b.Permission == nil || b.Permission.RequestID != "p1" || b.State != StateAwaiting {
		t.Fatalf("t/c1 = State %q Permission %+v, want awaiting p1", b.State, b.Permission)
	}
	pending := p.Pending()
	if len(pending) != 1 || pending[0].Block != "t/c1" || pending[0].Subagent != "" {
		t.Fatalf("Pending = %+v, want one entry on t/c1 with Subagent \"\"", pending)
	}
	run(t, p, []step{
		{event.PermissionResolved{Base: sessBase(kid), RequestID: "p1"}, []BlockID{"t/c1"}},
	})
	b = mustBlock(t, p, "t/c1")
	if b.Permission != nil {
		t.Errorf("after resolve: Permission = %+v, want nil", b.Permission)
	}
	if got := p.Pending(); len(got) != 0 {
		t.Errorf("after resolve: Pending = %+v, want empty", got)
	}
}

func TestNewChild_RootIDMustMatch(t *testing.T) {
	p := NewChild(root, kid)
	got := p.Apply(event.TextDelta{Base: event.Base{SessionID: kid, RootID: "other"}, MessageID: "k1", Text: "x"})
	if got != nil {
		t.Errorf("Apply = %q, want nil", got)
	}
	if n := len(p.Blocks()); n != 0 {
		t.Errorf("len(Blocks) = %d, want 0", n)
	}
}
