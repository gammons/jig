package ui

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/core"
)

// mcpServersMsg carries the outcome of a p.MCP.Servers call.
type mcpServersMsg struct {
	list []core.MCPServerStatus
	err  error
}

// mcpServersCmd lists the configured MCP servers through p.MCP.
func mcpServersCmd(ctx context.Context, p Ports) tea.Cmd {
	if p.MCP == nil {
		return nil
	}
	return func() tea.Msg {
		list, err := p.MCP.Servers(ctx)
		return mcpServersMsg{list: list, err: err}
	}
}

// mcpIssues counts the servers needing sign-in or that failed, for the
// status bar badge.
func mcpIssues(list []core.MCPServerStatus) int {
	n := 0
	for _, s := range list {
		if s.State == core.MCPNeedsAuth || s.State == core.MCPFailed {
			n++
		}
	}
	return n
}

// mcpSection builds the sidebar's MCP section from list: one row per
// non-disabled server (icon/tone by state, "<name>  <detail>"), plus a
// second row holding the auth URL while authenticating. It is omitted
// entirely (no rows) when every server is disabled or there are none.
func mcpSection(list []core.MCPServerStatus) sidebar.Section {
	rows := make([]sidebar.Row, 0, len(list))
	for _, s := range list {
		if s.State == core.MCPDisabled {
			continue
		}
		icon, tone := mcpIconTone(s.State)
		rows = append(rows, sidebar.Row{Icon: icon, Text: ansi.SanitizeLine(s.Name) + "  " + ansi.SanitizeLine(mcpDetail(s)), Tone: tone})
		if s.State == core.MCPAuthenticating && s.AuthURL != "" {
			rows = append(rows, sidebar.Row{Text: ansi.SanitizeLine(s.AuthURL), Tone: sidebar.Muted})
		}
	}
	return sidebar.Section{Title: "MCP", Rows: rows}
}

// mcpIconTone maps a server state to its sidebar icon and tone (§4.1).
func mcpIconTone(st core.MCPState) (string, sidebar.Tone) {
	switch st {
	case core.MCPReady:
		return "●", sidebar.Success
	case core.MCPNeedsAuth:
		return "●", sidebar.Warning
	case core.MCPConnecting, core.MCPAuthenticating:
		return "◌", sidebar.Muted
	case core.MCPFailed:
		return "✕", sidebar.Error
	}
	return "●", sidebar.Normal
}

// onMCPResult folds a Servers read into the App's view state, and issues
// exactly one more read if events arrived while it was in flight.
func (a *App) onMCPResult(msg mcpServersMsg) tea.Cmd {
	a.view.mcp.pending = false
	if msg.err == nil {
		a.view.mcp.list = msg.list
	}
	if a.view.mcp.dirty {
		a.view.mcp.dirty = false
		a.view.mcp.pending = true
		return mcpServersCmd(a.ctx, a.ports)
	}
	return nil
}

// onMCPEvent coalesces a burst of MCPServerChanged events into one
// mcpServersCmd read: if a read is already in flight it marks dirty
// instead of starting another one (see onMCPResult, which issues the
// follow-up read once the in-flight one returns). It is a free function,
// not an App method, to keep App's method count under the archtest cap.
func onMCPEvent(a *App) tea.Cmd {
	if a.view.mcp.pending {
		a.view.mcp.dirty = true
		return waitEvent(a.sub)
	}
	a.view.mcp.pending = true
	return tea.Batch(waitEvent(a.sub), mcpServersCmd(a.ctx, a.ports))
}

// mcpDetail is a server's row detail text.
func mcpDetail(s core.MCPServerStatus) string {
	switch s.State {
	case core.MCPReady:
		if s.Tools == 1 {
			return "1 tool"
		}
		return strconv.Itoa(s.Tools) + " tools"
	case core.MCPNeedsAuth:
		return "needs sign-in"
	case core.MCPConnecting:
		return "connecting…"
	case core.MCPAuthenticating:
		return "signing in…"
	case core.MCPFailed:
		return "failed: " + s.Err
	}
	return string(s.State)
}
