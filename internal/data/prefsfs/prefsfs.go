// Package prefsfs persists core.Prefs as a single JSON file
// ($XDG_STATE_HOME/jig/prefs.json), keeping an in-memory copy so Get never
// touches the filesystem.
package prefsfs

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/atomicfile"
)

// Store is a core.PrefsService backed by a JSON file at path. Construct one
// with Open; the zero value is not usable.
type Store struct {
	path string
	mu   sync.Mutex
	cur  core.Prefs
}

// Open reads path into a Store. A missing file yields a Store holding the
// zero core.Prefs; corrupt JSON is an error naming path.
func Open(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prefsfs: reading %s: %w", path, err)
	}

	var p core.Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("prefsfs: parsing %s: %w", path, err)
	}
	return &Store{path: path, cur: p}, nil
}

// Get returns a deep copy of the current Prefs: mutating the result,
// including its Sidebar pointer, Recent slice, or History map, never
// affects the Store.
func (s *Store) Get() core.Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clonePrefs(s.cur)
}

// Save writes p to disk (atomicfile.Write, 0600) and, on success, makes it
// the Prefs a later Get returns.
func (s *Store) Save(p core.Prefs) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("prefsfs: encoding prefs: %w", err)
	}
	if err := atomicfile.Write(s.path, data, 0o600); err != nil {
		return fmt.Errorf("prefsfs: saving %s: %w", s.path, err)
	}

	s.mu.Lock()
	s.cur = clonePrefs(p)
	s.mu.Unlock()
	return nil
}

// clonePrefs returns a deep copy of p: its Sidebar pointer, Recent slice,
// and History map (and each of its value slices) are copied rather than
// shared with p.
func clonePrefs(p core.Prefs) core.Prefs {
	out := p

	if p.Sidebar != nil {
		v := *p.Sidebar
		out.Sidebar = &v
	}
	if p.Recent != nil {
		out.Recent = append([]string(nil), p.Recent...)
	}
	if p.History != nil {
		out.History = make(map[string][]string, len(p.History))
		for k, v := range p.History {
			out.History[k] = append([]string(nil), v...)
		}
	}
	return out
}
