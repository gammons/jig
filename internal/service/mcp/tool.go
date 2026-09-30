package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// wrapName builds the model-visible tool name for one remote tool. Task 8
// replaces this with the full §7.1 name-sanitizing/hashing behavior; for
// now it's the plain, unsanitized join the brief calls for.
func wrapName(server, tool string) string {
	return "mcp__" + server + "__" + tool
}

// mcpTool is the minimal ext.Tool wrapper around one RemoteTool. Task 8
// replaces its Description/Concurrent/Run bodies with the full §7
// behavior (content mapping, timeouts, vanished-tool handling); this
// version exists so Tools()/Servers() and the Manager's lifecycle have
// something real to hold and the tests in this task can exercise them.
type mcpTool struct {
	m      *Manager
	server string
	remote RemoteTool
	name   string
	schema map[string]any
}

// newTool builds the ext.Tool wrapper for one remote tool.
func newTool(m *Manager, server string, rt RemoteTool) (ext.Tool, error) {
	schema := map[string]any{"type": "object"}
	if len(rt.Schema) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(rt.Schema, &parsed); err == nil {
			schema = parsed
		}
	}
	return &mcpTool{
		m:      m,
		server: server,
		remote: rt,
		name:   wrapName(server, rt.Name),
		schema: schema,
	}, nil
}

func (t *mcpTool) Name() string           { return t.name }
func (t *mcpTool) Description() string    { return t.remote.Description }
func (t *mcpTool) Schema() map[string]any { return t.schema }
func (t *mcpTool) Concurrent() bool       { return false }

// Run looks up the server's live connection and calls the remote tool with
// the call's input unchanged.
func (t *mcpTool) Run(ctx context.Context, _ ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	conn, ok := t.m.liveConn(t.server)
	if !ok {
		return core.ToolError(call, fmt.Sprintf("mcp server %q is not ready", t.server)), nil
	}

	res, err := conn.CallTool(ctx, t.remote.Name, call.Input)
	if err != nil {
		if ctx.Err() != nil {
			return core.ToolResult{}, ctx.Err()
		}
		t.m.markFailed(t.server, err)
		return core.ToolError(call, err.Error()), nil
	}
	if res.IsError {
		return core.ToolError(call, joinText(res.Content)), nil
	}
	return core.ToolOK(call, joinText(res.Content)), nil
}

// joinText concatenates every text content item with "\n".
func joinText(content []RemoteContent) string {
	var parts []string
	for _, c := range content {
		if c.Kind == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// buildTools wraps every remote tool into an ext.Tool, dropping any whose
// construction fails (Task 8 makes this possible via schema validation;
// today newTool never errors). Tools are sorted by remote name to keep
// Manager.Tools's per-server slice stably ordered.
func buildTools(m *Manager, server string, remote []RemoteTool) []ext.Tool {
	sorted := append([]RemoteTool(nil), remote...)
	sortRemoteTools(sorted)

	tools := make([]ext.Tool, 0, len(sorted))
	for _, rt := range sorted {
		tool, err := newTool(m, server, rt)
		if err != nil {
			continue
		}
		tools = append(tools, tool)
	}
	return tools
}

// sortRemoteTools sorts rt by Name ascending.
func sortRemoteTools(rt []RemoteTool) {
	sort.Slice(rt, func(i, j int) bool { return rt[i].Name < rt[j].Name })
}
