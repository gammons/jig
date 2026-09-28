//go:build jigtest

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

const untrustedWarning = "warning: project config not trusted; loosening settings ignored (use --trust-project)"

// setupWrite points env's global config at a fresh script whose m1 writes
// name and then says "done".
func setupWrite(t *testing.T, env *testEnv, name string) {
	t.Helper()
	input := `{"path":"` + name + `","content":"hi"}`
	script := writeScript(t, env, name+".json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("w1", "write", input)}},
			{Text: "done"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))
}

func wantFile(t *testing.T, env *testEnv, name string, exists bool) {
	t.Helper()
	_, err := os.Stat(filepath.Join(env.work, name))
	if exists && err != nil {
		t.Errorf("%s: stat err = %v, want it written", name, err)
	}
	if !exists && !os.IsNotExist(err) {
		t.Errorf("%s: stat err = %v, want not-exist", name, err)
	}
}

func wantWarning(t *testing.T, stderr string, want bool) {
	t.Helper()
	if got := strings.Count(stderr, untrustedWarning+"\n"); got != map[bool]int{true: 1, false: 0}[want] {
		t.Errorf("stderr has the untrusted warning %d times, want %v:\n%s", got, want, stderr)
	}
}

func TestE2E_UntrustedProjectCannotLoosen(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".jig", "config.toml"), "[permissions]\nwrite = \"allow\"\n")
	setupWrite(t, env, "x.txt")

	stdout, stderr, code := runPrompt(t, env, "write x")
	wantCode(t, code, 0, stdout, stderr)
	wantWarning(t, stderr, true)
	wantFile(t, env, "x.txt", false)
}

func TestE2E_TrustProjectFlag(t *testing.T) {
	env := newEnv(t)
	projectCfg := filepath.Join(env.work, ".jig", "config.toml")
	writeFile(t, projectCfg, "[permissions]\nwrite = \"allow\"\n")

	setupWrite(t, env, "x.txt")
	stdout, stderr, code := runPrompt(t, env, "--trust-project", "write x")
	wantCode(t, code, 0, stdout, stderr)
	wantWarning(t, stderr, false)
	wantFile(t, env, "x.txt", true)

	setupWrite(t, env, "y.txt")
	stdout, stderr, code = runPrompt(t, env, "write y")
	wantCode(t, code, 0, stdout, stderr)
	wantWarning(t, stderr, false)
	wantFile(t, env, "y.txt", true)

	writeFile(t, projectCfg, "[permissions]\nwrite = \"allow\"\n# changed\n")
	setupWrite(t, env, "z.txt")
	stdout, stderr, code = runPrompt(t, env, "write z")
	wantCode(t, code, 0, stdout, stderr)
	wantWarning(t, stderr, true)
	wantFile(t, env, "z.txt", false)
}

// A permission whose value is a {file:} token can't be applied untrusted;
// it still counts as dropped, so the warning prints (once).
func TestE2E_UntrustedTokenPermissionWarns(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".jig", "mode"), "allow\n")
	writeFile(t, filepath.Join(env.work, ".jig", "config.toml"), "[permissions]\nwrite = \"{file:mode}\"\n")
	setupWrite(t, env, "x.txt")

	stdout, stderr, code := runPrompt(t, env, "write x")
	wantCode(t, code, 0, stdout, stderr)
	wantWarning(t, stderr, true)
	wantFile(t, env, "x.txt", false)
}
