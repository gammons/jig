package mcpauth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOpenCmd(t *testing.T) {
	tests := []struct {
		goos string
		want string
		ok   bool
	}{
		{"linux", "xdg-open", true},
		{"darwin", "open", true},
		{"windows", "", false},
	}
	for _, tt := range tests {
		got, ok := openCmd(tt.goos)
		if got != tt.want || ok != tt.ok {
			t.Errorf("openCmd(%q) = %q, %v, want %q, %v", tt.goos, got, ok, tt.want, tt.ok)
		}
	}
}

// TestOpenBrowser_LauncherOutlivesCtx runs a fake opener that, like
// xdg-open starting a browser, keeps running for a moment. Cancelling ctx
// right after OpenBrowser returns (the sign-in ended) must not kill it.
func TestOpenBrowser_LauncherOutlivesCtx(t *testing.T) {
	name, ok := openCmd(runtime.GOOS)
	if !ok {
		t.Skip("no browser opener on this OS")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	script := "#!/bin/sh\nsleep 0.2\necho \"$1\" > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	if err := OpenBrowser(ctx, "https://example.test/authorize"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	cancel()

	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			b, err := os.ReadFile(marker)
			if err == nil && string(b) == "https://example.test/authorize\n" {
				return
			}
		case <-deadline:
			t.Fatal("opener never finished: it was killed or never started")
		}
	}
}
