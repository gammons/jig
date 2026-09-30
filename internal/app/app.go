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
	"strings"

	"github.com/gammons/jig/internal/service/chat"
)

// Exit codes (spec: headless exit codes).
const (
	exitOK        = 0
	exitRunFailed = 1
	exitConfig    = 2
)

const usage = `usage:
  jig [--cwd DIR] [--session ID] [--trust-project]
  jig run [--agent A] [--model M] [--effort E] [--yes] [--trust-project] [--session ID] [--cwd DIR] <prompt...>
  jig models [provider]
  jig sessions
  jig version`

// Stdio is the process's standard streams.
type Stdio struct {
	In       io.Reader
	Out, Err io.Writer
}

// Run executes the jig command line args (without the program name) and
// returns the process exit code. No args, or leading flags, start the TUI
// (R25).
func Run(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	if len(args) == 0 || isTUIFlag(args[0]) {
		return runTUI(ctx, args, std, getenv)
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

// isTUIFlag reports whether arg is a flag (not -h/--help), so the command
// line is `jig [flags]`, the TUI.
func isTUIFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-h" && arg != "--help"
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
