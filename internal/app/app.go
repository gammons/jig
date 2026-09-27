// Package app is jig's composition root: it parses the command line,
// builds every layer from concrete implementations, and runs the
// requested subcommand. It is the only package that knows the concrete
// types behind every port.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/gammons/jig/internal/service/chat"
)

// Exit codes (spec: headless exit codes).
const (
	exitOK        = 0
	exitRunFailed = 1
	exitConfig    = 2
)

const noUIMessage = `the interactive UI is not built yet; use: jig run "prompt"`

const usage = `usage:
  jig run [--agent A] [--model M] [--yes] [--session ID] [--cwd DIR] <prompt...>
  jig models [provider]
  jig sessions
  jig version`

// Stdio is the process's standard streams.
type Stdio struct {
	In       io.Reader
	Out, Err io.Writer
}

// Run executes the jig command line args (without the program name) and
// returns the process exit code.
func Run(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprintln(std.Err, noUIMessage)
		return exitConfig
	}
	switch args[0] {
	case "run":
		return runCmd(ctx, args[1:], std, getenv)
	case "models":
		return modelsCmd(args[1:], std, getenv)
	case "sessions":
		return sessionsCmd(ctx, args[1:], std, getenv)
	case "version":
		fmt.Fprintln(std.Out, "jig", version())
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprintln(std.Out, usage)
		return exitOK
	default:
		fmt.Fprintf(std.Err, "jig: unknown command %q\n%s\n", args[0], usage)
		return exitConfig
	}
}

// version reports the main module's version from the build info, or
// "dev" for untagged local builds.
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// configError marks a failure caused by configuration or input, which
// exits with exitConfig.
type configError struct{ err error }

func (e configError) Error() string { return e.err.Error() }
func (e configError) Unwrap() error { return e.err }

// exitCode maps err to an exit code: configuration and input errors give
// exitConfig, anything else exitRunFailed.
func exitCode(err error) int {
	var ce configError
	var cce *chat.ConfigError
	if errors.As(err, &ce) || errors.As(err, &cce) {
		return exitConfig
	}
	return exitRunFailed
}
