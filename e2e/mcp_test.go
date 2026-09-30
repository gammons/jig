//go:build jigtest

package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// mcpFakeScript is the fake server's own script format (e2e/testdata/mcpfake).
type mcpFakeReply struct {
	Text string `json:"text"`
}
type mcpFakeTool struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Reply  mcpFakeReply    `json:"reply"`
}
type mcpFakeScript struct {
	Tools []mcpFakeTool `json:"tools"`
}

// writeMCPFakeScript writes the fake server's script to env.root/name and
// returns its path.
func writeMCPFakeScript(t *testing.T, env *testEnv, name string, s mcpFakeScript) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.root, name)
	writeFile(t, path, string(data))
	return path
}

// mcpConfig returns the [mcp.servers.fake] TOML table pointing at
// mcpfakeBin, scripted by fakeScript.
func mcpConfig(fakeScript string) string {
	return "\n[mcp.servers.fake]\ncommand = " + tomlString(mcpfakeBin) + "\nenv = { MCPFAKE_SCRIPT = " + tomlString(fakeScript) + " }\n"
}

func tomlString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func TestRun_MCPToolCall(t *testing.T) {
	env := newEnv(t)
	fakeScript := writeMCPFakeScript(t, env, "mcpfake-script.json", mcpFakeScript{
		Tools: []mcpFakeTool{{Name: "echo", Schema: json.RawMessage(`{"type":"object"}`), Reply: mcpFakeReply{Text: "hello from fake"}}},
	})
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("e1", "mcp__fake__echo", `{}`)}},
			{ExpectPromptContains: []string{"hello from fake"}, Text: "done"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, mcpConfig(fakeScript)))

	stdout, stderr, code := runPrompt(t, env, "--yes", "call the fake tool")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "done") {
		t.Errorf("stdout = %q, want the final text", stdout)
	}
}

func TestMCPList(t *testing.T) {
	env := newEnv(t)
	fakeScript := writeMCPFakeScript(t, env, "mcpfake-script.json", mcpFakeScript{
		Tools: []mcpFakeTool{{Name: "echo", Schema: json.RawMessage(`{"type":"object"}`), Reply: mcpFakeReply{Text: "hello from fake"}}},
	})
	script := writeScript(t, env, "script.json", jigtest.Script{})
	writeConfig(t, env, jigtestConfig(script, mcpConfig(fakeScript)))

	stdout, stderr, code := runJig(t, env, "mcp", "list")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "fake") || !strings.Contains(stdout, "stdio") ||
		!strings.Contains(stdout, "ready") || !strings.Contains(stdout, "1") {
		t.Errorf("stdout = %q, want a row for fake: stdio ready 1", stdout)
	}
}
