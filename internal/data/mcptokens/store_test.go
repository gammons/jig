package mcptokens

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStore_RoundTripPerms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mcp-auth")
	s := New(dir)

	rec := Record{
		ServerURL:    "https://example.com/mcp",
		Issuer:       "https://example.com",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		Registration: "dynamic",
		RedirectPort: 5555,
		AuthURL:      "https://example.com/authorize",
		TokenURL:     "https://example.com/token",
		Scopes:       []string{"read", "write"},
	}
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want %o", perm, 0o700)
	}

	got, ok, err := s.Load(rec.ServerURL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load: ok = false, want true")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("Load() = %+v, want %+v", got, rec)
	}

	fi, err := os.Stat(s.path(rec.ServerURL))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want %o", perm, 0o600)
	}
}

func TestStore_URLMismatch(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	recA := Record{ServerURL: "https://a/mcp"}
	if err := s.Save(recA); err != nil {
		t.Fatalf("Save a: %v", err)
	}

	// Simulate a hash collision: write a's URL under b's hash-derived path.
	pathB := s.path("https://b/mcp")
	data, err := json.Marshal(recA)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(pathB, data, 0o600); err != nil {
		t.Fatalf("write collision file: %v", err)
	}

	_, ok, err := s.Load("https://b/mcp")
	if err != nil {
		t.Fatalf("Load b: %v", err)
	}
	if ok {
		t.Error("Load(b) ok = true, want false (ServerURL in file is a's, not b's)")
	}

	// b's own record still isn't found (no file for its own hash content).
	_, ok, err = s.Load("https://c/mcp")
	if err != nil {
		t.Fatalf("Load c: %v", err)
	}
	if ok {
		t.Error("Load(c) ok = true, want false (no file at all)")
	}
}

func TestStore_Delete(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	rec := Record{ServerURL: "https://d/mcp"}
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete(rec.ServerURL); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := s.Load(rec.ServerURL); err != nil || ok {
		t.Fatalf("Load after Delete: ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	// Deleting a record that never existed is still success.
	if err := s.Delete("https://never-existed/mcp"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}
