package app

import (
	"cmp"
	"context"
	"io"
	"path/filepath"
	"sync"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/gammons/jig/internal/client/catalog"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/prefsfs"
	"github.com/gammons/jig/internal/data/skillfs"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/service/agents"
)

// defaultCatwalkURL is the public catwalk service (the same default crush
// uses). catwalk.New() falls back to http://localhost:8080, so jig names
// the public one explicitly unless CATWALK_URL is set.
const defaultCatwalkURL = "https://catwalk.charm.land"

// jigDBPath is the path of jig's own SQLite store (<DataDir>/jig.db).
func jigDBPath(e env) string {
	return filepath.Join(e.paths.DataDir, "jig.db")
}

// openStore opens the SQLite store at <DataDir>/jig.db.
func openStore(ctx context.Context, e env) (*store.Store, error) {
	return store.Open(ctx, jigDBPath(e))
}

// prefsPath is the path to jig's persisted preferences file
// ($XDG_STATE_HOME/jig/prefs.json).
func (e env) prefsPath() string {
	return filepath.Join(e.paths.StateDir, "prefs.json")
}

// blobsDir is the blob store's directory (<DataDir>/blobs).
func (e env) blobsDir() string {
	return filepath.Join(e.paths.DataDir, "blobs")
}

// mcpAuthDir is the MCP OAuth token store's directory (<DataDir>/mcp-auth),
// shared by newMCPManager and mcpLogoutCmd so both always agree on where
// tokens live.
func (e env) mcpAuthDir() string {
	return filepath.Join(e.paths.DataDir, "mcp-auth")
}

// prefsHandle opens the prefs store at path at most once, so every user in
// a process (agent-browser discovery in loadEnv, the TUI's Prefs port)
// shares one prefsfs.Store and its mutex.
type prefsHandle struct {
	path  string
	once  sync.Once
	store *prefsfs.Store
	err   error
}

// open returns the shared store, opening it on the first call.
func (h *prefsHandle) open() (*prefsfs.Store, error) {
	h.once.Do(func() { h.store, h.err = prefsfs.Open(h.path) })
	return h.store, h.err
}

// newCatalog loads the provider catalog from <CacheDir>/catalog.json (or
// catwalk's embedded list), merged with the config's custom providers. It
// does no network I/O.
func newCatalog(e env, clk clock.Clock) *catalog.Catalog {
	client := catwalk.NewWithURL(cmp.Or(e.getenv("CATWALK_URL"), defaultCatwalkURL))
	return catalog.New(catalog.Options{
		CachePath: filepath.Join(e.paths.CacheDir, "catalog.json"),
		Clock:     clk,
		Fetch:     client.GetProviders,
		Custom:    e.cfg().Providers,
	})
}

// startRefresh refreshes cat in the background if its cache is stale. It
// is bound to ctx and never awaited: a failed or unfinished refresh just
// leaves the current snapshot in place.
func startRefresh(ctx context.Context, cat *catalog.Catalog) {
	go func() { _ = cat.RefreshIfStale(ctx) }()
}

// discovered is what skill and markdown-agent discovery found on disk.
type discovered struct {
	skills  []core.Skill
	sources agents.Sources
}

// discover finds skills for e (with agent-browser's skills dir, if any,
// prepended as the lowest-precedence directory) and assembles the agent
// sources from e's post-trust layers, writing skill, markdown agent, and
// agent-browser warnings to errw.
func discover(e env, errw io.Writer) discovered {
	dirs := skillfs.Dirs(e.paths, e.gitRoot, e.workDir, e.cfg().SkillPaths)
	if e.browser.skillsDir != "" {
		dirs = append([]string{e.browser.skillsDir}, dirs...)
	}
	skills, warns := skillfs.Discover(dirs)
	printWarnings(errw, warns)
	printWarnings(errw, e.agentWarns)
	for _, w := range e.mcpWarns {
		printLine(errw, "warning: "+w)
	}
	for _, w := range e.browserWarns {
		printLine(errw, w)
	}

	return discovered{
		skills: skills,
		sources: agents.Sources{
			GlobalTOML:  e.layers.Global.Agents,
			GlobalMD:    e.layers.GlobalMD,
			ProjectTOML: e.layers.Project.Agents,
			ProjectMD:   e.layers.ProjectMD,
		},
	}
}
