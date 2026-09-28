package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui"
	"github.com/gammons/jig/internal/ui/theme"
)

const wantNoTerminal = "jig: the interactive UI needs a terminal; use: jig run \"prompt\"\n"

func TestRunTUI_NotATerminalExits2(t *testing.T) {
	for _, args := range [][]string{nil, {"--cwd", "."}, {"--trust-project"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, stderr := newTestEnv(t).run(t, args...)
			if code != exitConfig {
				t.Errorf("exit = %d, want %d", code, exitConfig)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if stderr != wantNoTerminal {
				t.Errorf("stderr = %q, want %q", stderr, wantNoTerminal)
			}
		})
	}
}

// /dev/null is a character device but not a terminal; a pipe is neither.
func TestIsTerminal_DevNullAndPipesAreNot(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	if isTerminalOut(f) {
		t.Error("isTerminalOut(/dev/null) = true")
	}
	if isTerminalOut(&bytes.Buffer{}) {
		t.Error("isTerminalOut(buffer) = true")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminalIn(r) {
		t.Error("isTerminalIn(pipe) = true")
	}
	if isTerminalIn(strings.NewReader("")) {
		t.Error("isTerminalIn(reader) = true")
	}
}

func TestParseTUI_Flags(t *testing.T) {
	var errw bytes.Buffer
	got, err := parseTUI([]string{"--cwd", "/w", "--session", "ses_1", "--trust-project"}, &errw)
	if err != nil {
		t.Fatalf("parseTUI: %v", err)
	}
	want := tuiOpts{cwd: "/w", session: "ses_1", trustProject: true}
	if got != want {
		t.Errorf("opts = %+v, want %+v", got, want)
	}

	if got, err := parseTUI(nil, &errw); err != nil || got != (tuiOpts{}) {
		t.Errorf("parseTUI(nil) = %+v, %v; want zero, nil", got, err)
	}

	for _, bad := range [][]string{{"--bogus"}, {"--cwd", "/w", "extra"}} {
		errw.Reset()
		if _, err := parseTUI(bad, &errw); !errors.Is(err, ErrUsage) {
			t.Errorf("parseTUI(%q) err = %v, want ErrUsage", bad, err)
		}
		if !strings.Contains(errw.String(), "usage: jig [--cwd DIR]") {
			t.Errorf("parseTUI(%q) stderr = %q, want usage", bad, errw.String())
		}
	}
}

// fakeTUI is a tuiDeps whose "terminal" check passes and whose program
// runner records the model instead of starting a program.
type fakeTUI struct {
	model tea.Model
	err   error
}

func (f *fakeTUI) deps() tuiDeps {
	return tuiDeps{
		outIsTerminal: func(io.Writer) bool { return true },
		inIsTerminal:  func(io.Reader) bool { return true },
		runProgram: func(_ context.Context, m tea.Model, _ Stdio) error {
			f.model = m
			return f.err
		},
	}
}

func (f *fakeTUI) run(t *testing.T, env *testEnv, args ...string) (int, string) {
	t.Helper()
	var out, errw bytes.Buffer
	std := Stdio{In: strings.NewReader(""), Out: &out, Err: &errw}
	code := f.deps().run(t.Context(), append([]string{"--cwd", env.workDir}, args...), std, env.getenv)
	return code, errw.String()
}

func TestRunTUI_KeybindWarningsPrinted(t *testing.T) {
	env := newTestEnv(t)
	env.writeConfig(t, "[keybinds]\n\"bogus.x\" = \"picker.open\"\n\"normal.z\" = \"no.such\"\n")
	f := &fakeTUI{}
	code, stderr := f.run(t, env)
	if code != exitOK {
		t.Fatalf("exit = %d, want 0; stderr %q", code, stderr)
	}
	for _, want := range []string{`unknown mode "bogus"`, `unknown action "no.such"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
	if _, ok := f.model.(*ui.App); !ok {
		t.Errorf("program model = %T, want *ui.App", f.model)
	}
}

// A non-terminal std.In (e.g. `echo t | jig`, piping an answer to the
// trust dialog) must exit 2 even when std.Out is a terminal.
func TestRunTUI_NonTerminalStdinExits2(t *testing.T) {
	env := newTestEnv(t)
	d := tuiDeps{
		outIsTerminal: func(io.Writer) bool { return true },
		inIsTerminal:  func(io.Reader) bool { return false },
		runProgram: func(context.Context, tea.Model, Stdio) error {
			t.Fatal("runProgram called with a non-terminal stdin")
			return nil
		},
	}
	var out, errw bytes.Buffer
	std := Stdio{In: strings.NewReader("t\n"), Out: &out, Err: &errw}
	code := d.run(t.Context(), []string{"--cwd", env.workDir}, std, env.getenv)
	if code != exitConfig {
		t.Errorf("exit = %d, want %d", code, exitConfig)
	}
	if errw.String() != wantNoTerminal {
		t.Errorf("stderr = %q, want %q", errw.String(), wantNoTerminal)
	}
}

func TestRunTUI_ProgramErrorExits1(t *testing.T) {
	env := newTestEnv(t)
	f := &fakeTUI{err: errors.New("tty gone")}
	code, stderr := f.run(t, env)
	if code != exitRunFailed {
		t.Errorf("exit = %d, want %d", code, exitRunFailed)
	}
	if !strings.Contains(stderr, "error: tty gone") {
		t.Errorf("stderr = %q, want the program error", stderr)
	}
}

func TestRunTUI_BadCwdExits2(t *testing.T) {
	env := newTestEnv(t)
	f := &fakeTUI{}
	var errw bytes.Buffer
	std := Stdio{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &errw}
	code := f.deps().run(t.Context(), []string{"--cwd", filepath.Join(env.workDir, "nope")}, std, env.getenv)
	if code != exitConfig || f.model != nil {
		t.Errorf("exit = %d, model %T; want %d and no program", code, f.model, exitConfig)
	}
}

// Untrusted project keybinds are dropped (trust.Restrict), so they never
// reach the keymap; trusted ones apply.
func TestTUIKeymap_UntrustedProjectKeybindsDropped(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		env := newTestEnv(t)
		env.writeProject(t, ".jig/config.toml", "[keybinds]\n\"normal.x\" = \"app.quit\"\n")
		e := mustLoadEnv(t, env, staticTrust(trusted))
		if got := e.cfg().Keybinds["normal.x"]; (got != "") != trusted {
			t.Errorf("trusted=%v: cfg keybind normal.x = %q", trusted, got)
		}
	}
}

func TestChooseTheme(t *testing.T) {
	custom := []theme.Palette{{Name: "mine"}}
	tests := []struct {
		name, pref, cfg, want, warn string
	}{
		{"default", "", "", "dark", ""},
		{"config", "", "mine", "mine", ""},
		{"prefs win", "Dark", "mine", "Dark", ""},
		{"unknown", "nope", "mine", "dark", `warning: unknown theme "nope"; using dark`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errw bytes.Buffer
			got := chooseTheme(tt.pref, tt.cfg, custom, &errw)
			if got != tt.want {
				t.Errorf("theme = %q, want %q", got, tt.want)
			}
			if strings.TrimSpace(errw.String()) != tt.warn {
				t.Errorf("stderr = %q, want %q", errw.String(), tt.warn)
			}
		})
	}
}

func TestLoadThemes_CompletesAndWarns(t *testing.T) {
	dir := t.TempDir()
	toml := "name = \"mine\"\n[colors]\nprimary = \"#ff0000\"\nbogus = \"#000000\"\n"
	if err := os.WriteFile(filepath.Join(dir, "mine.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	var errw bytes.Buffer
	got := loadThemes(dir, &errw)
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("themes = %+v, want one named mine", got)
	}
	if got[0].Background == "" || got[0].Primary != "#ff0000" {
		t.Errorf("palette not completed: %+v", got[0])
	}
	if !strings.Contains(errw.String(), `unknown color key "bogus"`) {
		t.Errorf("stderr = %q, want the unknown key warning", errw.String())
	}
}

func TestTUIPorts_PermsIsTheRuntimeAsker(t *testing.T) {
	env := newTestEnv(t)
	e := mustLoadEnv(t, env, staticTrust(false))
	rt, perms, err := newTUIRuntime(t.Context(), e, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.close()
	p := tuiPorts(e, rt, perms, nil)
	if !reflect.DeepEqual(p.Perms, core.PermissionService(perms)) || perms == nil {
		t.Errorf("Perms = %v, want the runtime's BusAsker", p.Perms)
	}
	if p.Chat == nil || p.Sessions == nil || p.Agents == nil || p.Catalog == nil ||
		p.Project == nil || p.Blobs == nil || p.Editor == nil || p.Subscribe == nil {
		t.Errorf("ports has a nil field: %+v", p)
	}
}
