package app

import (
	"cmp"
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"os"

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
// the chat service the UIs drive, and the services the TUI's other ports
// wrap.
type runtime struct {
	bus      *event.Bus
	store    *store.Store
	chat     *chat.Service
	spillDir string // private (0700) per-process dir for bash spill files
	svc      tuiServices
	log      *slog.Logger // JIG_DEBUG log; discards when unset
	logFile  io.Closer    // closed last
}

// tuiServices are the runtime's services the TUI reaches through ports
// other than chat: sessions, agents, the catalog, blobs, and the frozen
// registry (its commands and keybinds).
type tuiServices struct {
	sessions *session.Service
	agents   *agents.Service
	catalog  catalogPort
	blobs    *blobfs.Store
	view     ext.View
}

// askerFunc builds the permission.Asker for a runtime from its bus.
type askerFunc func(*event.Bus) permission.Asker

// staticAsker is an askerFunc for a permission.StaticAsker (headless).
func staticAsker(allow bool) askerFunc {
	return func(*event.Bus) permission.Asker { return permission.StaticAsker{Allow: allow} }
}

// newRuntime builds every service for e. askerFor builds the asker that
// answers permission requests; discovery warnings go to errw. Errors
// caused by configuration are configErrors.
func newRuntime(ctx context.Context, e env, askerFor askerFunc, errw io.Writer) (*runtime, error) {
	clk := clock.Real()
	log, logFile := startDebugLog(e, errw)
	st, err := openStore(ctx, e)
	if err != nil {
		_ = logFile.Close()
		return nil, err
	}
	spillDir, err := os.MkdirTemp("", "jig-*")
	if err != nil {
		st.Close()
		_ = logFile.Close()
		return nil, err
	}
	cat := newCatalog(e, clk)
	rt := &runtime{bus: event.NewBus(), store: st, spillDir: spillDir, log: log, logFile: logFile}
	if rt.chat, err = newChat(e, rt, cat, askerFor(rt.bus), errw); err != nil {
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
	clk := clock.Real()
	hc := debugHTTPClient(e.getenv, rt.log, clk)
	pv, err := providerView(hc)
	if err != nil {
		return nil, err
	}
	idGen := ids.New(clk, rand.Reader)
	blobs := blobfs.New(e.blobsDir())
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
		debugDeps: debugDeps{log: rt.log, httpClient: hc},
	})
	if err != nil {
		return nil, err
	}
	rt.svc = tuiServices{sessions: sess, agents: ag, catalog: catalogPort{cat: cat, src: src}, blobs: blobs, view: view}
	runner := newRunner(rt, src, view, sess, clk, idGen)
	proxy.Set(runner)
	return chat.New(chat.Deps{
		Sessions: sess, Agents: ag, LLMs: src, Runner: runner, WorkDir: e.workDir,
		Files: tools.OSFS(), Reads: tracker, Images: pipeline,
	}), nil
}

// newRunner builds the agent Runner over rt's bus and store.
func newRunner(rt *runtime, src *llm.Source, view ext.View, sess *session.Service, clk clock.Clock, idGen *ids.Gen) *agent.Runner {
	return agent.NewRunner(agent.Deps{
		LLMs: src, Ext: view, Store: rt.store, History: sess, Bus: rt.bus,
		Clock: clk, IDs: idGen, ToolsFor: agents.ToolsFor, Log: rt.log,
	})
}

// closeChat closes rt's chat service, waiting at most closeTimeout for
// background work (session titles), even if ctx is already cancelled.
func (rt *runtime) closeChat(ctx context.Context) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	_ = rt.chat.Close(closeCtx)
	cancel()
}

// providerView is a frozen registry holding only the provider factories.
// The llm.Source must exist before the main registry is frozen (the
// session service needs it, and the task tool in that registry needs the
// session service), so the Source resolves factories through this view.
func providerView(hc *http.Client) (ext.View, error) {
	r := ext.NewRegistry()
	if err := addProviders(r, registryDeps{debugDeps: debugDeps{httpClient: hc}}); err != nil {
		return ext.View{}, err
	}
	return r.Freeze(), nil
}

// close releases rt's store and removes its spill dir, then closes the
// debug log.
func (rt *runtime) close() error {
	rmErr := os.RemoveAll(rt.spillDir)
	stErr := rt.store.Close()
	logErr := rt.logFile.Close()
	return cmp.Or(stErr, rmErr, logErr)
}
