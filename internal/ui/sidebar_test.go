package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
)

// toolPair is an assistant tool call part plus its (successful) result.
func toolPair(id, name, input, output string) []core.Part {
	return []core.Part{
		{Kind: core.PartToolCall, Call: &core.ToolCall{ID: id, Name: name, Input: []byte(input)}},
		{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: id, Name: name, Output: output}},
	}
}

// sidebarSession is a stored session touching every sidebar section.
func sidebarSession() (core.Session, []core.Message, []core.Todo) {
	var parts []core.Part
	parts = append(parts, toolPair("c1", "write", `{"path":"new.go","content":"package x"}`, "wrote new.go")...)
	parts = append(parts, toolPair("c2", "edit", `{"path":"./old.go","old_string":"a","new_string":"b"}`, "edited")...)
	parts = append(parts, toolPair("c3", "task", `{"agent":"explore","description":"map the \u001b[31mrepo"}`, "done")...)
	parts = append(parts, toolPair("c4", "bash", `{"command":"agent-browser --session=s7 open https://example.test/docs"}`, "ok")...)
	msgs := []core.Message{
		{ID: "u1", SessionID: "ses_1", Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "tidy up"}}},
		{ID: "a1", SessionID: "ses_1", Role: core.RoleAssistant, Model: "anthropic/claude-sonnet-5", CostUSD: 0.25,
			Usage: core.Usage{Input: 50000}, Parts: parts},
	}
	todos := []core.Todo{
		{Content: "read the code", Status: "completed"},
		{Content: "fix \u001b[2Jthe bug", Status: "in_progress"},
		{Content: "write tests", Status: "pending"},
	}
	return core.Session{ID: "ses_1", Title: "Tidy \u001b]0;pwn\u0007up", Agent: "build", Model: "anthropic/claude-sonnet-5"}, msgs, todos
}

func TestSidebar_SectionsFromState(t *testing.T) {
	t.Parallel()
	info, msgs, todos := sidebarSession()
	ta := newTestApp(t, withSize(150, 40), withResume(info, msgs, todos))
	raw := ta.app.w.side.View()
	if strings.ContainsAny(xansi.Strip(raw), "\x1b\x07") {
		t.Fatalf("sidebar has unsanitized control bytes: %q", raw)
	}
	side := xansi.Strip(raw)
	for _, want := range []string{
		"Session", "Tidy up", "25%", "$0.25 · build · claude-sonnet-5",
		"Todos", "✓ read the code", "● fix the bug", "○ write tests",
		"Files", "A new.go", "M old.go",
		"Subagents", "✓ explore", "map the repo",
		"Browser", "https://example.test/docs", "s7",
	} {
		if !strings.Contains(side, want) {
			t.Errorf("sidebar lacks %q:\n%s", want, side)
		}
	}

	// A root TodosUpdated replaces the list; a descendant's is ignored.
	ta.event(event.TodosUpdated{Base: rootBase(), Todos: []core.Todo{{Content: "ship it", Status: "pending"}}})
	ta.event(event.TodosUpdated{Base: childBase(), Todos: []core.Todo{{Content: "child todo", Status: "pending"}}})
	side = xansi.Strip(ta.app.w.side.View())
	if !strings.Contains(side, "○ ship it") || strings.Contains(side, "read the code") || strings.Contains(side, "child todo") {
		t.Errorf("todos after updates:\n%s", side)
	}
}

// TestSidebar_SessionTitleWraps: a long session title wraps onto as many
// sidebar lines as it needs instead of being cut with "…".
func TestSidebar_SessionTitleWraps(t *testing.T) {
	t.Parallel()
	title := "refactor the session sidebar so that long titles wrap neatly"
	ta := newTestApp(t, withSize(150, 40), withResume(core.Session{ID: "ses_1", Title: title, Agent: "build"}, nil, nil))
	side := xansi.Strip(ta.app.w.side.View())
	if strings.Contains(side, "…") {
		t.Errorf("title was truncated:\n%s", side)
	}
	if got := strings.Join(strings.Fields(side), " "); !strings.Contains(got, title) {
		t.Errorf("sidebar lacks the full title %q:\n%s", title, side)
	}
}

func TestSidebar_SubagentStateIcons(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(150, 40))
	ta.sendAndAdopt("go")
	ta.startSubagent()
	side := xansi.Strip(ta.app.w.side.View())
	if !strings.Contains(side, "● explore") {
		t.Errorf("a running subagent should show ●:\n%s", side)
	}
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: "c9", Name: "task", Output: "failed", IsError: true}})
	side = xansi.Strip(ta.app.w.side.View())
	if !strings.Contains(side, "✗ explore") {
		t.Errorf("a failed subagent should show ✗:\n%s", side)
	}
}

func TestSidebar_BrowserSessionName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		cmds  []string
		want  string
		shown bool
	}{
		{"no browser use", []string{"ls"}, "", false},
		{"default session", []string{"agent-browser open https://a.test"}, "session default", true},
		{"separate flag value", []string{"agent-browser --session s1 open https://a.test"}, "session s1", true},
		{"latest command wins", []string{"agent-browser --session s1 open https://a.test", "agent-browser --session=s2 snapshot -i"}, "session s2", true},
		{"a later non-browser command changes nothing", []string{"agent-browser --session s1 open https://a.test", "ls"}, "session s1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var parts []core.Part
			for i, c := range tt.cmds {
				id := string(rune('a' + i))
				parts = append(parts, toolPair(id, "bash", `{"command":"`+c+`"}`, "ok")...)
			}
			msgs := []core.Message{{ID: "a1", SessionID: "ses_1", Role: core.RoleAssistant, Parts: parts}}
			ta := newTestApp(t, withSize(150, 40), withResume(core.Session{ID: "ses_1", Agent: "build"}, msgs, nil))
			side := xansi.Strip(ta.app.w.side.View())
			if got := strings.Contains(side, "Browser"); got != tt.shown {
				t.Fatalf("Browser section shown = %v, want %v:\n%s", got, tt.shown, side)
			}
			if tt.shown && !strings.Contains(side, tt.want) {
				t.Errorf("sidebar lacks %q:\n%s", tt.want, side)
			}
		})
	}
}

func TestApp_GoldenSidebar(t *testing.T) {
	t.Parallel()
	info, msgs, todos := sidebarSession()
	ta := newTestApp(t, withSize(150, 40), withResume(info, msgs, todos))
	golden.Assert(t, "app_sidebar", ta.view())
}

func TestSidebar_RebuiltOnlyOnChange(t *testing.T) {
	t.Parallel()
	info, msgs, todos := sidebarSession()
	ta := newTestApp(t, withSize(150, 40), withResume(info, msgs, todos))
	ta.key("esc")
	n := ta.app.w.sideProj.builds
	ta.key("k")
	ta.key("k")
	ta.send(tea.WindowSizeMsg{Width: 160, Height: 40})
	ta.fire()
	ta.key("j")
	if got := ta.app.w.sideProj.builds; got != n {
		t.Fatalf("projection sections rebuilt %d times on navigation/resize, want 0", got-n)
	}
	// A todos change needs no projection rebuild but still shows.
	ta.event(event.TodosUpdated{Base: rootBase(), Todos: []core.Todo{{Content: "new todo", Status: "pending"}}})
	if !strings.Contains(xansi.Strip(ta.app.w.side.View()), "new todo") {
		t.Error("todos change not shown")
	}
	// A new tool block changes the projection: one rebuild.
	ta.key("i")
	ta.typeText("x")
	ta.key("enter")
	ta.event(event.MessageStarted{Base: rootBase(), MessageID: "m1"})
	n = ta.app.w.sideProj.builds
	call := core.ToolCall{ID: "w9", Name: "write", Input: []byte(`{"path":"z.go","content":"x"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: "w9", Name: "write", Output: "ok"}})
	if got := ta.app.w.sideProj.builds; got <= n {
		t.Fatal("a projection change did not rebuild the sidebar sections")
	}
	if !strings.Contains(xansi.Strip(ta.app.w.side.View()), "A z.go") {
		t.Error("the new file is not listed")
	}
}
