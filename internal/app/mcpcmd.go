package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/data/mcptokens"
)

// mcpCmd implements `jig mcp list|auth|logout`.
func mcpCmd(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprintln(std.Err, mcpUsage)
		return exitConfig
	}
	sub, rest, cwd, err := parseMCP(args, std.Err)
	if err != nil {
		return exitConfig
	}
	e, err := loadEnv(cwd, getenv, staticTrust(false))
	if err != nil {
		printLine(std.Err, err.Error())
		return exitConfig
	}
	switch sub {
	case "list":
		return mcpListCmd(ctx, e, std)
	case "auth":
		return mcpAuthCmd(ctx, rest, e, std)
	case "logout":
		return mcpLogoutCmd(rest, e, std)
	default:
		fmt.Fprintln(std.Err, mcpUsage)
		return exitConfig
	}
}

// parseMCP splits args into the subcommand, its remaining positional
// args, and a --cwd flag that may appear anywhere after the subcommand.
func parseMCP(args []string, errw io.Writer) (sub string, rest []string, cwd string, err error) {
	sub = args[0]
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { fmt.Fprintln(errw, mcpUsage) }
	fs.StringVar(&cwd, "cwd", "", "working directory (default: current directory)")
	words, ferr := parseInterspersed(fs, args[1:])
	if ferr != nil {
		return "", nil, "", ErrUsage
	}
	return sub, words, cwd, nil
}

const mcpUsage = `usage:
  jig mcp list
  jig mcp auth <name>
  jig mcp logout <name>`

// mcpListCmd starts every configured server, waits for them to settle,
// and prints a tab-aligned table of their state.
func mcpListCmd(ctx context.Context, e env, std Stdio) int {
	servers := resolvedMCPServers(e)
	if len(servers) == 0 {
		printLine(std.Out, "no MCP servers configured")
		return exitOK
	}
	bus := event.NewBus()
	mgr := newMCPManager(e, clock.Real(), bus, nil)
	mgr.Start(ctx)
	defer mgr.Close()
	mgr.Settle(ctx, maxStartupTimeout(servers))
	printMCPTable(std.Out, e.workDir, mgr.Servers())
	return exitOK
}

// printMCPTable writes one tab-aligned row per server, plus an indented
// error line for every non-ready one.
func printMCPTable(w io.Writer, workDir string, statuses []core.MCPServerStatus) {
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, sanitizeRow("name", "source", "transport", "state", "tools"))
	for _, s := range statuses {
		fmt.Fprintln(tw, mcpRow(workDir, s))
	}
	tw.Flush()
	for _, s := range statuses {
		if s.State != core.MCPReady && s.State != core.MCPDisabled && s.Err != "" {
			printLine(w, fmt.Sprintf("  %s: %s", s.Name, s.Err))
		}
	}
}

// mcpRow formats one sanitized, tab-separated row for s.
func mcpRow(workDir string, s core.MCPServerStatus) string {
	source := s.Source
	if rel, err := filepath.Rel(workDir, source); err == nil && !strings.HasPrefix(rel, "..") {
		source = rel
	}
	return sanitizeRow(s.Name, source, string(s.Transport), string(s.State), fmt.Sprintf("%d", s.Tools))
}

// sanitizeRow joins cols as a tab-separated row, sanitizing each cell.
func sanitizeRow(cols ...string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = ansi.SanitizeLine(c)
	}
	return strings.Join(out, "\t")
}

// mcpAuthCmd runs interactive sign-in for name: it prints the sign-in URL
// as soon as it is known, then blocks until Authenticate finishes.
func mcpAuthCmd(ctx context.Context, args []string, e env, std Stdio) int {
	if len(args) != 1 {
		fmt.Fprintln(std.Err, mcpUsage)
		return exitConfig
	}
	name := args[0]
	srv, ok := findMCPServer(e, name)
	if !ok {
		printLine(std.Err, fmt.Sprintf("mcp: unknown server %q", name))
		return exitConfig
	}
	if srv.Transport == core.MCPStdio {
		printLine(std.Err, fmt.Sprintf("mcp: %s is a stdio server; it has no sign-in", name))
		return exitConfig
	}
	bus := event.NewBus()
	mgr := newMCPManager(e, clock.Real(), bus, nil)
	mgr.Start(ctx)
	defer mgr.Close()
	return runMCPAuth(ctx, mgr, bus, name, std)
}

// runMCPAuth watches bus for name's AuthURL while Authenticate runs in the
// background, printing it once, then reports the outcome.
func runMCPAuth(ctx context.Context, mgr interface {
	Authenticate(context.Context, string) error
	Servers() []core.MCPServerStatus
}, bus *event.Bus, name string, std Stdio) int {
	sub := bus.Subscribe()
	defer sub.Close()

	done := make(chan error, 1)
	go func() { done <- mgr.Authenticate(ctx, name) }()

	printed := false
	for {
		select {
		case e := <-sub.C():
			if ch, ok := e.(event.MCPServerChanged); ok && ch.Name == name && !printed {
				if url := findAuthURL(mgr.Servers(), name); url != "" {
					fmt.Fprintln(std.Err, "opening your browser to sign in to "+ansi.SanitizeLine(name)+"; if it doesn't open, visit:\n  "+ansi.SanitizeLine(url))
					printed = true
				}
			}
		case err := <-done:
			return reportMCPAuth(mgr.Servers(), name, err, std)
		}
	}
}

// findAuthURL returns name's current AuthURL, or "".
func findAuthURL(statuses []core.MCPServerStatus, name string) string {
	for _, s := range statuses {
		if s.Name == name {
			return s.AuthURL
		}
	}
	return ""
}

// reportMCPAuth prints the outcome of an Authenticate call and returns
// the exit code.
func reportMCPAuth(statuses []core.MCPServerStatus, name string, err error, std Stdio) int {
	if err != nil {
		printLine(std.Err, fmt.Sprintf("%s: sign-in failed: %s", name, err.Error()))
		return exitRunFailed
	}
	tools := 0
	for _, s := range statuses {
		if s.Name == name {
			tools = s.Tools
		}
	}
	printLine(std.Out, fmt.Sprintf("%s: ready (%d tools)", name, tools))
	return exitOK
}

// mcpLogoutCmd deletes name's stored OAuth token directly, without
// building a Manager.
func mcpLogoutCmd(args []string, e env, std Stdio) int {
	if len(args) != 1 {
		fmt.Fprintln(std.Err, mcpUsage)
		return exitConfig
	}
	name := args[0]
	srv, ok := findMCPServer(e, name)
	if !ok {
		printLine(std.Err, fmt.Sprintf("mcp: unknown server %q", name))
		return exitConfig
	}
	if srv.Transport == core.MCPStdio {
		printLine(std.Err, fmt.Sprintf("mcp: %s is a stdio server; it has no sign-in", name))
		return exitConfig
	}
	tokens := mcptokens.New(e.mcpAuthDir())
	if err := tokens.Delete(srv.URL); err != nil {
		printLine(std.Err, "error: "+err.Error())
		return exitRunFailed
	}
	printLine(std.Out, name+": signed out")
	return exitOK
}

// findMCPServer returns e's configured server named name.
func findMCPServer(e env, name string) (core.MCPServer, bool) {
	for _, s := range resolvedMCPServers(e) {
		if s.Name == name {
			return s, true
		}
	}
	return core.MCPServer{}, false
}
