package archtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowlist_EntriesHaveJustification(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}

	for key, justification := range allowlist {
		if strings.TrimSpace(justification) == "" {
			t.Errorf("allowlist[%q]: justification is empty", key)
		}
		_, path, ok := strings.Cut(key, ":")
		if !ok || path == "" {
			t.Errorf("allowlist key %q: want format \"rule:path\"", key)
			continue
		}
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("allowlist[%q]: file %q does not exist", key, path)
		}
	}
}
