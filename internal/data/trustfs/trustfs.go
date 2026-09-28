// Package trustfs persists per-project trust grants as a single JSON file
// ($XDG_DATA_HOME/jig/trust.json) mapping an absolute project path to the
// Grants (one per trusted config hash, at most maxHashes) that trusted
// it, and hashes a project's config files so callers can
// detect when a trusted config has changed since it was granted.
package trustfs

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gammons/jig/internal/data/atomicfile"
)

// Grant records that a project's config was trusted at a point in time,
// keyed to the hash of the files that produced it: a later Hash mismatch
// means the config changed since GrantedAt.
type Grant struct {
	Hash      string    `json:"hash"`
	GrantedAt time.Time `json:"grantedAt"`
}

// maxHashes bounds how many granted hashes a project keeps; the least
// recently granted is evicted first.
const maxHashes = 16

// projectGrants is one project's entry in trust.json: its granted hashes,
// most recently granted first. Different subdirectories of one git root
// (a monorepo) can have different project configs, hence a set.
type projectGrants struct {
	Hashes []Grant `json:"hashes"`
}

// UnmarshalJSON also accepts the original single-grant shape
// {"hash":…,"grantedAt":…} as a one-element set.
func (p *projectGrants) UnmarshalJSON(data []byte) error {
	var v struct {
		Hashes    []Grant   `json:"hashes"`
		Hash      string    `json:"hash"`
		GrantedAt time.Time `json:"grantedAt"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	p.Hashes = v.Hashes
	if p.Hashes == nil && v.Hash != "" {
		p.Hashes = []Grant{{Hash: v.Hash, GrantedAt: v.GrantedAt}}
	}
	return nil
}

// Store is a trust.json file at path, keyed by project path. Construct one
// with New; the zero value is not usable. Get and Put each read the whole
// file under s.mu, so callers see a consistent read-modify-write even
// without a cached copy in memory.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns a Store backed by the trust.json file at path. It does not
// touch the filesystem; a missing file behaves as if it were empty.
func New(path string) *Store {
	return &Store{path: path}
}

// Get reports whether hash is among project's granted hashes. A missing
// file or project yields (false, nil); a malformed file is an error
// naming path.
func (s *Store) Get(project, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	grants, err := s.load()
	if err != nil {
		return false, err
	}
	for _, g := range grants[project].Hashes {
		if g.Hash == hash {
			return true, nil
		}
	}
	return false, nil
}

// Put adds g to project's granted hashes (or moves an existing grant of
// g.Hash to the front), keeping at most maxHashes, and atomically rewrites
// the whole file (0600). It reads the current contents first, so a Put
// for one project never drops another project's grants.
func (s *Store) Put(project string, g Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	grants, err := s.load()
	if err != nil {
		return err
	}
	if grants == nil {
		grants = make(map[string]projectGrants)
	}
	hashes := []Grant{g}
	for _, old := range grants[project].Hashes {
		if old.Hash != g.Hash && len(hashes) < maxHashes {
			hashes = append(hashes, old)
		}
	}
	grants[project] = projectGrants{Hashes: hashes}

	data, err := json.Marshal(grants)
	if err != nil {
		return fmt.Errorf("trustfs: encoding %s: %w", s.path, err)
	}
	if err := atomicfile.Write(s.path, data, 0o600); err != nil {
		return fmt.Errorf("trustfs: saving %s: %w", s.path, err)
	}
	return nil
}

// load reads and parses s.path. A missing file yields (nil, nil).
func (s *Store) load() (map[string]projectGrants, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trustfs: reading %s: %w", s.path, err)
	}

	var grants map[string]projectGrants
	if err := json.Unmarshal(data, &grants); err != nil {
		return nil, fmt.Errorf("trustfs: parsing %s: %w", s.path, err)
	}
	return grants, nil
}
