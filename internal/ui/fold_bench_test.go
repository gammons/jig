package ui

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// benchHistoryGroups is benchHistory with a group in every fourth message
// (3, 7, 11, …, all assistant messages): reasoning, then a read and a
// grep with their results, which group as g/r<index>.
func benchHistoryGroups(n int) []core.Message {
	msgs := benchHistory(n)
	for i := 3; i < n; i += 4 {
		r, q := fmt.Sprintf("r%05d", i), fmt.Sprintf("q%05d", i)
		msgs[i].Parts = []core.Part{
			{Kind: core.PartReasoning, Text: "look around first"},
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: r, Name: "read", Input: json.RawMessage(`{"path":"internal/ui/render.go"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: r, Name: "read", Output: "1: package ui"}},
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: q, Name: "grep", Input: json.RawMessage(`{"pattern":"renderGroup"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: q, Name: "grep", Output: "internal/ui/groups.go:40: func"}},
		}
	}
	return msgs
}

// BenchmarkApp_FoldToggle2000 measures o on a group header (the group
// expands or collapses: a list rebuild) and the next View, on a resumed
// session of 2,000 messages holding 500 groups.
func BenchmarkApp_FoldToggle2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistoryGroups(2000), nil))
	_ = ta.view()
	ta.key("esc")
	if !ta.app.w.list.Select("g/r01003") {
		b.Fatal("no group g/r01003 in the list")
	}
	_ = ta.view()
	b.ReportAllocs()
	for b.Loop() {
		ta.key("o")
		_ = ta.view()
	}
}

// BenchmarkApp_ToolStart2000 measures one ToolCallStarted that joins the
// run's trailing group and absorbs the reasoning before it (a structural
// rebuild of the list), and the next View, on the same resumed session.
// Each iteration's setup (a new step with its reasoning, and every 20
// steps a reply that ends the group so it stays small) is untimed.
func BenchmarkApp_ToolStart2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistoryGroups(2000), nil))
	_ = ta.view()
	ta.typeText("go")
	ta.key("enter")
	n := 0
	step := func() core.MessageID {
		n++
		msg := core.MessageID(fmt.Sprintf("live%d", n))
		ta.event(event.MessageStarted{Base: rootBase(), MessageID: msg})
		ta.event(event.ReasoningDelta{Base: rootBase(), MessageID: msg, Text: "next"})
		ta.fire()
		return msg
	}
	start := func(msg core.MessageID) {
		ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: msg,
			Call: core.ToolCall{ID: fmt.Sprintf("b%d", n), Name: "read", Input: json.RawMessage(`{"path":"a.go"}`)}})
	}
	finish := func(msg core.MessageID) {
		ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: msg,
			Result: core.ToolResult{CallID: fmt.Sprintf("b%d", n), Name: "read", Output: "1: package a"}})
	}
	first := step()
	start(first)
	finish(first)
	_ = ta.view()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		if i%20 == 19 {
			reply := step()
			ta.event(event.TextDelta{Base: rootBase(), MessageID: reply, Text: "ok"})
			lone := step()
			start(lone)
			finish(lone)
		}
		msg := step()
		_ = ta.view()
		b.StartTimer()
		start(msg)
		_ = ta.view()
		b.StopTimer()
		finish(msg)
		b.StartTimer()
	}
}
