package transcript

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

type step struct {
	ev   event.Event
	want []BlockID
}

// run applies each step's event to p, checking the returned IDs.
func run(t *testing.T, p *Projection, steps []step) {
	t.Helper()
	for i, s := range steps {
		got := p.Apply(s.ev)
		if !reflect.DeepEqual(got, s.want) {
			t.Fatalf("step %d (%T): Apply = %q, want %q", i, s.ev, got, s.want)
		}
	}
}

func started(msg string) event.MessageStarted {
	return event.MessageStarted{Base: rootBase(), MessageID: core.MessageID(msg)}
}

func textDelta(msg, s string) event.TextDelta {
	return event.TextDelta{Base: rootBase(), MessageID: core.MessageID(msg), Text: s}
}

func reasonDelta(msg, s string) event.ReasoningDelta {
	return event.ReasoningDelta{Base: rootBase(), MessageID: core.MessageID(msg), Text: s}
}

func TestApply_StreamingAppendsAndSplitsOnKindChange(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{started("m1"), nil},
		{reasonDelta("m1", "a"), []BlockID{"m/m1/0"}},
		{reasonDelta("m1", "b"), []BlockID{"m/m1/0"}},
		{textDelta("m1", "c"), []BlockID{"m/m1/1"}},
		{reasonDelta("m1", "d"), []BlockID{"m/m1/2"}},
		{started("m2"), nil},
		{textDelta("m2", "x"), []BlockID{"m/m2/0"}},
		{textDelta("m2", "y"), []BlockID{"m/m2/0"}},
		{textDelta("m2", "z"), []BlockID{"m/m2/0"}},
	})
	want := []Block{
		{ID: "m/m1/0", Kind: KindReasoning, MessageID: "m1", Text: "ab", Streaming: true},
		{ID: "m/m1/1", Kind: KindText, MessageID: "m1", Text: "c", Streaming: true},
		{ID: "m/m1/2", Kind: KindReasoning, MessageID: "m1", Text: "d", Streaming: true},
		{ID: "m/m2/0", Kind: KindText, MessageID: "m2", Text: "xyz", Streaming: true},
	}
	assertBlocks(t, p.Blocks(), want)
	versions := map[BlockID]int{"m/m1/0": 2, "m/m1/1": 1, "m/m1/2": 1, "m/m2/0": 3}
	for id, v := range versions {
		if b, _ := p.Block(id); b.Version != v {
			t.Errorf("%s: Version = %d, want %d", id, b.Version, v)
		}
	}
}

func TestApply_StreamingFlagClearsOnStepEnd(t *testing.T) {
	tests := []struct {
		name string
		end  event.Event
		want []BlockID
	}{
		{"step finished", event.StepFinished{Base: rootBase(), MessageID: "m1"}, []BlockID{"m/m1/0", "m/m1/1"}},
		{"run finished", event.RunFinished{Base: rootBase(), MessageID: "m1"}, []BlockID{"m/m1/0", "m/m1/1"}},
		{"run failed", event.RunFailed{Base: rootBase(), Err: "boom"}, []BlockID{"m/m1/0", "m/m1/1", "n/0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(root)
			run(t, p, []step{
				{started("m1"), nil},
				{reasonDelta("m1", "r"), []BlockID{"m/m1/0"}},
				{textDelta("m1", "t"), []BlockID{"m/m1/1"}},
				{event.StepFinished{Base: rootBase(), MessageID: "other"}, nil},
			})
			for _, id := range []BlockID{"m/m1/0", "m/m1/1"} {
				if b, _ := p.Block(id); !b.Streaming {
					t.Fatalf("%s: Streaming = false before step end", id)
				}
			}
			run(t, p, []step{{tt.end, tt.want}})
			for _, id := range []BlockID{"m/m1/0", "m/m1/1"} {
				if b, _ := p.Block(id); b.Streaming || b.Version != 2 {
					t.Errorf("%s: Streaming = %v, Version = %d; want false, 2", id, b.Streaming, b.Version)
				}
			}
			// A delta after the step ended opens a fresh block.
			run(t, p, []step{{textDelta("m1", "late"), []BlockID{"m/m1/2"}}})
		})
	}
}

func TestApply_ToolLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		out   string
		isErr bool
		want  ToolState
	}{
		{"ok", "fine", false, StateOK},
		{"error", "boom", true, StateError},
		{"denied by rule", "denied by permission rule for bash", true, StateDenied},
		{"user denied", "user denied: no", true, StateDenied},
		{"cancelled", "cancelled", true, StateCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mkCall("c1", "bash", `{"command":"ls"}`)
			r := mkResult("c1", "bash", tt.out, tt.isErr)
			p := New(root)
			run(t, p, []step{
				{started("m1"), nil},
				{event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: *c}, []BlockID{"c1"}},
			})
			assertBlocks(t, p.Blocks(), []Block{{ID: "c1", Kind: KindTool, MessageID: "m1", Call: c, State: StateRunning}})
			run(t, p, []step{
				{event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: *r}, []BlockID{"c1"}},
				{event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: *mkResult("nope", "bash", "", false)}, nil},
			})
			assertBlocks(t, p.Blocks(), []Block{{ID: "c1", Kind: KindTool, MessageID: "m1", Call: c, Result: r, State: tt.want}})
			if b, _ := p.Block("c1"); b.Version != 2 {
				t.Errorf("Version = %d, want 2", b.Version)
			}
		})
	}
}

func TestApply_TaskCallLifecycle(t *testing.T) {
	c := mkCall("t1", "task", `{"agent":"explore","description":"find x","prompt":"p"}`)
	r := mkResult("t1", "task", "<task_result session_id=\"ses_child\">\ndone\n</task_result>", false)
	p := New(root)
	run(t, p, []step{
		{event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: *c}, []BlockID{"t1"}},
	})
	assertBlocks(t, p.Blocks(), []Block{{
		ID: "t1", Kind: KindSubagent, MessageID: "m1", Call: c, State: StateRunning,
		Sub: &Subagent{Agent: "explore", Description: "find x"},
	}})
	run(t, p, []step{{event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: *r}, []BlockID{"t1"}}})
	assertBlocks(t, p.Blocks(), []Block{{
		ID: "t1", Kind: KindSubagent, MessageID: "m1", Call: c, Result: r, State: StateOK,
		Sub: &Subagent{Child: "ses_child", Agent: "explore", Description: "find x"},
	}})
}

func TestApply_RunFailedNotices(t *testing.T) {
	tests := []struct {
		name      string
		err       string
		want      Block
		toolState ToolState
	}{
		{"cancelled", "cancelled", Block{ID: "n/0", Kind: KindNotice, Text: "cancelled", Level: LevelInfo}, StateCancelled},
		{"failure", "boom", Block{ID: "n/0", Kind: KindNotice, Text: "run failed: boom", Level: LevelError}, StateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mkCall("c1", "bash", `{}`)
			p := New(root)
			run(t, p, []step{
				{event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: *c}, []BlockID{"c1"}},
				// A tool still running when the run ends is settled with it.
				{event.RunFailed{Base: rootBase(), Err: tt.err}, []BlockID{"c1", "n/0"}},
			})
			assertBlocks(t, p.Blocks(), []Block{
				{ID: "c1", Kind: KindTool, MessageID: "m1", Call: c, State: tt.toolState},
				tt.want,
			})
		})
	}
}

func TestApply_MaxStepsDeltaBecomesNotice(t *testing.T) {
	tests := []struct {
		name  string
		delta string
	}{
		{"bare", "[stopped: reached max_steps (2)]"},
		{"after newline", "\n[stopped: reached max_steps (40)]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(root)
			run(t, p, []step{
				{started("m1"), nil},
				{textDelta("m1", "hi"), []BlockID{"m/m1/0"}},
				{event.StepFinished{Base: rootBase(), MessageID: "m1"}, []BlockID{"m/m1/0"}},
				{textDelta("m1", tt.delta), []BlockID{"n/0"}},
			})
			want := Block{ID: "n/0", Kind: KindNotice, MessageID: "m1", Text: strings.TrimSpace(tt.delta), Level: LevelInfo}
			if b, _ := p.Block("n/0"); !reflect.DeepEqual(unversioned([]Block{b})[0], want) {
				t.Errorf("notice = %+v, want %+v", b, want)
			}
			if t0, _ := p.Block("m/m1/0"); t0.Text != "hi" {
				t.Errorf("text block = %q, want %q", t0.Text, "hi")
			}
		})
	}
}

func TestApply_IgnoresOtherRoots(t *testing.T) {
	other := event.Base{SessionID: "ses_other", RootID: "ses_other"}
	child := event.Base{SessionID: "ses_kid", RootID: root}
	tests := []struct {
		name string
		ev   event.Event
	}{
		{"other root text", event.TextDelta{Base: other, MessageID: "m1", Text: "x"}},
		{"other root tool", event.ToolCallStarted{Base: other, MessageID: "m1", Call: core.ToolCall{ID: "c9", Name: "read"}}},
		{"other root failure", event.RunFailed{Base: other, Err: "boom"}},
		{"other session without root id", event.TextDelta{Base: event.Base{SessionID: "ses_other"}, MessageID: "m1", Text: "x"}},
		{"unowned descendant tool", event.ToolCallStarted{Base: child, MessageID: "m1", Call: core.ToolCall{ID: "c9", Name: "read"}}},
		{"unowned descendant failure", event.RunFailed{Base: child, Err: "boom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(root)
			run(t, p, []step{{started("m1"), nil}, {textDelta("m1", "a"), []BlockID{"m/m1/0"}}})
			before := p.Blocks()
			if got := p.Apply(tt.ev); got != nil {
				t.Errorf("Apply = %q, want nil", got)
			}
			if after := p.Blocks(); !reflect.DeepEqual(after, before) {
				t.Errorf("blocks changed:\n before %+v\n after  %+v", before, after)
			}
		})
	}
}

func TestApply_RootWithoutRootIDIsAccepted(t *testing.T) {
	p := New(root)
	got := p.Apply(event.TextDelta{Base: event.Base{SessionID: root}, MessageID: "m1", Text: "a"})
	if !reflect.DeepEqual(got, []BlockID{"m/m1/0"}) {
		t.Errorf("Apply = %q", got)
	}
}
