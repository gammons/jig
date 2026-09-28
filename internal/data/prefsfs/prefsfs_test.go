package prefsfs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
