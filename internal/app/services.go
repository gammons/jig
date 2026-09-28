package app

import (
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/client/catalog"
	"github.com/gammons/jig/internal/client/llm"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/data/blobfs"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agent"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/chat"
	"github.com/gammons/jig/internal/service/media"
	"github.com/gammons/jig/internal/service/permission"
	"github.com/gammons/jig/internal/service/session"
	"github.com/gammons/jig/internal/service/skills"
	"github.com/gammons/jig/internal/service/tools"
)

// runtime is a fully wired jig: the one event bus, the store backing it,
// and the chat service the UIs drive.
type runtime struct {
	bus      *event.Bus
	store    *store.Store
	chat     *chat.Service
	spillDir string // private (0700) per-process dir for bash spill files
}

// newRuntime builds every service for e. asker answers permission
// requests; discovery warnings go to errw. Errors caused by configuration
// are configErrors.
func newRuntime(ctx context.Context, e env, asker permission.Asker, errw io.Writer) (*runtime, error) {
	clk := clock.Real()
	st, err := openStore(ctx, e)
	if err != nil {
		return nil, err
	}
	spillDir, err := os.MkdirTemp("", "jig-*")
	if err != nil {
		st.Close()
		return nil, err
	}
	cat := newCatalog(e, clk)
	rt := &runtime{bus: event.NewBus(), store: st, spillDir: spillDir}
	if rt.chat, err = newChat(e, rt, cat, asker, errw); err != nil {
		_ = rt.close()
		return nil, err
	}
	startRefresh(ctx, cat)
	return rt, nil
}

// newChat builds the agents, session, registry, runner, and chat
// services over rt's bus and store.
func newChat(e env, rt *runtime, cat *catalog.Catalog, asker permission.Asker, errw io.Writer) (*chat.Service, error) {
	disc := discover(e, errw)
	ag, err := agents.New(e.cfg(), disc.sources)
	if err != nil {
		return nil, configError{err}
	}
	pv, err := providerView()
	if err != nil {
		return nil, err
	}
	clk := clock.Real()
	idGen := ids.New(clk, rand.Reader)
	blobs := blobfs.New(filepath.Join(e.paths.DataDir, "blobs"))
	src := llm.NewSource(cat, pv, e.cfg().Providers, e.getenv, blobs)
	sess := session.New(session.Deps{Store: rt.store, LLMs: src, Agents: ag, Bus: rt.bus, Clock: clk, IDs: idGen})
	proxy := &agent.Proxy{}
	tracker := tools.NewTracker()
	pipeline := media.New(blobs)
	view, err := buildRegistry(registryDeps{
		env: e, clk: clk, bus: rt.bus, store: rt.store,
		skills: skills.New(disc.skills, skillFS{}), sessions: sess, agents: ag,
		proxy: proxy, asker: asker, ids: idGen, spillDir: rt.spillDir,
		blobs: blobs, media: pipeline, tracker: tracker,
	})
	if err != nil {
		return nil, err
	}
	runner := agent.NewRunner(agent.Deps{
		LLMs: src, Ext: view, Store: rt.store, History: sess, Bus: rt.bus,
		Clock: clk, IDs: idGen, ToolsFor: agents.ToolsFor,
	})
	proxy.Set(runner)
	return chat.New(chat.Deps{
		Sessions: sess, Agents: ag, LLMs: src, Runner: runner, WorkDir: e.workDir,
		Files: tools.OSFS(), Reads: tracker, Images: pipeline,
	}), nil
}

// providerView is a frozen registry holding only the provider factories.
// The llm.Source must exist before the main registry is frozen (the
// session service needs it, and the task tool in that registry needs the
// session service), so the Source resolves factories through this view.
func providerView() (ext.View, error) {
	r := ext.NewRegistry()
	if err := addProviders(r, registryDeps{}); err != nil {
		return ext.View{}, err
	}
	return r.Freeze(), nil
}

// close releases rt's store and removes its spill dir.
func (rt *runtime) close() error {
	rmErr := os.RemoveAll(rt.spillDir)
	if err := rt.store.Close(); err != nil {
		return err
	}
	return rmErr
}
