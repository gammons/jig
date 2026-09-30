// Package mcptokens persists OAuth sign-in state for remote MCP servers:
// one 0600 JSON file per server URL, named by the URL's SHA-256 hash, in a
// 0700 directory. Secrets never leave this package except through Record.
package mcptokens

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"

	"github.com/gammons/jig/internal/data/atomicfile"
)

// Record is one server's OAuth sign-in state.
type Record struct {
	ServerURL    string `json:"server_url"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// Registration is "preregistered" or "dynamic".
	Registration string        `json:"registration"`
	RedirectPort int           `json:"redirect_port"`
	AuthURL      string        `json:"auth_url"`
	TokenURL     string        `json:"token_url"`
	Scopes       []string      `json:"scopes,omitempty"`
	Token        *oauth2.Token `json:"token,omitempty"`
}

// Store persists Records under dir, one file per server URL.
type Store struct {
	dir string
}

// New returns a Store rooted at dir. dir is created (0700) lazily, on the
// first Save.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// path returns the file a record for url is stored at: dir/<hex
// sha256(url)>.json.
func (s *Store) path(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

// Load reads the record for url. It returns false, with no error, when no
// file exists for url's hash, or when the file's ServerURL does not
// exactly match url (a defense against hash collisions or a corrupted
// store: the URL is always double-checked, never trusted from the
// filename alone).
func (s *Store) Load(url string) (Record, bool, error) {
	data, err := os.ReadFile(s.path(url))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("mcptokens: reading record for %s: %w", url, err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, false, fmt.Errorf("mcptokens: parsing record for %s: %w", url, err)
	}
	if rec.ServerURL != url {
		return Record{}, false, nil
	}
	return rec, true, nil
}

// Save writes r's record, creating the store directory (0700) if needed and
// writing the file atomically at 0600.
func (s *Store) Save(r Record) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("mcptokens: creating dir %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("mcptokens: encoding record for %s: %w", r.ServerURL, err)
	}
	if err := atomicfile.Write(s.path(r.ServerURL), data, 0o600); err != nil {
		return fmt.Errorf("mcptokens: writing record for %s: %w", r.ServerURL, err)
	}
	return nil
}

// Delete removes the record for url. A missing file is treated as success.
func (s *Store) Delete(url string) error {
	err := os.Remove(s.path(url))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("mcptokens: deleting record for %s: %w", url, err)
	}
	return nil
}
