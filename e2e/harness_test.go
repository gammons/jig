//go:build jigtest

// Package e2e runs the jig binary, built with the jigtest provider, against
// scripted models in a sandboxed HOME and XDG tree.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// runTimeout bounds every jig invocation.
const runTimeout = 30 * time.Second

// jigBin is the path of the jig binary TestMain builds. It is set once,
// before any test runs, and only read afterwards.
var jigBin string

func TestMain(m *testing.M) {
	os.Exit(buildAndRun(m))
}

func buildAndRun(m *testing.M) int {
	dir, err := os.MkdirTemp("", "jig-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	jigBin = filepath.Join(dir, "jig")
	build := exec.Command("go", "build", "-tags", "jigtest", "-o", jigBin, "./cmd/jig")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: building jig: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// testEnv is one test's sandbox: a fresh HOME and XDG tree, a work dir
// that is a git repo, and the process environment jig runs with.
type testEnv struct {
	root      string
	work      string
	configDir string // $XDG_CONFIG_HOME
	vars      []string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	e := &testEnv{
		root:      root,
		work:      filepath.Join(root, "work"),
		configDir: filepath.Join(root, "config"),
	}
	home := filepath.Join(root, "home")
	for _, d := range []string{home, e.configDir, filepath.Join(root, "data"), filepath.Join(root, "cache"), filepath.Join(e.work, ".git")} {
		mkdir(t, d)
	}
	e.vars = []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + e.configDir,
		"XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"ANTHROPIC_API_KEY=",
		"CATWALK_URL=http://127.0.0.1:1",
		"PATH=" + os.Getenv("PATH"),
	}
	return e
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeFile writes content to path, creating its parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeConfig writes env's user config.toml.
func writeConfig(t *testing.T, env *testEnv, toml string) {
	t.Helper()
	writeFile(t, filepath.Join(env.configDir, "jig", "config.toml"), toml)
}

// writeScript writes s as env.root/name and returns its absolute path.
func writeScript(t *testing.T, env *testEnv, name string, s jigtest.Script) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.root, name)
	writeFile(t, path, string(data))
	return path
}

// jigtestConfig is a config whose default model is jigtest/m1, scripted by
// the file at script, followed by extra (TOML tables). Titles run on
// jigtest/title, whose queue the tests leave empty: a failed title is
// ignored, and it keeps the background title request off m1's queue.
// Only m1 accepts images.
func jigtestConfig(script, extra string) string {
	return fmt.Sprintf(`default_model = "jigtest/m1"
small_model = "jigtest/title"

[providers.jigtest]
type = "jigtest"
models = ["m1", "m2", "title"]
image_models = ["m1"]
options = { script = %q }
%s`, script, extra)
}

// command returns a jig command running in env's work dir.
func command(ctx context.Context, env *testEnv, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, jigBin, args...)
	cmd.Dir = env.work
	cmd.Env = env.vars
	return cmd
}

// runJig runs jig with args and returns its output and exit code.
func runJig(t *testing.T, env *testEnv, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := command(ctx, env, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), exitCode(t, err, errOut.String())
}

// exitCode maps cmd.Run/Wait's error to the process exit code, failing
// the test if the process did not exit normally.
func exitCode(t *testing.T, err error, stderr string) int {
	t.Helper()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee) && ee.Exited():
		return ee.ExitCode()
	default:
		t.Fatalf("jig did not exit normally: %v\nstderr:\n%s", err, stderr)
		return -1
	}
}

// runPrompt runs `jig run --cwd <work> [flags...] <prompt>`.
func runPrompt(t *testing.T, env *testEnv, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runJig(t, env, append([]string{"run", "--cwd", env.work}, args...)...)
}

// sessionIDs returns the IDs `jig sessions` lists, newest first.
func sessionIDs(t *testing.T, env *testEnv) []string {
	t.Helper()
	out, errOut, code := runJig(t, env, "sessions")
	if code != 0 {
		t.Fatalf("jig sessions: exit %d\n%s", code, errOut)
	}
	var ids []string
	for line := range strings.Lines(out) {
		if f := strings.Fields(line); len(f) > 0 {
			ids = append(ids, f[0])
		}
	}
	return ids
}

// call builds a scripted tool call.
func call(id, name, input string) jigtest.Call {
	return jigtest.Call{ID: id, Name: name, Input: json.RawMessage(input)}
}

func wantCode(t *testing.T, got, want int, stdout, stderr string) {
	t.Helper()
	if got != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
}
