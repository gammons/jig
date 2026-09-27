//go:build jigtest

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// TestE2E_SubagentCannotEscapePlanPermissions: with bash allowed in
// config, the plan agent (bash = ask, which headless answers with deny)
// delegates to general, which tries bash. The ancestor's ask must still
// apply, so the file is never created.
func TestE2E_SubagentCannotEscapePlanPermissions(t *testing.T) {
	env := newEnv(t)
	input := `{"agent":"general","description":"make a file","prompt":"touch x"}`
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("t1", "task", input)}},
			{Text: "parent done"},
		},
		"m2": {
			{Calls: []jigtest.Call{call("b1", "bash", `{"command":"touch x"}`)}},
			{Text: "child done"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, "\n[permissions]\nbash = \"allow\"\n\n[agents.general]\nmodel = \"jigtest/m2\"\n"))

	stdout, stderr, code := runPrompt(t, env, "--agent", "plan", "delegate")
	wantCode(t, code, 0, stdout, stderr)
	if _, err := os.Stat(filepath.Join(env.work, "x")); !os.IsNotExist(err) {
		t.Errorf("x: stat err = %v, want not-exist (subagent escaped plan's bash = ask)", err)
	}
	if !strings.Contains(stderr, "✗") {
		t.Errorf("stderr = %q, want a denied bash (✗)", stderr)
	}
}
