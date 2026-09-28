package transcript

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
)

// derived holds state computed from the root session's successful tool
// calls rather than shown as blocks: changed files (R16) and the last
// browser URL. Descendant sessions' calls are not observed: Load sees only
// the root's stored messages, and live and loaded state must agree.
type derived struct {
	changed []FileChange
	seen    map[string]bool // paths a successful read, write or edit named
	url     string
}

// Tool and command names derived state recognizes.
const (
	readTool     = "read"
	writeTool    = "write"
	editTool     = "edit"
	bashTool     = "bash"
	browserBin   = "agent-browser"
	shellQuoting = `"'`
)

// observe folds b's call and result into d. Failed calls change nothing.
func (d *derived) observe(b *Block) {
	if b.Kind != KindTool || b.Call == nil || b.Result == nil || b.Result.IsError {
		return
	}
	var in struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}
	if json.Unmarshal(b.Call.Input, &in) != nil {
		return
	}
	switch b.Call.Name {
	case readTool, writeTool, editTool:
		if in.Path != "" {
			d.touch(b.Call.Name, in.Path)
		}
	case bashTool:
		if u := browserURL(in.Command); u != "" {
			d.url = u
		}
	}
}

// touch records that tool succeeded on p. A write counts as added unless p
// was seen before; an edit is always modified (R16). Each path is listed
// once, with the kind it was first listed with, in first-seen order. Paths
// are kept as given (no resolution against the workdir).
func (d *derived) touch(tool, p string) {
	seen := d.seen[p]
	if d.seen == nil {
		d.seen = make(map[string]bool)
	}
	d.seen[p] = true
	if tool == readTool || slices.ContainsFunc(d.changed, func(c FileChange) bool { return c.Path == p }) {
		return
	}
	kind := ChangeModified
	if tool == writeTool && !seen {
		kind = ChangeAdded
	}
	d.changed = append(d.changed, FileChange{Path: p, Kind: kind})
}

// browserURL returns the target of the last agent-browser navigation
// (open, goto or navigate) in a bash command, walking every agent-browser
// invocation the command chains together (&&, ||, ;, |, &) in order, so
// the last navigation anywhere in the command wins.
func browserURL(command string) string {
	var url string
	fields := strings.Fields(command)
	for i, f := range fields {
		if path.Base(f) != browserBin {
			continue
		}
		seg := browserSegment(fields[i:])
		sub, args, ok := BrowserCommand(strings.Join(seg, " "))
		if ok && isNavigation(sub) && len(args) >= 1 {
			url = args[0]
		}
	}
	return url
}

// browserSegment returns the leading fields of one shell command up to
// (not including) the next control operator, splitting a trailing ";"
// off the last token it keeps.
func browserSegment(fields []string) []string {
	var out []string
	for _, f := range fields {
		if trimmed, ok := strings.CutSuffix(f, ";"); ok {
			if trimmed != "" {
				out = append(out, trimmed)
			}
			return out
		}
		switch f {
		case "&&", "||", ";", "|", "&":
			return out
		}
		out = append(out, f)
	}
	return out
}

// BrowserCommand parses cmd as one agent-browser invocation: cmd's first
// field's base name must be "agent-browser" (a bare name or a path to
// it, e.g. "/usr/bin/agent-browser"). It skips cmd's global flags before
// the subcommand — each flag browserValueFlag recognizes also consumes
// its following argument, unless the flag and its value are joined with
// "=" ("--session=s1"), which needs no extra token — and returns the
// subcommand and its remaining arguments, shell-quote characters
// trimmed. ok is false when cmd is not an agent-browser invocation, or
// there is no subcommand left after its flags.
func BrowserCommand(cmd string) (sub string, args []string, ok bool) {
	sub, args, _, ok = parseBrowser(cmd)
	return sub, args, ok
}

// defaultBrowserSession is agent-browser's session when no --session
// flag names one.
const defaultBrowserSession = "default"

// BrowserSession returns the agent-browser session the last agent-browser
// invocation in command (walked like the browser URL, across &&, ||, ;, |
// and &) runs in: its global --session flag's value ("--session s1" or
// "--session=s1", unquoted), else "default". ok is false when command
// holds no agent-browser invocation BrowserCommand accepts.
func BrowserSession(command string) (name string, ok bool) {
	fields := strings.Fields(command)
	for i, f := range fields {
		if path.Base(f) != browserBin {
			continue
		}
		if _, _, flags, found := parseBrowser(strings.Join(browserSegment(fields[i:]), " ")); found {
			name, ok = flagValue(flags, "--session"), true
			if name == "" {
				name = defaultBrowserSession
			}
		}
	}
	return name, ok
}

// flagValue returns the value of the last flag named name in flags
// ("--name value" or "--name=value"), unquoted, or "".
func flagValue(flags []string, name string) string {
	var v string
	for i := 0; i < len(flags); i++ {
		if val, found := strings.CutPrefix(flags[i], name+"="); found {
			v = unquoteShell(val)
		} else if flags[i] == name && i+1 < len(flags) {
			v = unquoteShell(flags[i+1])
			i++
		}
	}
	return v
}

// parseBrowser is BrowserCommand, also returning the global flag tokens
// (with their values) that precede the subcommand.
func parseBrowser(cmd string) (sub string, args, flags []string, ok bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 || path.Base(fields[0]) != browserBin {
		return "", nil, nil, false
	}
	rest := fields[1:]
	i := 0
	for i < len(rest) && strings.HasPrefix(rest[i], "-") {
		if browserValueFlag(rest[i]) {
			i += 2
			continue
		}
		i++
	}
	if i >= len(rest) {
		return "", nil, nil, false
	}
	flags = rest[:i]
	sub = unquoteShell(rest[i])
	for _, a := range rest[i+1:] {
		args = append(args, unquoteShell(a))
	}
	return sub, args, flags, true
}

// browserValueFlag reports whether f is one of agent-browser's global
// flags that takes a separate following argument. Every other flag
// (e.g. --headed, --json, --restore, --annotate) is boolean and consumes
// nothing; a flag joined to its value with "=" also consumes nothing
// extra, however it is spelled.
func browserValueFlag(f string) bool {
	if strings.Contains(f, "=") {
		return false
	}
	switch f {
	case "--session", "--profile", "--state", "--cdp", "--headers",
		"--executable-path", "--proxy", "--allowed-domains", "--user-agent",
		"--screenshot-dir", "--screenshot-format", "--screenshot-quality":
		return true
	}
	return false
}

// unquoteShell trims shell quote characters strings.Fields does not
// parse away.
func unquoteShell(s string) string {
	return strings.Trim(s, shellQuoting)
}

func isNavigation(sub string) bool {
	switch sub {
	case "open", "goto", "navigate":
		return true
	}
	return false
}
