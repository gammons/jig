package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

func TestWrapName(t *testing.T) {
	cases := []struct {
		server, tool, want string
	}{
		{"gh", "get_issue", "mcp__gh__get_issue"},
		{"gh", "get.issue/v2", "mcp__gh__get_issue_v2"},
	}
	for _, c := range cases {
		got := wrapName(c.server, c.tool)
		if got != c.want {
			t.Errorf("wrapName(%q,%q) = %q, want %q", c.server, c.tool, got, c.want)
		}
	}

	// 70-character full name.
	longTool := strings.Repeat("a", 70)
	full := "mcp__" + "gh" + "__" + longTool
	// sanitized full is identical here (all valid chars); recompute exactly
	// as the implementation should: sanitize(full), then hash the sanitized
	// full name if too long.
	got := wrapName("gh", longTool)
	if len(got) != 64 {
		t.Fatalf("len(got) = %d, want 64", len(got))
	}
	sum := sha256.Sum256([]byte(full))
	want := full[:55] + "_" + hex.EncodeToString(sum[:])[:8]
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got2 := wrapName("gh", longTool)
	if got2 != got {
		t.Errorf("wrapName not stable: %q != %q", got2, got)
	}
}

func TestTool_Description(t *testing.T) {
	m := newTestManagerForTool(t)
	longDesc := strings.Repeat("x", 3000)
	rt := RemoteTool{Name: "get_issue", Description: longDesc}
	tool, err := newTool(m, "gh", rt)
	if err != nil {
		t.Fatal(err)
	}
	desc := tool.Description()
	if !strings.HasPrefix(desc, "[mcp:gh] ") {
		t.Errorf("description doesn't have prefix: %q", desc[:20])
	}
	if len(desc) > 2048 {
		t.Errorf("description too long: %d bytes", len(desc))
	}
}

func TestTool_SchemaFallback(t *testing.T) {
	m := newTestManagerForTool(t)

	// missing schema
	tool, err := newTool(m, "gh", RemoteTool{Name: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if tool.Schema()["type"] != "object" {
		t.Errorf("missing schema: got %v", tool.Schema())
	}

	// wrong type
	tool2, err := newTool(m, "gh", RemoteTool{Name: "t2", Schema: []byte(`{"type":"string"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if tool2.Schema()["type"] != "object" {
		t.Errorf("wrong type schema: got %v", tool2.Schema())
	}

	// invalid JSON: dropped
	_, err = newTool(m, "gh", RemoteTool{Name: "t3", Schema: []byte(`not json`)})
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

func TestTool_BuildToolsWarnings(t *testing.T) {
	m := newTestManagerForTool(t)
	remote := []RemoteTool{
		{Name: "good", Schema: []byte(`{"type":"object"}`)},
		{Name: "bad", Schema: []byte(`not json`)},
	}
	tools, warnings := buildTools(m, "gh", remote)
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "bad") || !strings.Contains(warnings[0], "invalid schema") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestTool_ConcurrentReadOnly(t *testing.T) {
	m := newTestManagerForTool(t)
	tool, err := newTool(m, "gh", RemoteTool{Name: "t1", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !tool.Concurrent() {
		t.Error("expected Concurrent() true for read-only tool")
	}

	tool2, err := newTool(m, "gh", RemoteTool{Name: "t2", ReadOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	if tool2.Concurrent() {
		t.Error("expected Concurrent() false for non-read-only tool")
	}
}

func newTestManagerForTool(t *testing.T) *Manager {
	t.Helper()
	m := New(Deps{
		Servers: []core.MCPServer{{Name: "gh"}},
		Clock:   clock.NewFake(time.Unix(0, 0)),
	})
	return m
}

func TestTool_RunNotReady(t *testing.T) {
	m := newTestManagerForTool(t)
	m.servers["gh"].status.State = core.MCPNeedsAuth
	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Run(context.Background(), ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError")
	}
	want := `mcp server "gh" needs sign-in; open ctrl+p → MCP to sign in`
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestTool_RunNotReady_OtherState(t *testing.T) {
	m := newTestManagerForTool(t)
	m.servers["gh"].status.State = core.MCPFailed
	m.servers["gh"].status.Err = "boom"
	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Run(context.Background(), ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
	if err != nil {
		t.Fatal(err)
	}
	want := `mcp server "gh" is failed: boom`
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestTool_RunTimeout(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	m := New(Deps{
		Servers: []core.MCPServer{{Name: "gh", ToolTimeout: 2 * time.Minute}},
		Clock:   fake,
		Bus:     newRecorder(),
	})
	conn := newFakeConn()
	conn.tools = []RemoteTool{{Name: "get_issue"}}
	blockCh := make(chan struct{})
	conn.callBlock = blockCh

	e, applied := finishReady(m, "gh", m.servers["gh"].generation, conn, conn.tools)
	if !applied {
		t.Fatal("finishReady did not apply")
	}
	_ = e

	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}

	resCh := make(chan core.ToolResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := tool.Run(context.Background(), ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
		resCh <- res
		errCh <- err
	}()

	fake.BlockUntilWaiters(1)
	fake.Advance(2 * time.Minute)
	close(blockCh)

	res := <-resCh
	err = <-errCh
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError")
	}
	want := "mcp: get_issue timed out after 2m0s"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestTool_RunCancel(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	m := New(Deps{
		Servers: []core.MCPServer{{Name: "gh", ToolTimeout: 2 * time.Minute}},
		Clock:   fake,
		Bus:     newRecorder(),
	})
	conn := newFakeConn()
	conn.tools = []RemoteTool{{Name: "get_issue"}}
	blockCh := make(chan struct{})
	conn.callBlock = blockCh

	finishReady(m, "gh", m.servers["gh"].generation, conn, conn.tools)

	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resCh := make(chan error, 1)
	go func() {
		_, err := tool.Run(ctx, ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
		resCh <- err
	}()
	cancel()
	err = <-resCh
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestTool_TransportErrorMarksFailed(t *testing.T) {
	rec := newRecorder()
	fake := clock.NewFake(time.Unix(0, 0))
	m := New(Deps{
		Servers: []core.MCPServer{{Name: "gh", ToolTimeout: 2 * time.Minute}},
		Clock:   fake,
		Bus:     rec,
	})
	conn := newFakeConn()
	conn.tools = []RemoteTool{{Name: "get_issue"}}
	conn.callErr = errors.New("boom")
	finishReady(m, "gh", m.servers["gh"].generation, conn, conn.tools)
	// Simulate transport failure: close Done, as a real conn would.
	conn.dropDoneOnly()

	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Run(context.Background(), ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Output, "boom") {
		t.Errorf("res = %+v", res)
	}
	m.mu.Lock()
	state := m.servers["gh"].status.State
	m.mu.Unlock()
	if state != core.MCPFailed {
		t.Errorf("state = %v, want failed", state)
	}
}

func TestTool_JSONRPCErrorStaysReady(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	m := New(Deps{
		Servers: []core.MCPServer{{Name: "gh", ToolTimeout: 2 * time.Minute}},
		Clock:   fake,
		Bus:     newRecorder(),
	})
	conn := newFakeConn()
	conn.tools = []RemoteTool{{Name: "get_issue"}}
	conn.callErr = errors.New("bad args")
	finishReady(m, "gh", m.servers["gh"].generation, conn, conn.tools)

	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Run(context.Background(), ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Output, "bad args") {
		t.Errorf("res = %+v", res)
	}
	m.mu.Lock()
	state := m.servers["gh"].status.State
	m.mu.Unlock()
	if state != core.MCPReady {
		t.Errorf("state = %v, want ready", state)
	}
}

func TestTool_VanishedBetweenListAndCall(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	m := New(Deps{
		Servers: []core.MCPServer{{Name: "gh", ToolTimeout: 2 * time.Minute}},
		Clock:   fake,
		Bus:     newRecorder(),
	})
	conn := newFakeConn()
	conn.tools = []RemoteTool{{Name: "get_issue"}}
	finishReady(m, "gh", m.servers["gh"].generation, conn, conn.tools)

	tool, err := newTool(m, "gh", RemoteTool{Name: "get_issue"})
	if err != nil {
		t.Fatal(err)
	}

	// server re-lists without the tool
	m.mu.Lock()
	m.servers["gh"].remote = nil
	m.mu.Unlock()

	res, err := tool.Run(context.Background(), ext.RunContext{}, core.ToolCall{ID: "c1", Name: tool.Name()})
	if err != nil {
		t.Fatal(err)
	}
	want := `tool get_issue is no longer offered by mcp server "gh"`
	if !res.IsError || res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}
