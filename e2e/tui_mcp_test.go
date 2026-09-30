//go:build jigtest

package e2e

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/creack/pty"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// TestTUI_MCPSidebar drives the built binary under a pty wide enough for
// the sidebar (120+ columns) and checks the MCP section shows the
// configured server and its tool count. The server config lives in the
// global config file, which is trusted by default (a project .mcp.json
// would trigger the trust dialog instead).
func TestTUI_MCPSidebar(t *testing.T) {
	env := newEnv(t)
	fakeScript := writeMCPFakeScript(t, env, "mcpfake-script.json", mcpFakeScript{
		Tools: []mcpFakeTool{
			{Name: "echo", Schema: json.RawMessage(`{"type":"object"}`), Reply: mcpFakeReply{Text: "hello"}},
			{Name: "add", Schema: json.RawMessage(`{"type":"object"}`), Reply: mcpFakeReply{Text: "3"}},
		},
	})
	script := writeScript(t, env, "tui-mcp.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "hello from jig"}},
	}})
	writeConfig(t, env, jigtestConfig(script, mcpConfig(fakeScript)))

	ctx, cancel := context.WithTimeout(context.Background(), tuiTimeout)
	defer cancel()
	cmd := command(ctx, env, "--cwd", env.work)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "JIG_IMAGES=off")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 140})
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	scr := newScreen()
	go scr.read(tty)

	scr.waitFor(ctx, t, 0, "fake")
	scr.waitFor(ctx, t, 0, "2 tools")

	if _, err := tty.WriteString("\x04"); err != nil {
		t.Fatalf("writing quit key: %v", err)
	}

	err = cmd.Wait()
	if code := exitCode(t, err, scr.snapshot()); code != 0 {
		t.Fatalf("jig exited %d\nscreen:\n%s", code, scr.snapshot())
	}
}
