package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/store"
)

// testEnv is an isolated set of XDG dirs, a work dir, and an environment
// map, so tests never touch the real $HOME.
type testEnv struct {
	vars    map[string]string
	workDir string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	e := &testEnv{
		vars: map[string]string{
			"HOME":            filepath.Join(root, "home"),
			"XDG_CONFIG_HOME": filepath.Join(root, "config"),
			"XDG_DATA_HOME":   filepath.Join(root, "data"),
			"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
			// Unroutable, so a background catalog refresh fails fast.
			"CATWALK_URL": "http://127.0.0.1:1",
		},
		workDir: filepath.Join(root, "work"),
	}
	for _, d := range []string{e.vars["HOME"], e.workDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *testEnv) getenv(k string) string { return e.vars[k] }

func (e *testEnv) writeConfig(t *testing.T, toml string) {
	t.Helper()
	dir := filepath.Join(e.vars["XDG_CONFIG_HOME"], "jig")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	code := Run(t.Context(), args, Stdio{In: strings.NewReader(""), Out: &out, Err: &errw}, e.getenv)
	return code, out.String(), errw.String()
}

func TestRun_NoArgsExits2(t *testing.T) {
	code, out, stderr := newTestEnv(t).run(t)
	if code != exitConfig {
		t.Errorf("exit = %d, want %d", code, exitConfig)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	want := "the interactive UI is not built yet; use: jig run \"prompt\"\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestRun_UnknownSubcommandExits2(t *testing.T) {
	code, _, stderr := newTestEnv(t).run(t, "frobnicate")
	if code != exitConfig {
		t.Errorf("exit = %d, want %d", code, exitConfig)
	}
	if !strings.Contains(stderr, `unknown command "frobnicate"`) {
		t.Errorf("stderr = %q, want unknown command", stderr)
	}
}

func TestRun_Version(t *testing.T) {
	code, out, _ := newTestEnv(t).run(t, "version")
	if code != exitOK || !strings.HasPrefix(out, "jig ") {
		t.Errorf("version: exit %d, stdout %q", code, out)
	}
}

func TestRun_InvalidModelConfigExits2(t *testing.T) {
	for _, key := range []string{"default_model", "small_model"} {
		t.Run(key, func(t *testing.T) {
			env := newTestEnv(t)
			env.writeConfig(t, key+` = "no-slash"`+"\n")
			code, _, stderr := env.run(t, "run", "--cwd", env.workDir, "hi")
			if code != exitConfig {
				t.Errorf("exit = %d, want %d", code, exitConfig)
			}
			if !strings.HasPrefix(stderr, "config: "+key+": ") {
				t.Errorf("stderr = %q, want config: %s: prefix", stderr, key)
			}
		})
	}
}

func TestRun_ConfigSyntaxErrorExits2(t *testing.T) {
	env := newTestEnv(t)
	env.writeConfig(t, "default_model = \n")
	code, _, stderr := env.run(t, "run", "--cwd", env.workDir, "hi")
	if code != exitConfig || !strings.Contains(stderr, "config.toml") {
		t.Errorf("exit %d, stderr %q; want 2 naming config.toml", code, stderr)
	}
}

func TestRun_BadAgentConfigExits2(t *testing.T) {
	env := newTestEnv(t)
	env.writeConfig(t, "[agents.build]\nmode = \"sideways\"\n")
	code, _, stderr := env.run(t, "run", "--cwd", env.workDir, "hi")
	if code != exitConfig || stderr == "" {
		t.Errorf("exit %d, stderr %q; want 2 with an error", code, stderr)
	}
}

func TestRun_MissingCredentialsExits2(t *testing.T) {
	env := newTestEnv(t)
	env.writeConfig(t, `default_model = "anthropic/claude-haiku-4-5-20251001"`+"\n")
	code, out, stderr := env.run(t, "run", "--cwd", env.workDir, "hi")
	if code != exitConfig {
		t.Errorf("exit = %d, want %d (stderr %q)", code, exitConfig, stderr)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, "ANTHROPIC_API_KEY") {
		t.Errorf("stderr = %q, want error naming ANTHROPIC_API_KEY", stderr)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr has %d lines, want 1: %q", strings.Count(stderr, "\n"), stderr)
	}
}

func TestRun_RelativeCwdResolved(t *testing.T) {
	env := newTestEnv(t)
	t.Chdir(filepath.Dir(env.workDir))
	e, err := loadEnv("work", env.getenv)
	if err != nil {
		t.Fatalf("loadEnv: %v", err)
	}
	if e.workDir != env.workDir {
		t.Errorf("workDir = %q, want %q", e.workDir, env.workDir)
	}
}

func TestModels_ShowsCredentialStatus(t *testing.T) {
	env := newTestEnv(t)
	env.writeConfig(t, `
[providers.local]
type = "openai-compat"
base_url = "http://localhost:1234/v1"
models = ["qwen-coder"]
`)
	code, out, stderr := env.run(t, "models")
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"local (configured)\n",
		"local/qwen-coder  ctx 0k  $0.00/$0.00 per 1M\n",
		"anthropic (no credentials)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("models output missing %q:\n%s", want, out)
		}
	}

	env.vars["ANTHROPIC_API_KEY"] = "sk-test"
	code, out, _ = env.run(t, "models", "anthropic")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.HasPrefix(out, "anthropic (configured)\n") {
		t.Errorf("filtered output = %q, want only anthropic, configured", out)
	}
	if strings.Contains(out, "local") {
		t.Errorf("filtered output lists other providers:\n%s", out)
	}
	if !strings.Contains(out, "ctx 200k  $") || !strings.Contains(out, " per 1M\n") {
		t.Errorf("anthropic models lack ctx/price columns:\n%s", out)
	}
}

func TestModels_UnknownProviderExits2(t *testing.T) {
	code, _, stderr := newTestEnv(t).run(t, "models", "nope")
	if code != exitConfig || !strings.Contains(stderr, `"nope"`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestSessions_EmptyDBPrintsNothing(t *testing.T) {
	code, out, stderr := newTestEnv(t).run(t, "sessions")
	if code != exitOK {
		t.Errorf("exit = %d, stderr %q", code, stderr)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}

func TestSessions_ListsRootSessions(t *testing.T) {
	env := newTestEnv(t)
	st, err := store.Open(t.Context(), filepath.Join(env.vars["XDG_DATA_HOME"], "jig", "jig.db"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	sess := core.Session{ID: "ses_a", Title: "Fix bug", Agent: "build", CreatedAt: at, UpdatedAt: at}
	if err := st.CreateSession(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	st.Close()

	code, out, stderr := env.run(t, "sessions")
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if want := "ses_a  " + at.Local().Format(time.RFC3339) + "  Fix bug\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}
