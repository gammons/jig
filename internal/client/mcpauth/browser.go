package mcpauth

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/gammons/jig/internal/client/shell"
)

// OpenBrowser opens url in the system browser: xdg-open on linux, open on
// darwin. It runs with no stdio attached, in its own process group (via
// shell.ConfigureGroup), and does not wait for the browser itself to exit
// (only for the launcher command, in the background) — a slow or
// long-lived browser process must never block the caller. Only the start
// error is reported; a failure to open isn't fatal because the URL is
// always shown too (this matters over SSH, where there is no browser to
// open).
func OpenBrowser(ctx context.Context, url string) error {
	name, ok := openCmd(runtime.GOOS)
	if !ok {
		return fmt.Errorf("mcpauth: no known browser opener for %s", runtime.GOOS)
	}

	cmd := exec.CommandContext(ctx, name, url)
	shell.ConfigureGroup(cmd)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcpauth: opening browser: %w", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// openCmd returns the command used to open a URL in a browser for the
// given GOOS, and whether one is known.
func openCmd(goos string) (string, bool) {
	switch goos {
	case "linux":
		return "xdg-open", true
	case "darwin":
		return "open", true
	default:
		return "", false
	}
}
