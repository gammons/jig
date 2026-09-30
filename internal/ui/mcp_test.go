package ui

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/actions"
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

func TestApp_MCPPickerSignIn(t *testing.T) {
	t.Parallel()
	list := []core.MCPServerStatus{{Name: "gh", State: core.MCPNeedsAuth}}
	ta := newTestApp(t, withMCP(list))
	ta.key("ctrl+p")
	ta.typeText("MCP servers")
	ta.key("enter")
	ta.typeText("gh")
	ta.key("enter")
	ta.typeText("Sign in")
	ta.key("enter")
	if !slicesContains(ta.mcp.authCalls, "gh") {
		t.Fatalf("authCalls = %v, want gh", ta.mcp.authCalls)
	}
	if ta.app.w.picker.IsOpen() {
		t.Error("picker still open after choosing an MCP action")
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestApp_MCPPickerItemsByState(t *testing.T) {
	t.Parallel()
	list := []core.MCPServerStatus{
		{Name: "gh", State: core.MCPNeedsAuth},
		{Name: "linear", State: core.MCPAuthenticating, AuthURL: "https://example.com/auth"},
		{Name: "slack", State: core.MCPReady, HasToken: true},
	}
	ta := newTestApp(t, withMCP(list))

	needsAuth := loadItems(ta, picker.Level{ID: "mcp.server", Arg: "gh"})
	wantIDs(t, needsAuth, []string{"signin", "reconnect", "tools"})

	authing := loadItems(ta, picker.Level{ID: "mcp.server", Arg: "linear"})
	wantIDs(t, authing, []string{"cancel", "copy", "tools"})

	ready := loadItems(ta, picker.Level{ID: "mcp.server", Arg: "slack"})
	wantIDs(t, ready, []string{"reconnect", "signout", "tools"})
}

func wantIDs(t *testing.T, items []picker.Item, want []string) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("items = %v, want %v", items, want)
	}
	for i, id := range want {
		if items[i].ID != id {
			t.Fatalf("items = %v, want %v", items, want)
		}
	}
}

func TestApp_MCPPickerNilPort(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	items := loadItems(ta, picker.Level{ID: "mcp"})
	if len(items) != 1 || !items[0].Disabled || items[0].Title != "no MCP servers configured" {
		t.Fatalf("items = %+v, want one disabled item", items)
	}
	root := loadItems(ta, rootLevel())
	act := itemByID(t, root, string(actions.MCPServers))
	if act.Title != "MCP servers…" || act.Group != "MCP" || act.Drill == nil {
		t.Fatalf("MCP servers action = %+v", act)
	}
}

func TestApp_MCPPickerSanitizes(t *testing.T) {
	t.Parallel()
	evil := "gh\x1b]0;x\a\x1b[31mred"
	list := []core.MCPServerStatus{{Name: evil, State: core.MCPReady}}
	ta := newTestApp(t, withMCP(list))
	items := loadItems(ta, picker.Level{ID: "mcp"})
	for _, it := range items {
		if strings.ContainsRune(it.Title, '\x1b') {
			t.Fatalf("item title = %q, want no ESC", it.Title)
		}
	}

	// Drill mcp -> <escaped name> -> Tools…, and assert the picker's
	// rendered breadcrumb titles never leak the injected escapes (the
	// theme's own styling escapes are fine, so we assert on the
	// specific injected sequences, not on "\x1b" in general).
	ta.key("ctrl+p")
	ta.typeText("MCP servers")
	ta.key("enter")
	ta.typeText("gh")
	ta.key("enter")
	view := ta.view()
	if strings.Contains(view, "\x1b]0;x\a") || strings.Contains(view, "\x1b[31m") {
		t.Fatalf("mcp.server breadcrumb leaked the injected escape:\n%q", view)
	}
	ta.typeText("Tools")
	ta.key("enter")
	view = ta.view()
	if strings.Contains(view, "\x1b]0;x\a") || strings.Contains(view, "\x1b[31m") {
		t.Fatalf("mcp.tools breadcrumb leaked the injected escape:\n%q", view)
	}
}

func TestApp_MCPPickerCopyURL(t *testing.T) {
	t.Parallel()
	list := []core.MCPServerStatus{{Name: "linear", State: core.MCPAuthenticating, AuthURL: "https://example.com/auth"}}
	ta := newTestApp(t, withMCP(list))
	cmd := pickerCtl{ta.app}.chosen(picker.ChosenMsg{
		Level: picker.Level{ID: "mcp.server", Arg: "linear"},
		Items: []picker.Item{{ID: "copy"}},
	})
	if cmd == nil {
		t.Fatal("copy: want a Cmd")
	}
	if name := fmt.Sprintf("%T", cmd()); !strings.Contains(name, "ClipboardMsg") {
		t.Errorf("copy Cmd produced %s, want tea's clipboard message", name)
	}
}
