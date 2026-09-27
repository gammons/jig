package app

import (
	"cmp"
	"context"
	"io"
	"path/filepath"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/agentfs"
	"github.com/gammons/jig/internal/data/skillfs"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/service/agents"

	"github.com/gammons/jig/internal/client/catalog"
)

// defaultCatwalkURL is the public catwalk service. catwalk.New() falls
// back to localhost, so jig names the public one unless CATWALK_URL is set.
const defaultCatwalkURL = "https://catwalk.charm.sh"

// openStore opens the SQLite store at <DataDir>/jig.db.
func openStore(ctx context.Context, e env) (*store.Store, error) {
	return store.Open(ctx, filepath.Join(e.paths.DataDir, "jig.db"))
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

// discover finds skills and markdown agents for e, writing any warnings
// to errw.
func discover(e env, errw io.Writer) discovered {
	skills, warns := skillfs.Discover(skillfs.Dirs(e.paths, e.gitRoot, e.workDir, e.cfg().SkillPaths))
	printWarnings(errw, warns)

	globalDirs, projectDirs := agentfs.Dirs(e.paths, e.gitRoot, e.workDir)
	globalMD, warns := agentfs.Discover(globalDirs)
	printWarnings(errw, warns)
	projectMD, warns := agentfs.Discover(projectDirs)
	printWarnings(errw, warns)

	return discovered{
		skills: skills,
		sources: agents.Sources{
			GlobalTOML:  e.loaded.GlobalAgents,
			GlobalMD:    globalMD,
			ProjectTOML: e.loaded.ProjectAgents,
			ProjectMD:   projectMD,
		},
	}
}
