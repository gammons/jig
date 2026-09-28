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
// (open, goto or navigate) in a bash command, or "".
func browserURL(command string) string {
	var url string
	fields := strings.Fields(command)
	for i, f := range fields {
		if path.Base(f) != browserBin {
			continue
		}
		args := operands(fields[i+1:], 2)
		if len(args) == 2 && isNavigation(args[0]) {
			url = args[1]
		}
	}
	return url
}

// operands returns up to n leading non-flag arguments of one shell
// command, unquoted, stopping at a control operator.
func operands(fields []string, n int) []string {
	var out []string
	for _, f := range fields {
		if len(out) == n {
			break
		}
		end := false
		switch f {
		case "&&", "||", ";", "|", "&":
			return out
		}
		if trimmed, ok := strings.CutSuffix(f, ";"); ok {
			f, end = trimmed, true
		}
		if f != "" && !strings.HasPrefix(f, "-") {
			out = append(out, strings.Trim(f, shellQuoting))
		}
		if end {
			break
		}
	}
	return out
}

func isNavigation(sub string) bool {
	switch sub {
	case "open", "goto", "navigate":
		return true
	}
	return false
}
