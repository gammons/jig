package app

import (
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	goruntime "runtime"

	"github.com/gammons/jig/internal/client/llm"
	"github.com/gammons/jig/internal/client/search"
	"github.com/gammons/jig/internal/client/shell"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/data/blobfs"
	"github.com/gammons/jig/internal/data/contextfs"
	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agent"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/media"
	"github.com/gammons/jig/internal/service/permission"
	"github.com/gammons/jig/internal/service/prompt"
	"github.com/gammons/jig/internal/service/session"
	"github.com/gammons/jig/internal/service/skills"
	"github.com/gammons/jig/internal/service/task"
	"github.com/gammons/jig/internal/service/tools"
	"github.com/gammons/jig/internal/ui/actions"
)

// registryDeps are the collaborators the built-in extensions need.
type registryDeps struct {
	env      env
	clk      clock.Clock
	bus      *event.Bus
	store    *store.Store
	skills   *skills.Service
	sessions *session.Service
	agents   *agents.Service
	proxy    *agent.Proxy
	asker    permission.Asker
	ids      *ids.Gen
	spillDir string
	blobs    *blobfs.Store
	media    *media.Pipeline
	tracker  *tools.Tracker
	debugDeps
}

// debugDeps are the JIG_DEBUG collaborators, embedded in registryDeps to
// keep its field count down.
type debugDeps struct {
	log        *slog.Logger
	httpClient *http.Client // logs provider HTTP under JIG_DEBUG; nil otherwise
}

// buildRegistry registers every built-in tool, hook, transform, and
// provider, then freezes the registry.
func buildRegistry(d registryDeps) (ext.View, error) {
	r := ext.NewRegistry()
	steps := []func(*ext.Registry, registryDeps) error{
		addTools, addHooks, addTransforms, addProviders, addCommands, addKeybinds,
	}
	for _, step := range steps {
		if err := step(r, d); err != nil {
			return ext.View{}, err
		}
	}
	return r.Freeze(), nil
}

func addTools(r *ext.Registry, d registryDeps) error {
	fsys, tr := tools.OSFS(), d.tracker
	srch := searchAdapter{search.New(exec.LookPath)}
	var shots *tools.Screenshots
	if d.env.browser.enabled {
		shots = tools.NewScreenshots(d.media, fsys, os.TempDir())
	}
	all := []ext.Tool{
		tools.NewRead(fsys, tr, d.media),
		tools.NewWrite(fsys, tr),
		tools.NewEdit(fsys, tr),
		tools.NewBash(shellAdapter{shell.Runner{}}, d.spillDir, d.ids, shots),
		tools.NewGlob(srch),
		tools.NewGrep(srch),
		tools.NewTodo(d.store, d.bus),
		d.skills.Tool(),
		task.New(d.sessions, d.agents, d.proxy, d.bus, d.clk, d.log),
	}
	for _, t := range all {
		if err := r.AddTool(t); err != nil {
			return err
		}
	}
	return nil
}

func addHooks(r *ext.Registry, d registryDeps) error {
	var opts []permission.HookOption
	if d.env.browser.enabled {
		opts = append(opts, permission.WithPreset(permission.AgentBrowserPreset()))
	}
	return r.AddToolHook(permission.NewHook(d.env.cfg().Permissions, d.asker, opts...))
}

// addTransforms registers the system-prompt transforms. A literal
// instructions path that does not exist is a configError.
func addTransforms(r *ext.Registry, d registryDeps) error {
	e := d.env
	instr, err := contextfs.Instructions(e.cfg().Instructions, e.workDir, e.paths.Home)
	if err != nil {
		return configError{err}
	}
	all := []ext.ContextTransform{
		prompt.AgentPrompt(),
		prompt.Env(d.clk, goruntime.GOOS, isGit),
		prompt.Instructions(promptFiles(instr)),
		prompt.AgentsMD(promptFiles(contextfs.AgentsFiles(e.paths, e.gitRoot, e.workDir))),
		d.skills.Transform(),
	}
	for _, t := range all {
		if err := r.AddTransform(t); err != nil {
			return err
		}
	}
	return nil
}

// addCommands registers every ext.Command a built-in exposes as a picker
// action ("ext.<name>"). Empty for now; a future built-in appends to all.
func addCommands(r *ext.Registry, _ registryDeps) error {
	all := []ext.Command{}
	for _, c := range all {
		if err := r.AddCommand(c); err != nil {
			return err
		}
	}
	return nil
}

// addKeybinds registers jig's default keymap.
func addKeybinds(r *ext.Registry, _ registryDeps) error {
	for _, k := range actions.DefaultBindings() {
		if err := r.AddKeybind(k); err != nil {
			return err
		}
	}
	return nil
}

func addProviders(r *ext.Registry, d registryDeps) error {
	for _, p := range providerFactories(d.httpClient) {
		if err := r.AddProvider(p); err != nil {
			return err
		}
	}
	return nil
}

// providerFactories is every provider factory jig registers. A non-nil hc
// is the HTTP client the built-in factories use.
func providerFactories(hc *http.Client) []ext.ProviderFactory {
	var opts []llm.Option
	if hc != nil {
		opts = append(opts, llm.WithHTTPClient(hc))
	}
	return append(llm.Factories(opts...), extraProviders()...)
}

func isGit(dir string) bool {
	_, ok := fsroot.GitRoot(dir)
	return ok
}
