package ui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
)

func sidebarModel(secs []sidebar.Section, w, h int) sidebar.Model {
	m := sidebar.New()
	m.SetSize(w, h)
	m.SetSections(secs)
	return m
}

func TestMCPSection_States(t *testing.T) {
	t.Parallel()
	list := []core.MCPServerStatus{
		{Name: "gh", State: core.MCPReady, Tools: 5},
		{Name: "linear", State: core.MCPNeedsAuth},
		{Name: "slack", State: core.MCPConnecting},
		{Name: "notion", State: core.MCPAuthenticating, AuthURL: "https://example.com/auth"},
		{Name: "jira", State: core.MCPFailed, Err: "this is a very long reason that should get truncated at the edge"},
		{Name: "disabled-one", State: core.MCPDisabled},
	}
	m := sidebarModel([]sidebar.Section{mcpSection(list)}, 36, 10)
	golden.Assert(t, "mcp_sidebar_states", m.View())
}

func TestMCPSection_Sanitizes(t *testing.T) {
	t.Parallel()
	list := []core.MCPServerStatus{
		{Name: "gh\x1b]0;x\a", State: core.MCPFailed, Err: "bad\x1b]0;x\a reason"},
	}
	sec := mcpSection(list)
	for _, r := range sec.Rows {
		if strings.ContainsRune(r.Text, '\x1b') {
			t.Fatalf("row text = %q, want no ESC", r.Text)
		}
	}
}

func TestMCPSection_HiddenWhenNone(t *testing.T) {
	t.Parallel()
	sec := mcpSection(nil)
	if len(sec.Rows) != 0 {
		t.Fatalf("mcpSection(nil).Rows = %v, want none", sec.Rows)
	}
	sec = mcpSection([]core.MCPServerStatus{{Name: "x", State: core.MCPDisabled}})
	if len(sec.Rows) != 0 {
		t.Fatalf("mcpSection(all disabled).Rows = %v, want none", sec.Rows)
	}
}

func TestApp_MCPBadgeOnlyWhenSidebarHidden(t *testing.T) {
	t.Parallel()
	list := []core.MCPServerStatus{
		{Name: "gh", State: core.MCPNeedsAuth},
		{Name: "jira", State: core.MCPFailed, Err: "boom"},
	}
	ta := newTestApp(t, withMCP(list), withSize(200, 30))
	if got := xansi.Strip(ta.view()); strings.Contains(got, "mcp 2!") {
		t.Fatalf("view with sidebar visible shows the badge: %q", got)
	}

	ta2 := newTestApp(t, withMCP(list), withSize(80, 30))
	if got := xansi.Strip(ta2.view()); !strings.Contains(got, "mcp 2!") {
		t.Fatalf("view with sidebar hidden = %q, want it to contain \"mcp 2!\"", got)
	}
}

func TestApp_MCPEventRereadsServers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withMCP(nil))
	before := ta.mcp.calls()

	// Cmds run synchronously in this harness, so to see the coalescing a
	// burst produces in the real runtime (where a Cmd runs later, not
	// inline), drive Update directly without running the returned Cmds
	// until we choose to: three events arrive before the first read
	// completes.
	_, cmd1 := ta.app.Update(eventMsg{ev: event.MCPServerChanged{Name: "gh", State: core.MCPReady}})
	_, cmd2 := ta.app.Update(eventMsg{ev: event.MCPServerChanged{Name: "gh", State: core.MCPReady}})
	_, cmd3 := ta.app.Update(eventMsg{ev: event.MCPServerChanged{Name: "gh", State: core.MCPReady}})
	_ = cmd2
	_ = cmd3
	if got := ta.mcp.calls() - before; got != 0 {
		t.Fatalf("Servers calls before any Cmd ran = %d, want 0", got)
	}

	// Running the first event's Cmd performs the one coalesced read;
	// because a later event of the burst arrived while it was in
	// flight, the result triggers exactly one more read (2 total).
	ta.run(cmd1)
	if got := ta.mcp.calls() - before; got != 2 {
		t.Fatalf("Servers calls after the burst and its dirty re-read = %d, want 2", got)
	}
}

func TestApp_MCPNilPort(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	got := xansi.Strip(ta.view())
	if strings.Contains(got, "MCP") {
		t.Fatalf("view with no MCP port shows a section: %q", got)
	}
	ta.event(event.MCPServerChanged{Name: "gh", State: core.MCPReady})
	// No panic, and nothing rendered changes.
}
