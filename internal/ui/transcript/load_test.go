package transcript

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

const root core.SessionID = "ses_root"

func rootBase() event.Base { return event.Base{SessionID: root, RootID: root} }

func mkCall(id, name, input string) *core.ToolCall {
	return &core.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}
}

func mkResult(id, name, out string, isErr bool) *core.ToolResult {
	return &core.ToolResult{CallID: id, Name: name, Output: out, IsError: isErr}
}

func textPart(s string) core.Part         { return core.Part{Kind: core.PartText, Text: s} }
func reasonPart(s string) core.Part       { return core.Part{Kind: core.PartReasoning, Text: s} }
func callPart(c *core.ToolCall) core.Part { return core.Part{Kind: core.PartToolCall, Call: c} }
func resultPart(r *core.ToolResult) core.Part {
	return core.Part{Kind: core.PartToolResult, Result: r}
}

func userMsg(id string, parts ...core.Part) core.Message {
	return core.Message{ID: core.MessageID(id), SessionID: root, Role: core.RoleUser, Status: core.StatusComplete, Parts: parts}
}

func asstMsg(id string, status core.MessageStatus, parts ...core.Part) core.Message {
	return core.Message{ID: core.MessageID(id), SessionID: root, Role: core.RoleAssistant, Status: status, Parts: parts}
}

// unversioned returns bs with every Version zeroed.
func unversioned(bs []Block) []Block {
	out := make([]Block, len(bs))
	for i, b := range bs {
		b.Version = 0
		out[i] = b
	}
	return out
}

func assertBlocks(t *testing.T, got, want []Block) {
	t.Helper()
	got = unversioned(got)
	if len(got) != len(want) {
		t.Fatalf("got %d blocks, want %d:\n got  %+v\n want %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("block %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func TestLoad_UserTextToolsAndReasoning(t *testing.T) {
	read := mkCall("c1", "read", `{"path":"x.go"}`)
	readOK := mkResult("c1", "read", "1: package x", false)
	att := &core.Attachment{Path: "/w/shot.png", Media: &core.Media{MIME: "image/png", Ref: "abc"}}
	tests := []struct {
		name string
		msgs []core.Message
		want []Block
	}{
		{
			name: "user, reasoning, text, tool",
			msgs: []core.Message{
				userMsg("m1", textPart("look"), core.Part{Kind: core.PartAttachment, Attachment: att}),
				asstMsg("m2", core.StatusComplete, reasonPart("hmm"), textPart("ok"), callPart(read), resultPart(readOK)),
			},
			want: []Block{
				{ID: "u/m1", Kind: KindUser, MessageID: "m1", Text: "look", Attachments: []string{"/w/shot.png"}},
				{ID: "m/m2/0", Kind: KindReasoning, MessageID: "m2", Text: "hmm"},
				{ID: "m/m2/1", Kind: KindText, MessageID: "m2", Text: "ok"},
				{ID: "c1", Kind: KindTool, MessageID: "m2", Call: read, Result: readOK, State: StateOK},
			},
		},
		{
			name: "multiple user text parts join with newline",
			msgs: []core.Message{userMsg("m1", textPart("a"), textPart("b"))},
			want: []Block{{ID: "u/m1", Kind: KindUser, MessageID: "m1", Text: "a\nb"}},
		},
		{
			name: "text after a tool call is a new block of the message",
			msgs: []core.Message{
				asstMsg("m2", core.StatusComplete, textPart("x"), callPart(read), textPart("y"), resultPart(readOK)),
			},
			want: []Block{
				{ID: "m/m2/0", Kind: KindText, MessageID: "m2", Text: "x"},
				{ID: "c1", Kind: KindTool, MessageID: "m2", Call: read, Result: readOK, State: StateOK},
				{ID: "m/m2/1", Kind: KindText, MessageID: "m2", Text: "y"},
			},
		},
		{
			name: "complete message with an unanswered call leaves it pending",
			msgs: []core.Message{asstMsg("m2", core.StatusComplete, callPart(read))},
			want: []Block{{ID: "c1", Kind: KindTool, MessageID: "m2", Call: read, State: StatePending}},
		},
		{
			name: "failed message errors unanswered calls and adds a red notice",
			msgs: []core.Message{asstMsg("m2", core.StatusFailed, callPart(read))},
			want: []Block{
				{ID: "c1", Kind: KindTool, MessageID: "m2", Call: read, State: StateError},
				{ID: "n/0", Kind: KindNotice, MessageID: "m2", Text: "run failed", Level: LevelError},
			},
		},
		{
			name: "empty text parts make no block",
			msgs: []core.Message{asstMsg("m2", core.StatusComplete, textPart(""), textPart("z"))},
			want: []Block{{ID: "m/m2/0", Kind: KindText, MessageID: "m2", Text: "z"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(root)
			p.Load(tt.msgs)
			assertBlocks(t, p.Blocks(), tt.want)
		})
	}
}

func TestLoad_InterruptedRunCancelsUnansweredCalls(t *testing.T) {
	c1 := mkCall("c1", "bash", `{"command":"sleep 9"}`)
	c2 := mkCall("c2", "bash", `{"command":"ls"}`)
	r2 := mkResult("c2", "bash", "cancelled", true)
	p := New(root)
	p.Load([]core.Message{asstMsg("m1", core.StatusInterrupted, callPart(c1), callPart(c2), resultPart(r2))})
	assertBlocks(t, p.Blocks(), []Block{
		{ID: "c1", Kind: KindTool, MessageID: "m1", Call: c1, State: StateCancelled},
		{ID: "c2", Kind: KindTool, MessageID: "m1", Call: c2, Result: r2, State: StateCancelled},
		{ID: "n/0", Kind: KindNotice, MessageID: "m1", Text: "cancelled", Level: LevelInfo},
	})
}

func TestLoad_ToolStatesFromResults(t *testing.T) {
	tests := []struct {
		name  string
		out   string
		isErr bool
		want  ToolState
	}{
		{"error", "boom", true, StateError},
		{"rule denial", "denied by permission rule for bash", true, StateDenied},
		{"user denied with message", "user denied: no", true, StateDenied},
		{"bare user denied", "user denied", true, StateDenied},
		{"cancelled", "cancelled", true, StateCancelled},
		{"ok", "fine", false, StateOK},
		{"successful output that says cancelled stays ok", "cancelled", false, StateOK},
		{"successful output that says user denied stays ok", "user denied", false, StateOK},
		{"error mentioning denial mid-text is an error", "x: user denied", true, StateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mkCall("c1", "bash", `{}`)
			r := mkResult("c1", "bash", tt.out, tt.isErr)
			p := New(root)
			p.Load([]core.Message{asstMsg("m1", core.StatusComplete, callPart(c), resultPart(r))})
			b, ok := p.Block("c1")
			if !ok || b.State != tt.want {
				t.Errorf("state = %q (found %v), want %q", b.State, ok, tt.want)
			}
			if got := stateOf(*r); got != tt.want {
				t.Errorf("stateOf = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoad_TaskCallBecomesSubagentBlock(t *testing.T) {
	input := `{"agent":"explore","description":"find x","prompt":"look for x"}`
	tests := []struct {
		name   string
		input  string
		result *core.ToolResult
		want   Block
	}{
		{
			name:   "finished task",
			input:  input,
			result: mkResult("t1", "task", "<task_result session_id=\"ses_child\">\ndone\n</task_result>", false),
			want: Block{
				ID: "t1", Kind: KindSubagent, MessageID: "m1", State: StateOK,
				Sub: &Subagent{Child: "ses_child", Agent: "explore", Description: "find x"},
			},
		},
		{
			name:   "failed task has no child",
			input:  input,
			result: mkResult("t1", "task", "unknown agent", true),
			want: Block{
				ID: "t1", Kind: KindSubagent, MessageID: "m1", State: StateError,
				Sub: &Subagent{Agent: "explore", Description: "find x"},
			},
		},
		{
			name:  "resumed task takes its child from the input",
			input: `{"agent":"explore","description":"again","prompt":"p","session_id":"ses_old"}`,
			want: Block{
				ID: "t1", Kind: KindSubagent, MessageID: "m1", State: StatePending,
				Sub: &Subagent{Child: "ses_old", Agent: "explore", Description: "again"},
			},
		},
		{
			name:  "malformed input still makes a subagent block",
			input: `"not json"`,
			want:  Block{ID: "t1", Kind: KindSubagent, MessageID: "m1", State: StatePending, Sub: &Subagent{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mkCall("t1", "task", tt.input)
			parts := []core.Part{callPart(c)}
			if tt.result != nil {
				parts = append(parts, resultPart(tt.result))
			}
			p := New(root)
			p.Load([]core.Message{asstMsg("m1", core.StatusComplete, parts...)})
			want := tt.want
			want.Call, want.Result = c, tt.result
			assertBlocks(t, p.Blocks(), []Block{want})
		})
	}
}

func TestLoad_CompactionAndMaxStepsNotices(t *testing.T) {
	c := mkCall("c1", "read", `{}`)
	r := mkResult("c1", "read", "x", false)
	p := New(root)
	p.Load([]core.Message{
		asstMsg("m1", core.StatusComplete, core.Part{Kind: core.PartCompaction, Text: "the summary"}),
		asstMsg("m2", core.StatusComplete, callPart(c), resultPart(r), textPart("[stopped: reached max_steps (40)]")),
	})
	assertBlocks(t, p.Blocks(), []Block{
		{ID: "n/0", Kind: KindNotice, MessageID: "m1", Title: "compaction summary", Text: "the summary", Level: LevelInfo},
		{ID: "c1", Kind: KindTool, MessageID: "m2", Call: c, Result: r, State: StateOK},
		{ID: "n/1", Kind: KindNotice, MessageID: "m2", Text: "[stopped: reached max_steps (40)]", Level: LevelInfo},
	})
}

func TestLoad_ReplacesBlocksAndKeepsVersionsMonotonic(t *testing.T) {
	c := mkCall("c1", "read", `{}`)
	r := mkResult("c1", "read", "x", false)
	p := New(root)
	p.Apply(event.MessageStarted{Base: rootBase(), MessageID: "m1"})
	p.Apply(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "live"})
	p.Apply(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: *c})
	p.Apply(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: *r})
	p.Apply(event.RunFailed{Base: rootBase(), Err: "boom"})

	p.Load([]core.Message{asstMsg("m1", core.StatusComplete, textPart("live"), callPart(c), resultPart(r))})
	p.Apply(event.RunFailed{Base: rootBase(), Err: "again"})

	tests := []struct {
		id      BlockID
		version int
	}{
		{"m/m1/0", 3}, // v1 created, v2 streaming cleared by RunFailed, v3 reloaded
		{"c1", 3},     // v1 running, v2 finished, v3 reloaded
		{"n/1", 1},    // notice IDs keep counting across Load: "n/0" was the first run's
	}
	for _, tt := range tests {
		b, ok := p.Block(tt.id)
		if !ok || b.Version != tt.version {
			t.Errorf("block %q: version %d (found %v), want %d", tt.id, b.Version, ok, tt.version)
		}
	}
	if _, ok := p.Block("n/0"); ok {
		t.Error("Load kept the pre-Load notice n/0")
	}
	if got := len(p.Blocks()); got != 3 {
		t.Errorf("len(Blocks) = %d, want 3", got)
	}
}

func TestBlocks_ReturnsCopies(t *testing.T) {
	c := mkCall("t1", "task", `{"agent":"a","description":"d"}`)
	p := New(root)
	p.Load([]core.Message{
		userMsg("m0", textPart("u"), core.Part{Kind: core.PartAttachment, Attachment: &core.Attachment{Path: "/p"}}),
		asstMsg("m1", core.StatusComplete, callPart(c)),
	})
	bs := p.Blocks()
	bs[0].Attachments[0] = "/mutated"
	bs[1].Sub.Agent = "mutated"
	bs[1].Call.Name = "mutated"
	if b, _ := p.Block("u/m0"); b.Attachments[0] != "/p" {
		t.Errorf("attachments aliased: %q", b.Attachments[0])
	}
	if b, _ := p.Block("t1"); b.Sub.Agent != "a" || b.Call.Name != "task" {
		t.Errorf("sub/call aliased: %+v %+v", b.Sub, b.Call)
	}
}
