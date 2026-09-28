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
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
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

// TestPrefs_ConcurrentSavesAgree drives N goroutines each Saving a distinct
// Prefs concurrently, then asserts that whichever one "won" is consistent
// between the in-memory Store and the bytes on disk: Get().Theme must equal
// the Theme a fresh Open of the same path sees. Run with -race: before
// Save held s.mu for its whole body, this could observe cur set by one
// goroutine's Save while the file on disk held another's bytes.
func TestPrefs_ConcurrentSavesAgree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")

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
			p := core.Prefs{Theme: fmt.Sprintf("theme-%d", i)}
			if err := s.Save(p); err != nil {
				t.Errorf("Save: %v", err)
			}
		}(i)
	}
	wg.Wait()

	got := s.Get()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open (reopen): %v", err)
	}
	onDisk := reopened.Get()

	if got.Theme != onDisk.Theme {
		t.Errorf("Get().Theme = %q, but the file on disk has Theme = %q", got.Theme, onDisk.Theme)
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
