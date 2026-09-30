package app

import (
	"cmp"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/client/search"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/data/themefs"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/permission"
	"github.com/gammons/jig/internal/ui"
	"github.com/gammons/jig/internal/ui/actions"
	"github.com/gammons/jig/internal/ui/theme"
)

// noTerminalMessage is printed when `jig` (the TUI) runs without a
// terminal on stdin or stdout (R25).
const noTerminalMessage = `jig: the interactive UI needs a terminal; use: jig run "prompt"`

// defaultTheme is the theme used when neither prefs nor config name one,
// or when the named one is unknown.
const defaultTheme = "dark"

// tuiDeps are runTUI's seams: the terminal checks and the program runner,
// so tests never start a real program.
type tuiDeps struct {
	outIsTerminal func(io.Writer) bool
	inIsTerminal  func(io.Reader) bool
	runProgram    func(ctx context.Context, m tea.Model, std Stdio) error
}

// runTUI implements `jig [--cwd DIR] [--session ID] [--trust-project]`.
func runTUI(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	return tuiDeps{outIsTerminal: isTerminalOut, inIsTerminal: isTerminalIn, runProgram: runProgram}.run(ctx, args, std, getenv)
}

// run parses args, checks that both std.Out and std.In are terminals (a
// pipe on either, e.g. `echo t | jig`, is not interactive), decides trust
// (the trust dialog, or granted by --trust-project), builds the runtime
// with a permission.BusAsker, and runs the TUI until it quits.
func (d tuiDeps) run(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	opts, err := parseTUI(args, std.Err)
	if err != nil {
		return exitConfig
	}
	if !d.outIsTerminal(std.Out) || !d.inIsTerminal(std.In) {
		fmt.Fprintln(std.Err, noTerminalMessage)
		return exitConfig
	}
	decide := trustDialog(ctx, std)
	if opts.trustProject {
		decide = staticTrust(true)
	}
	e, err := loadEnv(opts.cwd, getenv, decide)
	if err == nil {
		_, err = e.prefs.open()
	}
	if err != nil {
		printLine(std.Err, err.Error())
		return exitConfig
	}
	warnProviderOptions(std.Err, e.cfg().Providers)
	rt, perms, err := newTUIRuntime(ctx, e, std.Err)
	if err != nil {
		printLine(std.Err, "error: "+err.Error())
		return exitCode(err)
	}
	defer rt.close()
	return d.start(ctx, e, rt, perms, opts, std)
}

// start builds the App over rt, runs it, and closes the chat service.
func (d tuiDeps) start(ctx context.Context, e env, rt *runtime, perms *permission.BusAsker, opts tuiOpts, std Stdio) int {
	prefs, _ := e.prefs.open() // already opened (and checked) by run
	app := ui.New(tuiPorts(e, rt, perms, prefs), tuiOptions(e, rt, opts, prefs.Get().Theme, std.Err))
	err := d.runProgram(ctx, app, std)
	rt.closeChat(ctx)
	if err != nil {
		printLine(std.Err, "error: "+err.Error())
		return exitRunFailed
	}
	return exitOK
}

// newTUIRuntime builds a runtime whose permission asker is a
// permission.BusAsker on its bus, returned for the TUI's Perms port.
func newTUIRuntime(ctx context.Context, e env, errw io.Writer) (*runtime, *permission.BusAsker, error) {
	var perms *permission.BusAsker
	rt, err := newRuntime(ctx, e, func(bus *event.Bus) permission.Asker {
		perms = permission.NewBusAsker(bus, ids.New(clock.Real(), rand.Reader))
		return perms
	}, errw)
	return rt, perms, err
}

// tuiPorts builds the App's ports over rt.
func tuiPorts(e env, rt *runtime, perms *permission.BusAsker, prefs core.PrefsService) ui.Ports {
	return ui.Ports{
		Chat: rt.chat, Sessions: rt.svc.sessions, Perms: perms,
		Catalog: rt.svc.catalog, Agents: rt.svc.agents,
		Project: projectPort{
			workDir: e.workDir, spillDir: rt.spillDir, blobDir: e.blobsDir(),
			search: search.New(exec.LookPath),
		},
		Blobs:     blobPort{blobs: rt.svc.blobs},
		Prefs:     prefs,
		Editor:    editorPort{spillDir: rt.spillDir, getenv: e.getenv},
		Subscribe: rt.bus.Subscribe,
	}
}

// tuiOptions builds the App's options: the keymap (registered bindings +
// cfg [keybinds], warnings to errw), custom themes, and the theme choice.
func tuiOptions(e env, rt *runtime, opts tuiOpts, prefTheme string, errw io.Writer) ui.Options {
	cat := actions.NewCatalogue(rt.svc.view.Commands())
	km, warns := actions.Resolve(rt.svc.view.Keybinds(), e.cfg().Keybinds, cat)
	for _, w := range warns {
		printLine(errw, w)
	}
	custom := loadThemes(filepath.Join(e.paths.ConfigDir, "themes"), errw)
	return ui.Options{
		Session: core.SessionID(opts.session),
		WorkDir: e.workDir, ProjectKey: e.trust.project,
		Untrusted: tuiUntrusted(e.trust),
		Actions:   cat, Keymap: km,
		Themes: custom, Theme: chooseTheme(prefTheme, e.cfg().Theme, custom, errw),
		Images: imageEnv(e.getenv), Tmux: e.getenv("TMUX") != "",
		Aliases: e.cfg().ModelAliases, DefaultModel: defaultModel(rt.svc.agents),
		DefaultEffort: configuredEffort(e.cfg()),
	}
}

// configuredEffort is default_effort, parsed (loadEnv validated it).
func configuredEffort(cfg core.Config) core.Effort {
	e, _ := core.ParseEffort(cfg.DefaultEffort)
	return e
}

// defaultModel is the resolved default_model, or "" when none resolves.
func defaultModel(ag *agents.Service) string {
	ref, err := ag.ResolveModel(core.Agent{}, core.ModelRef{}, core.ModelRef{})
	if err != nil {
		return ""
	}
	return ref.String()
}

// tuiUntrusted reports whether the status bar shows "untrusted": the
// project has effects and was not trusted. A project with nothing to
// trust is not flagged.
func tuiUntrusted(st trustState) bool {
	return !st.trusted && len(st.effects) > 0
}

// loadThemes reads the custom themes in dir as completed palettes,
// printing every warning to errw.
func loadThemes(dir string, errw io.Writer) []theme.Palette {
	themes, warns, err := themefs.Load(dir)
	if err != nil {
		warns = append(warns, err.Error())
	}
	var out []theme.Palette
	for _, th := range themes {
		p, ws := theme.Custom(th.Name, th.Colors)
		warns = append(warns, ws...)
		out = append(out, theme.Complete(p))
	}
	for _, w := range warns {
		printLine(errw, "warning: "+w)
	}
	return out
}

// chooseTheme is prefTheme, else cfgTheme, else defaultTheme; a name
// that is neither custom nor built-in warns and falls back to
// defaultTheme.
func chooseTheme(prefTheme, cfgTheme string, custom []theme.Palette, errw io.Writer) string {
	name := cmp.Or(prefTheme, cfgTheme, defaultTheme)
	if _, ok := theme.Lookup(name, custom); !ok {
		printLine(errw, fmt.Sprintf("warning: unknown theme %q; using %s", name, defaultTheme))
		return defaultTheme
	}
	return name
}

// imageEnv captures the terminal environment image detection reads (R24).
func imageEnv(getenv func(string) string) imgrender.Env {
	return imgrender.Env{
		Term: getenv("TERM"), TermProgram: getenv("TERM_PROGRAM"),
		KittyWindowID: getenv("KITTY_WINDOW_ID"), TMUX: getenv("TMUX"),
		Override: getenv("JIG_IMAGES"),
	}
}

// isTerminalOut reports whether w is a terminal.
func isTerminalOut(w io.Writer) bool { return isTerminalFile(w) }

// isTerminalIn reports whether r is a terminal.
func isTerminalIn(r io.Reader) bool { return isTerminalFile(r) }

// isTerminalFile reports whether f is a terminal: an *os.File that is a
// character device and answers terminal ioctls (so /dev/null, a
// character device too, and a pipe, are not one).
func isTerminalFile(f any) bool {
	file, ok := f.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && term.IsTerminal(file.Fd())
}

// runProgram runs m as a full tea program on std until it quits or ctx
// is cancelled.
func runProgram(ctx context.Context, m tea.Model, std Stdio) error {
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(std.In), tea.WithOutput(std.Out)).Run()
	return err
}
