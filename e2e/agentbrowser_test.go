//go:build jigtest

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// prependPath prepends dir to env's PATH entry.
func prependPath(env *testEnv, dir string) {
	for i, v := range env.vars {
		if strings.HasPrefix(v, "PATH=") {
			env.vars[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(v, "PATH=")
		}
	}
}

// writeAgentBrowserFake writes a fake `agent-browser` shell script to
// root/bin, on env's PATH, and a bundled skill at root/abskills. The
// script answers `skills path` with root/abskills (logging the
// invocation to root/calls.log), `snapshot` with a button ref, and
// anything else with "ran $*".
func writeAgentBrowserFake(t *testing.T, env *testEnv) {
	t.Helper()
	skillsDir := filepath.Join(env.root, "abskills")
	callsLog := filepath.Join(env.root, "calls.log")
	writeFile(t, filepath.Join(skillsDir, "agent-browser", "SKILL.md"), `---
name: agent-browser
description: drive a browser
---
Drive a browser from the command line.
`)

	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "skills" ] && [ "$2" = "path" ]; then
  echo "$*" >> "%s"
  echo "%s"
  exit 0
fi
if [ "$1" = "snapshot" ]; then
  echo '- button "Go" [ref=e1]'
  exit 0
fi
echo "ran $*"
`, callsLog, skillsDir)

	bin := filepath.Join(env.root, "bin", "agent-browser")
	writeFile(t, bin, script)
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	prependPath(env, filepath.Join(env.root, "bin"))
}

// countLines counts path's non-empty lines, or 0 if it does not exist.
func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for line := range strings.Lines(string(data)) {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func TestE2E_AgentBrowserPresetAndSkill(t *testing.T) {
	env := newEnv(t)
	writeAgentBrowserFake(t, env)

	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{
				ExpectSystemContains: []string{"agent-browser"},
				Calls: []jigtest.Call{
					call("s1", "bash", `{"command":"agent-browser snapshot -i"}`),
					call("c1", "bash", `{"command":"agent-browser click @e1"}`),
				},
			},
			{
				ExpectPromptContains: []string{"[ref=e1]", "permission required"},
				Text:                 "done",
			},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "use agent-browser to click Go")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "done") {
		t.Errorf("stdout = %q, want the final text", stdout)
	}
	callsLog := filepath.Join(env.root, "calls.log")
	if n := countLines(t, callsLog); n != 1 {
		t.Fatalf("calls.log has %d lines after the first run, want 1", n)
	}

	// A second run, with a fresh one-turn script, must not re-run `skills
	// path`: the binary's mtime is unchanged, so the cached prefs entry
	// is used.
	script2 := writeScript(t, env, "script2.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "done again"}},
	}})
	writeConfig(t, env, jigtestConfig(script2, ""))

	stdout, stderr, code = runPrompt(t, env, "say hi")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "done again") {
		t.Errorf("stdout = %q, want the second run's reply", stdout)
	}
	if n := countLines(t, callsLog); n != 1 {
		t.Errorf("calls.log has %d lines after the second run, want 1 (cached)", n)
	}
}
