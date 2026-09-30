package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// maxDescriptionBytes caps a wrapped tool's description (§7.1).
const maxDescriptionBytes = 2048

// sanitizeName replaces every character outside [A-Za-z0-9_-] with "_".
func sanitizeName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		b[i] = '_'
	}
	return string(b)
}

// wrapName builds the model-visible tool name for one remote tool: it
// sanitizes every character outside [A-Za-z0-9_-] to "_", then, if the
// sanitized full name is longer than 64 characters, shortens it to its
// first 55 characters + "_" + the first 8 hex characters of the sha256 of
// the full *sanitized* name (not the pre-sanitized name).
func wrapName(server, tool string) string {
	full := "mcp__" + server + "__" + tool
	sanitized := sanitizeName(full)
	if len(sanitized) <= 64 {
		return sanitized
	}
	sum := sha256.Sum256([]byte(sanitized))
	return sanitized[:55] + "_" + hex.EncodeToString(sum[:])[:8]
}

// buildDescription builds a wrapped tool's Description(): "[mcp:<server>]
// " + the server's description, capped at maxDescriptionBytes without
// splitting a UTF-8 rune.
func buildDescription(server, desc string) string {
	full := "[mcp:" + server + "] " + desc
	return capBytesAtRune(full, maxDescriptionBytes)
}

// capBytesAtRune truncates s to at most n bytes, never splitting a UTF-8
// rune.
func capBytesAtRune(s string, n int) string {
	if len(s) <= n {
		return s
	}
	b := s[:n]
	for len(b) > 0 {
		r, size := utf8.DecodeLastRuneInString(b)
		if r != utf8.RuneError || size != 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// buildSchema validates and normalizes rt.Schema per §7.1: a JSON object
// whose top-level "type" is "object" passes through untouched; a missing
// schema, or one whose type is anything else, falls back to
// {"type":"object"}. Invalid JSON returns an error (the tool is dropped).
func buildSchema(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{"type": "object"}, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if t, _ := parsed["type"].(string); t == "object" {
		return parsed, nil
	}
	return map[string]any{"type": "object"}, nil
}

// mcpTool is the ext.Tool wrapper around one RemoteTool.
type mcpTool struct {
	m      *Manager
	server string
	remote RemoteTool
	name   string
	schema map[string]any
}

// newTool builds the ext.Tool wrapper for one remote tool. It returns an
// error (the tool is dropped) only when rt.Schema is invalid JSON.
func newTool(m *Manager, server string, rt RemoteTool) (ext.Tool, error) {
	schema, err := buildSchema(rt.Schema)
	if err != nil {
		return nil, err
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
func (t *mcpTool) Description() string    { return buildDescription(t.server, t.remote.Description) }
func (t *mcpTool) Schema() map[string]any { return t.schema }
func (t *mcpTool) Concurrent() bool       { return t.remote.ReadOnly }

// serverInfo reads name's current status and its tool timeout under the
// read lock, for Run's not-ready path.
func serverInfo(m *Manager, name string) (core.MCPServerStatus, time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok {
		return core.MCPServerStatus{}, 0, false
	}
	return st.status, st.cfg.ToolTimeout, true
}

// toolStillOffered reports whether remote is still in name's current tool
// list, under the read lock.
func toolStillOffered(m *Manager, name, remote string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok {
		return false
	}
	for _, rt := range st.remote {
		if rt.Name == remote {
			return true
		}
	}
	return false
}

// notReadyResult builds the §7.2 step-1 IsError result for a server that
// isn't ready.
func notReadyResult(call core.ToolCall, name string, status core.MCPServerStatus) core.ToolResult {
	if status.State == core.MCPNeedsAuth {
		return core.ToolError(call, fmt.Sprintf("mcp server %q needs sign-in; open ctrl+p → MCP to sign in", name))
	}
	msg := fmt.Sprintf("mcp server %q is %s", name, status.State)
	if status.Err != "" {
		msg += ": " + status.Err
	}
	return core.ToolError(call, msg)
}

// Run implements the §7.2 calling sequence: not-ready hint, vanished-tool
// check, a tool_timeout deadline raced against CallTool, ctx cancellation,
// and transport-vs-application error handling.
func (t *mcpTool) Run(ctx context.Context, _ ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	conn, gen, ok := t.m.liveConn(t.server)
	if !ok {
		status, _, exists := serverInfo(t.m, t.server)
		if !exists {
			return core.ToolError(call, fmt.Sprintf("mcp server %q is not ready", t.server)), nil
		}
		return notReadyResult(call, t.server, status), nil
	}

	if !toolStillOffered(t.m, t.server, t.remote.Name) {
		return core.ToolError(call, fmt.Sprintf("tool %s is no longer offered by mcp server %q", t.remote.Name, t.server)), nil
	}

	_, toolTimeout, _ := serverInfo(t.m, t.server)

	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type callResult struct {
		res RemoteResult
		err error
	}
	resCh := make(chan callResult, 1)
	go func() {
		res, err := conn.CallTool(callCtx, t.remote.Name, call.Input)
		resCh <- callResult{res, err}
	}()

	select {
	case r := <-resCh:
		if r.err != nil {
			if ctx.Err() != nil {
				return core.ToolResult{}, ctx.Err()
			}
			select {
			case <-conn.Done():
				// The conn ended (transport failure): move the server to
				// failed, guarded by gen so a stale report can't clobber a
				// newer connection.
				t.m.markFailed(t.server, gen, r.err)
			default:
				// The conn is still live: this is a JSON-RPC or tool-level
				// error, not a transport failure, so the server stays ready.
			}
			return core.ToolError(call, r.err.Error()), nil
		}
		return mapResult(call, t.server, t.remote.Name, r.res, t.m.images), nil

	case <-ctx.Done():
		cancel()
		return core.ToolResult{}, ctx.Err()

	case <-t.m.clk.After(toolTimeout):
		cancel()
		return core.ToolError(call, fmt.Sprintf("mcp: %s timed out after %s", t.remote.Name, toolTimeout)), nil
	}
}

// buildTools wraps every remote tool into an ext.Tool, dropping any whose
// schema is invalid JSON (warning "tool <name>: invalid schema") or whose
// wrapped name collides with an earlier tool's, in sorted order (warning
// "tool <name>: name collides with <earlier> as <wrapped>"), per §7.1.
func buildTools(m *Manager, server string, remote []RemoteTool) ([]ext.Tool, []string) {
	sorted := append([]RemoteTool(nil), remote...)
	sortRemoteTools(sorted)

	tools := make([]ext.Tool, 0, len(sorted))
	seen := make(map[string]string, len(sorted)) // wrapped name -> remote name
	var warnings []string
	for _, rt := range sorted {
		tool, err := newTool(m, server, rt)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("tool %s: invalid schema", rt.Name))
			continue
		}
		wrapped := tool.Name()
		if earlier, dup := seen[wrapped]; dup {
			warnings = append(warnings, fmt.Sprintf("tool %s: name collides with %s as %s", rt.Name, earlier, wrapped))
			continue
		}
		seen[wrapped] = rt.Name
		tools = append(tools, tool)
	}
	return tools, warnings
}

// sortRemoteTools sorts rt by Name ascending.
func sortRemoteTools(rt []RemoteTool) {
	sort.Slice(rt, func(i, j int) bool { return rt[i].Name < rt[j].Name })
}
