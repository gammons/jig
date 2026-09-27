// Package catalog is a catwalk-backed catalog of LLM providers and models,
// cached to disk and refreshed at most once every 24h.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
)

// staleAfter is how long a cached catalog stays fresh before
// RefreshIfStale fetches again.
const staleAfter = 24 * time.Hour

// Fetcher fetches the current provider list from catwalk, given the ETag of
// the last-known cache contents (empty if there is none). It mirrors
// (*catwalk.Client).GetProviders, so production code passes
// catwalk.New().GetProviders.
type Fetcher func(ctx context.Context, etag string) ([]catwalk.Provider, error)

// Options configures a Catalog.
type Options struct {
	CachePath string
	Clock     clock.Clock
	Fetch     Fetcher
	Custom    map[string]core.ProviderConfig
}

// Catalog is a concurrency-safe, cached view of catwalk's provider/model
// catalog, merged with config-defined custom providers.
type Catalog struct {
	opts     Options
	snapshot atomic.Pointer[snapshot]
}

// snapshot is the immutable, queryable form of a Catalog at a point in
// time; RefreshIfStale swaps it out atomically.
type snapshot struct {
	providers []core.ProviderInfo // sorted by ID
	byID      map[string]core.ProviderInfo
}

// New returns a Catalog loaded from the cache file at o.CachePath if it
// exists and parses as a JSON array of catwalk.Provider, else from
// catwalk's embedded provider list. It does no network I/O; call
// RefreshIfStale for that.
func New(o Options) *Catalog {
	c := &Catalog{opts: o}
	providers, ok := loadCache(o.CachePath)
	if !ok {
		providers = embedded.GetAll()
	}
	c.swap(providers)
	return c
}

// loadCache reads and parses the cache file at path. ok is false if path is
// empty, the file does not exist or cannot be read, or it does not parse as
// a JSON array of catwalk.Provider.
func loadCache(path string) ([]catwalk.Provider, bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var providers []catwalk.Provider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, false
	}
	return providers, true
}

// swap builds a snapshot from providers merged with c.opts.Custom and
// installs it atomically.
func (c *Catalog) swap(providers []catwalk.Provider) {
	c.snapshot.Store(buildSnapshot(providers, c.opts.Custom))
}

func buildSnapshot(providers []catwalk.Provider, custom map[string]core.ProviderConfig) *snapshot {
	infos := mergeCustom(convertProviders(providers), custom)
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })

	byID := make(map[string]core.ProviderInfo, len(infos))
	for _, p := range infos {
		byID[p.ID] = p
	}
	return &snapshot{providers: infos, byID: byID}
}

// Providers returns every provider in the catalog (catwalk plus custom),
// sorted by ID.
func (c *Catalog) Providers() []core.ProviderInfo {
	return c.snapshot.Load().providers
}

// Provider looks up a provider by ID.
func (c *Catalog) Provider(id string) (core.ProviderInfo, bool) {
	p, ok := c.snapshot.Load().byID[id]
	return p, ok
}

// Model looks up a model by its provider/model ref.
func (c *Catalog) Model(ref core.ModelRef) (core.ModelInfo, bool) {
	p, ok := c.Provider(ref.Provider)
	if !ok {
		return core.ModelInfo{}, false
	}
	for _, m := range p.Models {
		if m.Ref == ref {
			return m, true
		}
	}
	return core.ModelInfo{}, false
}

// RefreshIfStale is a no-op unless the cache file is missing or its mtime
// is at least 24h old. When stale, it calls o.Fetch with the ETag of the
// current cache contents: on success it writes the new cache and swaps the
// snapshot in; on catwalk.ErrNotModified it just touches the cache's mtime;
// any other error leaves the current snapshot untouched and is returned.
func (c *Catalog) RefreshIfStale(ctx context.Context) error {
	now := c.opts.Clock.Now()
	if !c.isStale(now) {
		return nil
	}

	etag := ""
	if data, err := os.ReadFile(c.opts.CachePath); err == nil {
		etag = catwalk.Etag(data)
	}

	providers, err := c.opts.Fetch(ctx, etag)
	if err != nil {
		if errors.Is(err, catwalk.ErrNotModified) {
			return c.touch(now)
		}
		return err
	}

	if err := writeCacheAtomic(c.opts.CachePath, providers, now); err != nil {
		return err
	}
	c.swap(providers)
	return nil
}

// isStale reports whether the cache file is missing or at least staleAfter
// old, measured against now.
func (c *Catalog) isStale(now time.Time) bool {
	if c.opts.CachePath == "" {
		return true
	}
	info, err := os.Stat(c.opts.CachePath)
	if err != nil {
		return true
	}
	return now.Sub(info.ModTime()) >= staleAfter
}

// touch resets the cache file's mtime to now, marking it fresh without
// changing its contents.
func (c *Catalog) touch(now time.Time) error {
	if err := os.Chtimes(c.opts.CachePath, now, now); err != nil {
		return fmt.Errorf("catalog: touching cache mtime: %w", err)
	}
	return nil
}

// writeCacheAtomic JSON-encodes providers and writes them to path via a
// temp-file-plus-rename so readers never see a partial file, creating
// path's parent directory if needed. It then sets the file's mtime to now.
func writeCacheAtomic(path string, providers []catwalk.Provider, now time.Time) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("catalog: creating cache dir %s: %w", dir, err)
	}

	data, err := json.Marshal(providers)
	if err != nil {
		return fmt.Errorf("catalog: encoding cache: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return fmt.Errorf("catalog: creating temp cache file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("catalog: writing temp cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("catalog: closing temp cache file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("catalog: renaming temp cache file: %w", err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("catalog: setting cache mtime: %w", err)
	}
	return nil
}
