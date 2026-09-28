package prefsfs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestPrefs_RoundTripAndCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	sidebar := true
	want := core.Prefs{
		Theme:   "dark",
		Sidebar: &sidebar,
		Recent:  []string{"a", "b"},
		History: map[string][]string{"/proj": {"first", "second"}},
		AgentBrowser: core.AgentBrowserCache{
			Bin:       "/usr/bin/agent-browser",
			ModTime:   1234,
			SkillsDir: "/skills",
		},
	}
	if err := s.Update(func(p *core.Prefs) { *p = want }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open (reopen): %v", err)
	}
	got := reopened.Get()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get() after reopen = %+v, want %+v", got, want)
	}

	// Mutating the slice returned by Get must not change the next Get.
	got2 := s.Get()
	got2.Recent[0] = "mutated"
	got2.History["/proj"][0] = "mutated"
	got3 := s.Get()
	if got3.Recent[0] != "a" {
		t.Errorf("Get() shares the Recent slice: got3.Recent[0] = %q, want %q", got3.Recent[0], "a")
	}
	if got3.History["/proj"][0] != "first" {
		t.Errorf("Get() shares a History slice: got3.History[/proj][0] = %q, want %q", got3.History["/proj"][0], "first")
	}
}

func TestPrefs_CorruptFileIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open: want error for corrupt JSON, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Open err = %v, want it to name %q", err, path)
	}
}

// TestPrefs_ConcurrentUpdatesLoseNothing drives N goroutines each
// appending a distinct entry through Update; every entry must survive, in
// memory and on disk. Run with -race.
func TestPrefs_ConcurrentUpdatesLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := s.Update(func(p *core.Prefs) { p.Recent = append(p.Recent, fmt.Sprint(i)) }); err != nil {
				t.Errorf("Update: %v", err)
			}
		}(i)
	}
	wg.Wait()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open (reopen): %v", err)
	}
	if got, onDisk := len(s.Get().Recent), len(reopened.Get().Recent); got != n || onDisk != n {
		t.Errorf("Recent has %d entries in memory, %d on disk; want %d", got, onDisk, n)
	}
}

// TestPrefs_UpdateMergesAcrossStores pins that Update applies fn to the
// file's current contents, not the Store's stale copy: two Stores (two
// jig processes) on the same path each Updating a different field both
// survive.
func TestPrefs_UpdateMergesAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Update(func(p *core.Prefs) { p.Theme = "dark" }); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(func(p *core.Prefs) { p.AgentBrowser.Bin = "/bin/ab" }); err != nil {
		t.Fatal(err)
	}
	if got := b.Get(); got.Theme != "dark" || got.AgentBrowser.Bin != "/bin/ab" {
		t.Errorf("b.Get() = %+v, want both updates", got)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get(); got.Theme != "dark" || got.AgentBrowser.Bin != "/bin/ab" {
		t.Errorf("on disk = %+v, want both updates", got)
	}
}

// TestPrefs_UpdateCorruptFileIsErrorNoWrite pins that Update refuses to
// overwrite a file it cannot parse.
func TestPrefs_UpdateCorruptFileIsErrorNoWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := s.Update(func(*core.Prefs) { called = true }); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Update err = %v, want an error naming %q", err, path)
	}
	if called {
		t.Error("fn was called for a corrupt file")
	}
	if data, _ := os.ReadFile(path); string(data) != "{not json" {
		t.Errorf("file = %q, want it untouched", data)
	}
}

func TestPrefs_MissingFileIsZeroValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := s.Get(); !reflect.DeepEqual(got, core.Prefs{}) {
		t.Errorf("Get() = %+v, want zero value", got)
	}
}

func TestStore_ImplementsPrefsService(t *testing.T) {
	var _ core.PrefsService = (*Store)(nil)
}
