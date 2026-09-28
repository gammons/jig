// Package trustfs persists per-project trust grants as a single JSON file
// ($XDG_DATA_HOME/jig/trust.json) mapping an absolute project path to the
// Grant that trusted it, and hashes a project's config files so callers can
// detect when a trusted config has changed since it was granted.
package trustfs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
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

// Get returns the Grant recorded for project. A missing file or a project
// with no grant both yield (zero value, false, nil); a malformed file is an
// error naming path.
func (s *Store) Get(project string) (Grant, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	grants, err := s.load()
	if err != nil {
		return Grant{}, false, err
	}
	g, ok := grants[project]
	return g, ok, nil
}

// Put records g for project, atomically rewriting the whole file (0600).
// It reads the current contents first, so a Put for one project never
// drops another project's grant.
func (s *Store) Put(project string, g Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	grants, err := s.load()
	if err != nil {
		return err
	}
	if grants == nil {
		grants = make(map[string]Grant)
	}
	grants[project] = g

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
func (s *Store) load() (map[string]Grant, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trustfs: reading %s: %w", s.path, err)
	}

	var grants map[string]Grant
	if err := json.Unmarshal(data, &grants); err != nil {
		return nil, fmt.Errorf("trustfs: parsing %s: %w", s.path, err)
	}
	return grants, nil
}

// Hash returns a sha256 hex digest over files, sorted by path, so the
// result doesn't depend on the caller's order. Each file contributes
// strconv.Itoa(len(path)) + "\n" + path + strconv.Itoa(len(content)) +
// "\n" + content. Both the path and the content are length-prefixed:
// project filenames are attacker-controlled (a repo can contain a
// filename with an embedded newline and digits), so prefixing only the
// content's length would let a crafted path forge the boundary between
// one file's record and the next, making two different file sets hash
// equal. Length-prefixing the path too fixes exactly how many bytes are
// path before the content-length digits appear, closing that gap.
// Hash(nil) is "", not the hash of an empty stream, so "no files" is
// distinguishable from "one empty file".
func Hash(files []string) (string, error) {
	if len(files) == 0 {
		return "", nil
	}

	sorted := append([]string(nil), files...)
	sort.Strings(sorted)

	h := sha256.New()
	for _, path := range sorted {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("trustfs: reading %s: %w", path, err)
		}
		fmt.Fprintf(h, "%s\n%s%s\n", strconv.Itoa(len(path)), path, strconv.Itoa(len(content)))
		h.Write(content)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
