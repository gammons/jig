package mcpauth

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/gammons/jig/internal/client/shell"
)

// OpenBrowser opens url in the system browser: xdg-open on linux, open on
// darwin. It runs with no stdio attached, in its own session (via
// shell.ConfigureGroup), and does not wait for the browser itself to exit
// (only for the launcher command, in the background) — a slow or
// long-lived browser process must never block the caller. The launcher is
// deliberately not tied to ctx: xdg-open can stay running while it starts
// a browser that wasn't already open, and killing its group when the
// sign-in ends would take that browser down with it; ctx only stops a
// launch that hasn't started yet. Only the start error is reported; a
// failure to open isn't fatal because the URL is always shown too (this
// matters over SSH, where there is no browser to open).
func OpenBrowser(ctx context.Context, url string) error {
	name, ok := openCmd(runtime.GOOS)
	if !ok {
		return fmt.Errorf("mcpauth: no known browser opener for %s", runtime.GOOS)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// WithoutCancel: ConfigureGroup's Cancel hook needs CommandContext, but
	// the sign-in ending must never SIGKILL the launcher's group.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), name, url)
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
