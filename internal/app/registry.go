package app

import (
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
}

// buildRegistry registers every built-in tool, hook, transform, and
// provider, then freezes the registry.
func buildRegistry(d registryDeps) (ext.View, error) {
	r := ext.NewRegistry()
	steps := []func(*ext.Registry, registryDeps) error{addTools, addHooks, addTransforms, addProviders}
	for _, step := range steps {
		if err := step(r, d); err != nil {
			return ext.View{}, err
		}
	}
	return r.Freeze(), nil
}

func addTools(r *ext.Registry, d registryDeps) error {
	fsys, tr := tools.OSFS(), tools.NewTracker()
	srch := searchAdapter{search.New(exec.LookPath)}
	all := []ext.Tool{
		tools.NewRead(fsys, tr, d.media),
		tools.NewWrite(fsys, tr),
		tools.NewEdit(fsys, tr),
		tools.NewBash(shellAdapter{shell.Runner{}}, d.spillDir, d.ids),
		tools.NewGlob(srch),
		tools.NewGrep(srch),
		tools.NewTodo(d.store, d.bus),
		d.skills.Tool(),
		task.New(d.sessions, d.agents, d.proxy, d.bus),
	}
	for _, t := range all {
		if err := r.AddTool(t); err != nil {
			return err
		}
	}
	return nil
}

func addHooks(r *ext.Registry, d registryDeps) error {
	return r.AddToolHook(permission.NewHook(d.env.cfg().Permissions, d.asker))
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

func addProviders(r *ext.Registry, _ registryDeps) error {
	for _, p := range providerFactories() {
		if err := r.AddProvider(p); err != nil {
			return err
		}
	}
	return nil
}

// providerFactories is every provider factory jig registers.
func providerFactories() []ext.ProviderFactory {
	return append(llm.Factories(), extraProviders()...)
}

func isGit(dir string) bool {
	_, ok := fsroot.GitRoot(dir)
	return ok
}
