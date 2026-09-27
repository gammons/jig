package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/gammons/jig/internal/clock"
)

func writeProvidersFile(t *testing.T, path string, providers []catwalk.Provider) {
	t.Helper()
	data, err := json.Marshal(providers)
	if err != nil {
		t.Fatalf("marshal providers: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func testProvider(id string) catwalk.Provider {
	return catwalk.Provider{
		Name: id,
		ID:   catwalk.InferenceProvider(id),
		Type: catwalk.TypeOpenAICompat,
		Models: []catwalk.Model{
			{ID: "model-1", Name: "Model One"},
		},
	}
}

func TestNew_FallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	c := New(Options{
		CachePath: filepath.Join(dir, "catalog.json"), // does not exist
		Clock:     clk,
	})

	if _, ok := c.Provider("anthropic"); !ok {
		t.Fatal("Provider(anthropic) not found, want embedded fallback to include it")
	}
}

func TestNew_UsesCacheWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	writeProvidersFile(t, path, []catwalk.Provider{testProvider("cached-only")})
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	c := New(Options{CachePath: path, Clock: clk})

	if _, ok := c.Provider("cached-only"); !ok {
		t.Fatal("Provider(cached-only) not found, want cache contents used")
	}
	if _, ok := c.Provider("anthropic"); ok {
		t.Fatal("Provider(anthropic) found, want cache to replace embedded, not merge with it")
	}
}

func TestNew_CorruptCacheFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	c := New(Options{CachePath: path, Clock: clk})

	if _, ok := c.Provider("anthropic"); !ok {
		t.Fatal("Provider(anthropic) not found, want embedded fallback on corrupt cache")
	}
}

func TestRefresh_SkipsWhenFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	writeProvidersFile(t, path, []catwalk.Provider{testProvider("cached-only")})

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	if err := os.Chtimes(path, start, start); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	calls := 0
	c := New(Options{
		CachePath: path,
		Clock:     clk,
		Fetch: func(ctx context.Context, etag string) ([]catwalk.Provider, error) {
			calls++
			return nil, nil
		},
	})

	clk.Advance(23 * time.Hour) // still fresh, < 24h since mtime
	if err := c.RefreshIfStale(context.Background()); err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}
	if calls != 0 {
		t.Errorf("Fetch called %d times, want 0 (cache still fresh)", calls)
	}
}

func TestRefresh_WritesCacheAndSwaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	writeProvidersFile(t, path, []catwalk.Provider{testProvider("stale-provider")})

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	if err := os.Chtimes(path, start, start); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	fresh := []catwalk.Provider{testProvider("fresh-provider")}
	c := New(Options{
		CachePath: path,
		Clock:     clk,
		Fetch: func(ctx context.Context, etag string) ([]catwalk.Provider, error) {
			return fresh, nil
		},
	})

	clk.Advance(25 * time.Hour) // stale
	if err := c.RefreshIfStale(context.Background()); err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}

	if _, ok := c.Provider("fresh-provider"); !ok {
		t.Fatal("Provider(fresh-provider) not found after refresh, want snapshot swapped")
	}
	if _, ok := c.Provider("stale-provider"); ok {
		t.Fatal("Provider(stale-provider) still found after refresh, want old snapshot replaced")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cache after refresh: %v", err)
	}
	var onDisk []catwalk.Provider
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal cache after refresh: %v", err)
	}
	if len(onDisk) != 1 || onDisk[0].ID != "fresh-provider" {
		t.Errorf("cache on disk = %+v, want [fresh-provider]", onDisk)
	}
}

func TestRefresh_FetchErrorKeepsSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	// No cache file: New falls back to embedded, so "anthropic" is present.

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)

	wantErr := errors.New("boom")
	c := New(Options{
		CachePath: path,
		Clock:     clk,
		Fetch: func(ctx context.Context, etag string) ([]catwalk.Provider, error) {
			return nil, wantErr
		},
	})

	err := c.RefreshIfStale(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("RefreshIfStale() error = %v, want %v", err, wantErr)
	}
	if _, ok := c.Provider("anthropic"); !ok {
		t.Fatal("Provider(anthropic) not found after failed refresh, want old snapshot kept")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cache file exists after failed refresh, want untouched (missing)")
	}
}

func TestRefresh_NotModifiedTouchesMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	writeProvidersFile(t, path, []catwalk.Provider{testProvider("cached-only")})

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	if err := os.Chtimes(path, start, start); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	c := New(Options{
		CachePath: path,
		Clock:     clk,
		Fetch: func(ctx context.Context, etag string) ([]catwalk.Provider, error) {
			return nil, catwalk.ErrNotModified
		},
	})

	clk.Advance(25 * time.Hour) // stale
	if err := c.RefreshIfStale(context.Background()); err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache after refresh: %v", err)
	}
	wantMtime := clk.Now()
	if !info.ModTime().Equal(wantMtime) {
		t.Errorf("cache mtime = %v, want %v", info.ModTime(), wantMtime)
	}

	if _, ok := c.Provider("cached-only"); !ok {
		t.Fatal("Provider(cached-only) not found, want snapshot unchanged on ErrNotModified")
	}
}
