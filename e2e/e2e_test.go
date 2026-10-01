//go:build jigtest

package e2e

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

func TestE2E_TextReply(t *testing.T) {
	env := newEnv(t)
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "hello from jigtest"}},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "say hello")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "hello from jigtest") {
		t.Errorf("stdout = %q, want the scripted text", stdout)
	}
	if ids := sessionIDs(t, env); len(ids) != 1 {
		t.Errorf("jig sessions lists %d sessions (%v), want 1", len(ids), ids)
	}
}

func TestE2E_DebugLog(t *testing.T) {
	setup := func() *testEnv {
		env := newEnv(t)
		script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
			"m1": {{Text: "hello from jigtest"}},
		}})
		writeConfig(t, env, jigtestConfig(script, ""))
		return env
	}

	env := setup()
	env.vars = append(env.vars, "JIG_DEBUG=1")
	stdout, stderr, code := runPrompt(t, env, "say hello")
	wantCode(t, code, 0, stdout, stderr)
	got, err := os.ReadFile(filepath.Join(env.work, "jig-debug.log"))
	if err != nil {
		t.Fatalf("read jig-debug.log: %v", err)
	}
	for _, want := range []string{"cat=app", `msg="run start"`, `msg="step end"`, "outcome=done"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("jig-debug.log = %q, want it to contain %q", got, want)
		}
	}

	off := setup()
	stdout, stderr, code = runPrompt(t, off, "say hello")
	wantCode(t, code, 0, stdout, stderr)
	if _, err := os.Stat(filepath.Join(off.work, "jig-debug.log")); !os.IsNotExist(err) {
		t.Errorf("jig-debug.log without JIG_DEBUG: stat err = %v, want not-exist", err)
	}
}

// writeScriptedWrite configures env so m1 writes out.txt, then finishes.
func writeScriptedWrite(t *testing.T, env *testEnv) {
	t.Helper()
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("w1", "write", `{"path":"out.txt","content":"hi"}`)}},
			{Text: "done"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))
}

func TestE2E_WriteDeniedWithoutYes(t *testing.T) {
	env := newEnv(t)
	writeScriptedWrite(t, env)

	stdout, stderr, code := runPrompt(t, env, "write a file")
	wantCode(t, code, 0, stdout, stderr)
	if _, err := os.Stat(filepath.Join(env.work, "out.txt")); !os.IsNotExist(err) {
		t.Errorf("out.txt: stat err = %v, want not-exist", err)
	}
	if !strings.Contains(stderr, "→ write") || !strings.Contains(stderr, "✗") {
		t.Errorf("stderr = %q, want a denied write (→ write, ✗)", stderr)
	}
}

func TestE2E_WriteAllowedWithYes(t *testing.T) {
	env := newEnv(t)
	writeScriptedWrite(t, env)

	stdout, stderr, code := runPrompt(t, env, "--yes", "write a file")
	wantCode(t, code, 0, stdout, stderr)
	got, err := os.ReadFile(filepath.Join(env.work, "out.txt"))
	if err != nil || string(got) != "hi" {
		t.Errorf("out.txt = %q, %v; want %q", got, err, "hi")
	}
	if strings.Contains(stderr, "✗") {
		t.Errorf("stderr = %q, want no failed tool call", stderr)
	}
}

// assertSubagentRouting runs a script in which m1 spawns agentName via
// task, the subagent answers on m2, and m1 then finishes. If the subagent
// ran on m1 it would consume m1's final turn, leaving the parent with none
// (exit 1); exit 0 proves m2's queue served the subagent.
func assertSubagentRouting(t *testing.T, env *testEnv, extra, agentName string) {
	t.Helper()
	input := `{"agent":"` + agentName + `","description":"look around","prompt":"find the thing"}`
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("t1", "task", input)}},
			{Text: "parent done"},
		},
		"m2": {{Text: "found it"}},
	}})
	writeConfig(t, env, jigtestConfig(script, extra))

	stdout, stderr, code := runPrompt(t, env, "delegate")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "parent done") {
		t.Errorf("stdout = %q, want the parent's final text", stdout)
	}
	if !strings.Contains(stderr, "↳ "+agentName+" (jigtest/m2): look around") {
		t.Errorf("stderr = %q, want a %s spawn line naming its model jigtest/m2", stderr, agentName)
	}
	if strings.Contains(stderr, "✗") {
		t.Errorf("stderr = %q, want no failed tool call", stderr)
	}
}

func TestE2E_SubagentUsesItsModel(t *testing.T) {
	env := newEnv(t)
	assertSubagentRouting(t, env, "\n[agents.explore]\nmodel = \"jigtest/m2\"\n", "explore")
}

func TestE2E_ClaudeAgentWithAlias(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".claude", "agents", "reviewer.md"), `---
description: Reviews code
model: haiku
tools: Read
---
You review code.
`)
	assertSubagentRouting(t, env, "\n[model_aliases]\nhaiku = \"jigtest/m2\"\n", "reviewer")
}

func TestE2E_SkillListedAndLoaded(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".agents", "skills", "demo", "SKILL.md"), `---
name: demo
description: A demo skill
---
Demo skill body.
`)
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{
				ExpectSystemContains: []string{"<id>demo</id>"},
				Calls:                []jigtest.Call{call("s1", "skill", `{"id":"demo"}`)},
			},
			{Text: "skill loaded"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "use the demo skill")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stderr, `→ skill {"id":"demo"}`) {
		t.Errorf("stderr = %q, want the skill call", stderr)
	}
	if strings.Contains(stderr, "✗") {
		t.Errorf("stderr = %q, want the skill to load without error", stderr)
	}
	if !strings.Contains(stdout, "skill loaded") {
		t.Errorf("stdout = %q, want the final text", stdout)
	}
}

func TestE2E_AgentsMDInPrompt(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, "AGENTS.md"), "Project rule: zebra-marker-42.\n")
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{ExpectSystemContains: []string{"zebra-marker-42"}, Text: "ok"}},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "hi")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "ok") {
		t.Errorf("stdout = %q, want the scripted text", stdout)
	}
}

// TestE2E_MissingSystemTextFails is the control for the ExpectSystemContains
// tests: a missing expectation fails the run.
func TestE2E_MissingSystemTextFails(t *testing.T) {
	env := newEnv(t)
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{ExpectSystemContains: []string{"zebra-marker-42"}, Text: "ok"}},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "hi")
	wantCode(t, code, 1, stdout, stderr)
	if !strings.Contains(stderr, "zebra-marker-42") {
		t.Errorf("stderr = %q, want the missing system text named", stderr)
	}
}

// anthropicModel returns the first model `jig models anthropic` lists.
func anthropicModel(t *testing.T, env *testEnv) string {
	t.Helper()
	out, errOut, code := runJig(t, env, "models", "anthropic")
	wantCode(t, code, 0, out, errOut)
	for line := range strings.Lines(out) {
		if f := strings.Fields(line); len(f) > 0 && strings.HasPrefix(f[0], "anthropic/") {
			return f[0]
		}
	}
	t.Fatalf("jig models anthropic listed no models:\n%s", out)
	return ""
}

func TestE2E_MissingCredentialsExits2(t *testing.T) {
	env := newEnv(t)
	writeConfig(t, env, "")
	model := anthropicModel(t, env)
	writeConfig(t, env, `default_model = "`+model+`"`+"\n")

	stdout, stderr, code := runPrompt(t, env, "hi")
	wantCode(t, code, 2, stdout, stderr)
	if !strings.Contains(stderr, "ANTHROPIC_API_KEY") {
		t.Errorf("stderr = %q, want it to name ANTHROPIC_API_KEY", stderr)
	}
}

func TestE2E_UnknownModelExits2(t *testing.T) {
	env := newEnv(t)
	script := writeScript(t, env, "script.json", jigtest.Script{})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "--model", "jigtest/nope", "hi")
	wantCode(t, code, 2, stdout, stderr)
	if !strings.Contains(stderr, "jigtest/nope") {
		t.Errorf("stderr = %q, want it to name the unknown model", stderr)
	}
}

func TestE2E_ResumeAfterCancelledRun(t *testing.T) {
	env := newEnv(t)
	script1 := writeScript(t, env, "script1.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Calls: []jigtest.Call{call("b1", "bash", `{"command":"sleep 30"}`)}}},
	}})
	writeConfig(t, env, jigtestConfig(script1, ""))

	stderr, code := runUntilBashThenInterrupt(t, env, "--yes", "run something slow")
	wantCode(t, code, 1, "", stderr)

	ids := sessionIDs(t, env)
	if len(ids) != 1 {
		t.Fatalf("jig sessions lists %v, want one session", ids)
	}
	script2 := writeScript(t, env, "script2.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "resumed fine"}},
	}})
	writeConfig(t, env, jigtestConfig(script2, ""))

	stdout, stderr, code := runPrompt(t, env, "--session", ids[0], "carry on")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "resumed fine") {
		t.Errorf("stdout = %q, want the resumed reply", stdout)
	}
}

// runUntilBashThenInterrupt runs `jig run`, sends SIGINT once stderr shows
// a line starting with "→ bash", and returns stderr and the exit code.
func runUntilBashThenInterrupt(t *testing.T, env *testEnv, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := command(ctx, env, append([]string{"run", "--cwd", env.work}, args...)...)
	pipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	sc := bufio.NewScanner(pipe)
	signalled := false
	for sc.Scan() {
		line := sc.Text()
		stderr.WriteString(line + "\n")
		if !signalled && strings.HasPrefix(line, "→ bash") {
			if err := cmd.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			signalled = true
		}
	}
	_, _ = io.Copy(io.Discard, pipe)
	err = cmd.Wait()
	if !signalled {
		t.Fatalf("stderr never showed → bash:\n%s", stderr.String())
	}
	return stderr.String(), exitCode(t, err, stderr.String())
}
