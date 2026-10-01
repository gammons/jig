package transcript

import (
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

func sessBase(s core.SessionID) event.Base { return event.Base{SessionID: s, RootID: root} }

const taskInput = `{"agent":"explore","description":"find x","prompt":"p"}`

func taskStarted() event.ToolCallStarted {
	return event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: *mkCall("c1", "task", taskInput)}
}

func spawned(parent, child core.SessionID, agent, callID string) event.SubagentSpawned {
	return event.SubagentSpawned{Base: sessBase(parent), Child: child, Agent: agent, Description: "d", CallID: callID}
}

func toolStarted(s core.SessionID, id, name string) event.ToolCallStarted {
	return event.ToolCallStarted{Base: sessBase(s), MessageID: "mx", Call: *mkCall(id, name, `{}`)}
}

func toolFinished(s core.SessionID, id, name string) event.ToolCallFinished {
	return event.ToolCallFinished{Base: sessBase(s), MessageID: "mx", Result: *mkResult(id, name, "ok", false)}
}

func permRequested(s core.SessionID, req, callID string) event.PermissionRequested {
	return event.PermissionRequested{Base: sessBase(s), RequestID: req, Tool: "bash", Subject: "ls", Call: core.ToolCall{ID: callID, Name: "bash"}}
}

func permResolved(s core.SessionID, req string) event.PermissionResolved {
	return event.PermissionResolved{Base: sessBase(s), RequestID: req}
}

func mustBlock(t *testing.T, p *Projection, id BlockID) Block {
	t.Helper()
	b, ok := p.Block(id)
	if !ok {
		t.Fatalf("no block %q", id)
	}
	return b
}

func TestApply_SubagentLifecycle(t *testing.T) {
	p := New(root)
	tests := []struct {
		name  string
		ev    event.Event
		want  []BlockID
		state ToolState
		sub   Subagent
	}{
		{"task call starts", taskStarted(), []BlockID{"t/c1"}, StateRunning,
			Subagent{Agent: "explore", Description: "find x"}},
		{"child spawned", event.SubagentSpawned{Base: rootBase(), Child: "k1", Agent: "explore", Model: "anthropic/haiku", Description: "find x", CallID: "c1"},
			[]BlockID{"t/c1"}, StateRunning, Subagent{Child: "k1", Agent: "explore", Model: "anthropic/haiku", Description: "find x"}},
		{"child tool starts", toolStarted("k1", "r1", "read"), []BlockID{"t/c1"}, StateRunning,
			Subagent{Child: "k1", Agent: "explore", Model: "anthropic/haiku", Description: "find x", Tools: 1, Current: "read"}},
		{"child tool finishes", toolFinished("k1", "r1", "read"), []BlockID{"t/c1"}, StateRunning,
			Subagent{Child: "k1", Agent: "explore", Model: "anthropic/haiku", Description: "find x", Tools: 1}},
		{"second child tool starts", toolStarted("k1", "r2", "grep"), []BlockID{"t/c1"}, StateRunning,
			Subagent{Child: "k1", Agent: "explore", Model: "anthropic/haiku", Description: "find x", Tools: 2, Current: "grep"}},
		{"task call finishes", event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: *mkResult("c1", "task", "done", false)},
			[]BlockID{"t/c1"}, StateOK, Subagent{Child: "k1", Agent: "explore", Model: "anthropic/haiku", Description: "find x", Tools: 2}},
	}
	version := 0
	for _, tt := range tests {
		got := p.Apply(tt.ev)
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%s: Apply = %q, want %q", tt.name, got, tt.want)
		}
		b := mustBlock(t, p, "t/c1")
		if b.State != tt.state || !reflect.DeepEqual(*b.Sub, tt.sub) {
			t.Errorf("%s: State %q Sub %+v, want %q %+v", tt.name, b.State, *b.Sub, tt.state, tt.sub)
		}
		if b.Version != version+1 {
			t.Errorf("%s: Version = %d, want %d", tt.name, b.Version, version+1)
		}
		version = b.Version
	}
}

func TestApply_NestedSubagentRoutesToOwner(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{taskStarted(), []BlockID{"t/c1"}},
		{spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}},
		// A grandchild's spawn changes nothing visible; it only routes k2.
		{spawned("k1", "k2", "general", "kc1"), nil},
		{spawned("k2", "k3", "general", "kc2"), nil},
		{toolStarted("k2", "x1", "bash"), []BlockID{"t/c1"}},
		{toolStarted("k3", "x2", "edit"), []BlockID{"t/c1"}},
		{toolStarted("k1", "x3", "read"), []BlockID{"t/c1"}},
		{toolFinished("k3", "x2", "edit"), []BlockID{"t/c1"}},
		// A spawn under an unknown parent is ignored, and so is its child.
		{spawned("stranger", "k9", "general", "zz"), nil},
		{toolStarted("k9", "x4", "read"), nil},
	})
	b := mustBlock(t, p, "t/c1")
	if b.Sub.Tools != 3 || b.Sub.Current != "" || b.Sub.Child != "k1" {
		t.Errorf("Sub = %+v, want Tools 3, Current \"\", Child k1", *b.Sub)
	}
	if n := len(p.Blocks()); n != 1 {
		t.Errorf("len(Blocks) = %d, want 1", n)
	}
}

func TestApply_DescendantToolCallsNeverCreateBlocks(t *testing.T) {
	tests := []struct {
		name string
		ev   event.Event
	}{
		{"tool started", toolStarted("k1", "r1", "read")},
		{"tool finished", toolFinished("k1", "r1", "read")},
		{"tool started with the parent's call ID", toolStarted("k1", "c1", "read")},
		{"text delta", event.TextDelta{Base: sessBase("k1"), MessageID: "km", Text: "x"}},
		{"reasoning delta", event.ReasoningDelta{Base: sessBase("k1"), MessageID: "km", Text: "x"}},
		{"step finished", event.StepFinished{Base: sessBase("k1"), MessageID: "km"}},
		{"run finished", event.RunFinished{Base: sessBase("k1"), MessageID: "km"}},
		{"run failed", event.RunFailed{Base: sessBase("k1"), Err: "boom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(root)
			run(t, p, []step{{taskStarted(), []BlockID{"t/c1"}}, {spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}}})
			before := len(p.Blocks())
			p.Apply(tt.ev)
			if after := len(p.Blocks()); after != before {
				t.Errorf("len(Blocks) = %d, want %d", after, before)
			}
			if b := mustBlock(t, p, "t/c1"); b.State != StateRunning {
				t.Errorf("c1 State = %q, want running", b.State)
			}
		})
	}
}

func TestApply_RootPermissionAwaitsToolBlock(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{toolStarted(root, "t1", "bash"), []BlockID{"t/t1"}},
		{permRequested(root, "p1", "t1"), []BlockID{"t/t1"}},
	})
	b := mustBlock(t, p, "t/t1")
	if b.State != StateAwaiting || b.Permission == nil || b.Permission.RequestID != "p1" {
		t.Fatalf("t1 = State %q Permission %+v, want awaiting p1", b.State, b.Permission)
	}
	want := []PendingPermission{{
		RequestID: "p1", Session: root, Tool: "bash", Subject: "ls",
		Call: core.ToolCall{ID: "t1", Name: "bash"}, Block: "t/t1",
	}}
	if got := p.Pending(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Pending = %+v, want %+v", got, want)
	}
	run(t, p, []step{
		{permResolved(root, "p1"), []BlockID{"t/t1"}},
		{permResolved(root, "p1"), nil}, // already resolved
	})
	b = mustBlock(t, p, "t/t1")
	if b.State != StateRunning || b.Permission != nil {
		t.Errorf("t1 = State %q Permission %+v, want running, nil", b.State, b.Permission)
	}
	if got := p.Pending(); len(got) != 0 {
		t.Errorf("Pending = %+v, want empty", got)
	}
}

func TestApply_PermissionForUnknownCallIsPendingWithoutBlock(t *testing.T) {
	tests := []struct {
		name    string
		session core.SessionID
	}{
		{"root, unknown call", root},
		{"unowned descendant", "k7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(root)
			run(t, p, []step{{permRequested(tt.session, "p1", "zz"), nil}})
			if n := len(p.Blocks()); n != 0 {
				t.Errorf("len(Blocks) = %d, want 0", n)
			}
			got := p.Pending()
			if len(got) != 1 || got[0].Block != "" || got[0].RequestID != "p1" || got[0].Session != tt.session {
				t.Fatalf("Pending = %+v, want one unblocked p1 from %s", got, tt.session)
			}
			run(t, p, []step{{permResolved(tt.session, "p1"), nil}})
			if got := p.Pending(); len(got) != 0 {
				t.Errorf("Pending = %+v, want empty", got)
			}
		})
	}
}

func TestApply_SubagentPermissionMarksOwner(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{taskStarted(), []BlockID{"t/c1"}},
		{spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}},
		{spawned("k1", "k2", "general", "kc"), nil},
		{permRequested("k1", "p1", "x1"), []BlockID{"t/c1"}},
		{permRequested("k2", "p2", "x2"), nil}, // listed; c1 keeps showing p1
	})
	b := mustBlock(t, p, "t/c1")
	if b.State != StateAwaiting || b.Permission == nil || b.Permission.RequestID != "p1" ||
		b.Permission.Subagent != "explore" || b.Permission.Block != "t/c1" || b.Permission.Session != "k1" {
		t.Fatalf("c1 = State %q Permission %+v, want awaiting p1 from explore", b.State, b.Permission)
	}
	pending := p.Pending()
	if len(pending) != 2 || pending[1].Subagent != "general" || pending[1].Block != "t/c1" {
		t.Fatalf("Pending = %+v, want p1 and p2 (general) on c1", pending)
	}
	// Resolving the shown request shows the next one for the same block.
	run(t, p, []step{{permResolved("k1", "p1"), []BlockID{"t/c1"}}})
	if b = mustBlock(t, p, "t/c1"); b.State != StateAwaiting || b.Permission == nil || b.Permission.RequestID != "p2" {
		t.Fatalf("after p1: State %q Permission %+v, want awaiting p2", b.State, b.Permission)
	}
	run(t, p, []step{{permResolved("k2", "p2"), []BlockID{"t/c1"}}})
	if b = mustBlock(t, p, "t/c1"); b.State != StateRunning || b.Permission != nil {
		t.Errorf("after p2: State %q Permission %+v, want running, nil", b.State, b.Permission)
	}
	if got := p.Pending(); len(got) != 0 {
		t.Errorf("Pending = %+v, want empty", got)
	}
}

func TestApply_ResolvingAHiddenRequestLeavesBlockAlone(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{taskStarted(), []BlockID{"t/c1"}},
		{spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}},
		{permRequested("k1", "p1", "x1"), []BlockID{"t/c1"}},
		{permRequested("k1", "p2", "x2"), nil},
		{permResolved("k1", "p2"), nil}, // c1 still shows p1
	})
	if b := mustBlock(t, p, "t/c1"); b.Permission == nil || b.Permission.RequestID != "p1" {
		t.Errorf("Permission = %+v, want p1", b.Permission)
	}
	if got := p.Pending(); len(got) != 1 || got[0].RequestID != "p1" {
		t.Errorf("Pending = %+v, want [p1]", got)
	}
}

func TestLoad_ResumeWithTaskChild(t *testing.T) {
	first := mkCall("t0", "task", taskInput)
	firstRes := mkResult("t0", "task", "<task_result session_id=\"ses_old\">\ndone\n</task_result>", false)
	resumed := mkCall("t1", "task", `{"agent":"explore","description":"again","prompt":"p","session_id":"ses_old"}`)
	msgs := []core.Message{asstMsg("m1", core.StatusComplete, callPart(first), resultPart(firstRes), callPart(resumed))}

	p := New(root)
	p.Load(msgs)
	run(t, p, []step{
		{spawned(root, "ses_old", "explore", "t1"), []BlockID{"t/t1"}},
		{toolStarted("ses_old", "r1", "read"), []BlockID{"t/t1"}},
	})
	if n := len(p.Blocks()); n != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", n)
	}
	b := mustBlock(t, p, "t/t1")
	if b.Sub.Child != "ses_old" || b.Sub.Tools != 1 || b.Version != 3 {
		t.Errorf("t1 = Sub %+v Version %d, want Child ses_old, Tools 1, Version 3", *b.Sub, b.Version)
	}
	if b0 := mustBlock(t, p, "t/t0"); b0.Sub.Tools != 0 {
		t.Errorf("t0 Tools = %d, want 0", b0.Sub.Tools)
	}

	// A child named by a loaded block routes even without a new spawn.
	q := New(root)
	q.Load(msgs)
	run(t, q, []step{{toolStarted("ses_old", "r1", "read"), []BlockID{"t/t1"}}})
}

func TestLoad_KeepsLiveSubagentsAndPermissions(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{taskStarted(), []BlockID{"t/c1"}},
		{spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}},
		{permRequested("k1", "p1", "x1"), []BlockID{"t/c1"}},
	})
	// The step that called c1 is stored, but its result is not yet.
	p.Load([]core.Message{asstMsg("m1", core.StatusComplete, callPart(mkCall("c1", "task", taskInput)))})
	b := mustBlock(t, p, "t/c1")
	if b.Permission == nil || b.Permission.RequestID != "p1" {
		t.Errorf("after Load: Permission = %+v, want p1", b.Permission)
	}
	if got := p.Pending(); len(got) != 1 || got[0].Block != "t/c1" {
		t.Errorf("after Load: Pending = %+v, want [p1 on c1]", got)
	}
	run(t, p, []step{
		{toolStarted("k1", "r1", "read"), []BlockID{"t/c1"}},
		{permResolved("k1", "p1"), []BlockID{"t/c1"}},
	})
	if b = mustBlock(t, p, "t/c1"); b.Permission != nil || b.Sub.Tools != 1 {
		t.Errorf("c1 = Permission %+v Sub %+v, want nil, Tools 1", b.Permission, *b.Sub)
	}
}

func TestAddUser_AppendsPendingUserBlock(t *testing.T) {
	p := New(root)
	run(t, p, []step{{textDelta("m1", "hi"), []BlockID{"m/m1/0"}}})
	atts := []string{"/w/a.png"}
	id0 := p.AddUser("first", atts)
	id1 := p.AddUser("second", nil)
	atts[0] = "/mutated"
	if id0 != "u/pending/0" || id1 != "u/pending/1" {
		t.Fatalf("IDs = %q, %q; want u/pending/0, u/pending/1", id0, id1)
	}
	want := []Block{
		{ID: "m/m1/0", Kind: KindText, MessageID: "m1", Text: "hi", Streaming: true},
		{ID: "u/pending/0", Kind: KindUser, Text: "first", Attachments: []string{"/w/a.png"}},
		{ID: "u/pending/1", Kind: KindUser, Text: "second"},
	}
	assertBlocks(t, p.Blocks(), want)
	for _, id := range []BlockID{id0, id1} {
		if b := mustBlock(t, p, id); b.Version != 1 {
			t.Errorf("%s: Version = %d, want 1", id, b.Version)
		}
	}

	p.Load([]core.Message{userMsg("m0", textPart("first"))})
	assertBlocks(t, p.Blocks(), []Block{{ID: "u/m0", Kind: KindUser, MessageID: "m0", Text: "first"}})
	// IDs are never reused after a Load.
	if id := p.AddUser("third", nil); id != "u/pending/2" {
		t.Errorf("after Load: ID = %q, want u/pending/2", id)
	}
}

func TestDropUser_RemovesOnlyPendingUserBlocks(t *testing.T) {
	p := New(root)
	p.Load([]core.Message{{ID: "u1", SessionID: root, Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "stored"}}}})
	a := p.AddUser("a", nil)
	b := p.AddUser("b", nil)
	if p.DropUser("u/u1") {
		t.Error("DropUser removed a stored user block")
	}
	if !p.DropUser(a) {
		t.Fatal("DropUser(pending) = false")
	}
	if p.DropUser(a) {
		t.Error("DropUser twice = true")
	}
	var ids []BlockID
	for _, bl := range p.Blocks() {
		ids = append(ids, bl.ID)
	}
	if len(ids) != 2 || ids[0] != "u/u1" || ids[1] != b {
		t.Fatalf("blocks = %v, want [u/u1 %s]", ids, b)
	}
	if got, ok := p.Block(b); !ok || got.Text != "b" {
		t.Errorf("Block(%s) = %+v, %v after the drop", b, got, ok)
	}
	if id := p.AddUser("c", nil); id != "u/pending/2" {
		t.Errorf("next AddUser = %s, want u/pending/2 (ordinals never reused)", id)
	}
}
