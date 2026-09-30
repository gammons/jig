package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestMCPCmd_UnknownName(t *testing.T) {
	env := newTestEnv(t)
	env.writeConfig(t, `[mcp.servers.gh]
transport = "http"
url = "http://example.com"
`)
	code, _, stderr := env.run(t, "mcp", "auth", "nope", "--cwd", env.workDir)
	if code != exitConfig {
		t.Fatalf("exit = %d, want %d; stderr=%s", code, exitConfig, stderr)
	}

	code, _, stderr = env.run(t, "mcp", "logout", "nope", "--cwd", env.workDir)
	if code != exitConfig {
		t.Fatalf("logout exit = %d, want %d; stderr=%s", code, exitConfig, stderr)
	}
}

// TestMCPCmd_UntrustedProjectHidden checks that a project .mcp.json with
// no trust grant produces no row in `jig mcp list`, since the untrusted
// project layer has no MCP servers.
func TestMCPCmd_UntrustedProjectHidden(t *testing.T) {
	env := newTestEnv(t)
	env.writeProject(t, ".mcp.json", `{"mcpServers": {"untrusted": {"command": "true"}}}`)

	var out, errw bytes.Buffer
	code := Run(t.Context(), []string{"mcp", "list", "--cwd", env.workDir}, Stdio{In: strings.NewReader(""), Out: &out, Err: &errw}, env.getenv)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d; stderr=%s", code, exitOK, errw.String())
	}
	if strings.Contains(out.String(), "untrusted") {
		t.Errorf("list output = %q, want no row for the untrusted server", out.String())
	}
}

// TestMCPCmd_ListSanitizes pins that an Err field holding ESC never
// reaches the printed table.
func TestMCPCmd_ListSanitizes(t *testing.T) {
	var buf bytes.Buffer
	printMCPTable(&buf, "/work", []core.MCPServerStatus{
		{Name: "evil", Source: "/work/x", Transport: core.MCPStdio, State: core.MCPFailed, Err: "boom \x1b[31mred\x1b[0m"},
	})
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("output contains ESC: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "evil") || !strings.Contains(buf.String(), "boom") {
		t.Errorf("output missing expected content: %q", buf.String())
	}
}
