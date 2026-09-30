package transcript

import (
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func gTool(id, name string, st ToolState) Block {
	return Block{ID: toolBlockID(id), Kind: KindTool, State: st, Call: &core.ToolCall{ID: id, Name: name}}
}

func gBlock(id string, kind Kind) Block { return Block{ID: BlockID(id), Kind: kind} }

func TestGroups(t *testing.T) {
	ok := StateOK
	tests := []struct {
		name   string
		blocks []Block
		want   []Group
	}{
		{"empty", nil, nil},
		{"no tools", []Block{gBlock("u/u1", KindUser), gBlock("m/a/0", KindText)}, nil},
		{"a lone call is not grouped",
			[]Block{gBlock("m/a/0", KindText), gTool("c1", "read", ok), gBlock("m/a/1", KindText)}, nil},
		{"two exploration calls group",
			[]Block{gTool("c1", "read", ok), gTool("c2", "grep", ok)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "t/c2"}}}},
		{"reasoning between calls joins; leading and trailing reasoning does not",
			[]Block{gBlock("m/a/0", KindReasoning), gTool("c1", "read", ok), gBlock("m/b/0", KindReasoning),
				gTool("c2", "glob", ok), gBlock("m/c/0", KindReasoning), gBlock("m/c/1", KindText)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "m/b/0", "t/c2"}}}},
		{"the spec's example",
			[]Block{gBlock("m/a/0", KindText), gTool("c1", "read", ok), gBlock("m/b/0", KindReasoning),
				gTool("c2", "grep", ok), gBlock("m/c/0", KindReasoning), gTool("c3", "read", ok),
				gTool("c4", "edit", ok), gTool("c5", "read", ok), gBlock("m/d/0", KindText)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "m/b/0", "t/c2", "m/c/0", "t/c3"}}}},
		{"failed, denied, cancelled, awaiting, running, and pending calls are members",
			[]Block{gTool("c1", "read", StateError), gTool("c2", "grep", StateDenied), gTool("c3", "glob", StateCancelled),
				gTool("c4", "read", StateAwaiting), gTool("c5", "read", StateRunning), gTool("c6", "read", StatePending)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "t/c2", "t/c3", "t/c4", "t/c5", "t/c6"}}}},
		{"a breaker splits two groups",
			[]Block{gTool("c1", "read", ok), gTool("c2", "read", ok), gTool("c3", "bash", ok),
				gTool("c4", "grep", ok), gTool("c5", "glob", ok)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "t/c2"}}, {ID: "g/c4", Members: []BlockID{"t/c4", "t/c5"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Groups(tt.blocks); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Groups =\n %+v\nwant\n %+v", got, tt.want)
			}
		})
	}
}

func TestGroups_EveryBreakerEndsARun(t *testing.T) {
	ok := StateOK
	breakers := []Block{
		gBlock("m/x/0", KindText), gBlock("u/x", KindUser), gBlock("n/0", KindNotice),
		{ID: "t/s1", Kind: KindSubagent, Call: &core.ToolCall{ID: "s1", Name: "task"}},
		gTool("b1", "bash", ok), gTool("b2", "edit", ok), gTool("b3", "write", ok),
		gTool("b4", "todo", ok), gTool("b5", "skill", ok), gTool("b6", "mcp_search", ok),
	}
	for _, br := range breakers {
		blocks := []Block{gTool("c1", "read", ok), br, gTool("c2", "read", ok)}
		if got := Groups(blocks); got != nil {
			t.Errorf("read, %s (%v), read: Groups = %+v, want none", br.ID, br.Kind, got)
		}
	}
}

func TestGroups_IDStaysWithTheFirstCall(t *testing.T) {
	ok := StateOK
	blocks := []Block{gTool("c1", "read", ok), gTool("c2", "read", ok), gTool("c3", "grep", ok)}
	two, three := Groups(blocks[:2]), Groups(blocks)
	if len(two) != 1 || len(three) != 1 || two[0].ID != "g/c1" || three[0].ID != "g/c1" {
		t.Errorf("IDs as the group grows: %+v then %+v, want g/c1 both times", two, three)
	}
}
