package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

// defaultIcon is the tool-line icon (spec §5.3), except agent-browser's.
const (
	defaultIcon    = "▸"
	browserIcon    = "🌐"
	browserBinCmd  = "agent-browser "
	unknownRunes   = 60
	bashCmdRunes   = 60
	truncateSuffix = "…"
)

// imageDimsPattern matches the read tool's "image WxH (" output prefix.
const imageDimsPattern = `^image (\d+)x(\d+) \(`

// readLineNumberPattern matches one line of the read tool's "<n>: <line>"
// output.
const readLineNumberPattern = `(?m)^\d+: `

// bashExitPattern and bashTimeoutPattern match the trailing markers
// bash.go's formatBashResult appends to its output.
const (
	bashExitPattern    = `\[exit code (\d+)\]$`
	bashTimeoutPattern = `\[timed out after \d+s\]$`
)

// toolLine formats a Tool block's icon, name, and summary per spec §5.3.
// It applies no styling or state suffix; render adds those. b.Call is
// assumed non-nil (every Tool block has one).
func toolLine(b transcript.Block, dur time.Duration) (icon, name, summary string) {
	switch b.Call.Name {
	case "read":
		return readLine(b)
	case "write":
		return writeLine(b)
	case "edit":
		return editLine(b)
	case "bash":
		return bashLine(b, dur)
	case "glob":
		return searchLine("glob", b)
	case "grep":
		return searchLine("grep", b)
	case "todo":
		return todoLine(b)
	case "skill":
		return skillLine(b)
	default:
		return unknownLine(b)
	}
}

func readLine(b transcript.Block) (icon, name, summary string) {
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	if b.Result == nil || b.Result.IsError {
		return defaultIcon, "read", in.Path
	}
	out := b.Result.Output
	if m := regexp.MustCompile(imageDimsPattern).FindStringSubmatch(out); m != nil {
		return defaultIcon, "read", fmt.Sprintf("%s · image %sx%s", in.Path, m[1], m[2])
	}
	n := len(regexp.MustCompile(readLineNumberPattern).FindAllString(out, -1))
	return defaultIcon, "read", fmt.Sprintf("%s · %d lines", in.Path, n)
}

func writeLine(b transcript.Block) (icon, name, summary string) {
	var in struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	n := 0
	if in.Content != nil {
		n = lineCount(*in.Content)
	}
	return defaultIcon, "write", fmt.Sprintf("%s +%d", in.Path, n)
}

func editLine(b transcript.Block) (icon, name, summary string) {
	var in struct {
		Path      string  `json:"path"`
		OldString *string `json:"old_string"`
		NewString *string `json:"new_string"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	added, removed := 0, 0
	if in.NewString != nil {
		added = lineCount(*in.NewString)
	}
	if in.OldString != nil {
		removed = lineCount(*in.OldString)
	}
	return defaultIcon, "edit", fmt.Sprintf("%s +%d -%d", in.Path, added, removed)
}

// bashLine formats a bash call: agent-browser subcommands get the 🌐 icon
// and no name (browserSummary already reads as a full command), other
// commands show their truncated first line plus a finished-state suffix
// once they have a real result. A denied or cancelled call's Output is a
// sentinel ("user denied: …", "cancelled"), not the command's own output,
// so it never reaches bashStatus; render's generic state suffix (⊘) is
// the only indicator for those.
func bashLine(b transcript.Block, dur time.Duration) (icon, name, summary string) {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	if strings.HasPrefix(in.Command, browserBinCmd) {
		return browserIcon, "", browserSummary(in.Command)
	}
	first, _, _ := strings.Cut(in.Command, "\n")
	cmd := truncateRunes(first, bashCmdRunes)
	switch {
	case b.Result == nil, b.State == transcript.StateDenied, b.State == transcript.StateCancelled:
		return defaultIcon, "bash", cmd
	}
	return defaultIcon, "bash", cmd + " " + bashStatus(b.Result, dur)
}

// browserSummary strips agent-browser's global flags (each "--flag value"
// pair before the subcommand) and returns the subcommand and its
// remaining arguments, e.g. "--session s1 open x" -> "open x".
func browserSummary(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	rest := fields[1:]
	i := 0
	for i < len(rest) && strings.HasPrefix(rest[i], "-") {
		i += 2
	}
	if i > len(rest) {
		i = len(rest)
	}
	return strings.Join(rest[i:], " ")
}

// bashStatus renders bash's finished-state suffix: "✓ exit 0", "✗ exit N",
// or "✗ timed out", each with " · <dur>" appended unless dur is 0. An
// error result with neither marker (a bash launch failure, not an exit
// code) falls back to a bare "✗".
func bashStatus(r *core.ToolResult, dur time.Duration) string {
	out := r.Output
	timeoutRE, exitRE := regexp.MustCompile(bashTimeoutPattern), regexp.MustCompile(bashExitPattern)
	var status string
	switch {
	case timeoutRE.MatchString(out):
		status = "✗ timed out"
	case exitRE.MatchString(out):
		status = "✗ exit " + exitRE.FindStringSubmatch(out)[1]
	case r.IsError:
		status = "✗"
	default:
		status = "✓ exit 0"
	}
	if dur > 0 {
		status += fmt.Sprintf(" · %.1fs", dur.Seconds())
	}
	return status
}

func searchLine(name string, b transcript.Block) (icon, nm, summary string) {
	var in struct {
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	if b.Result == nil {
		return defaultIcon, name, fmt.Sprintf("%q", in.Pattern)
	}
	return defaultIcon, name, fmt.Sprintf("%q · %d matches", in.Pattern, matchCount(b.Result.Output))
}

// matchCount counts glob's/grep's one-match-per-line output, excluding
// the "no matches" placeholder and the truncation notice line.
func matchCount(out string) int {
	if out == "" || out == "no matches" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" || line == "[truncated at 100 results]" {
			continue
		}
		n++
	}
	return n
}

func todoLine(b transcript.Block) (icon, name, summary string) {
	var in struct {
		Todos []core.Todo `json:"todos"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	inProgress := 0
	for _, td := range in.Todos {
		if td.Status == "in_progress" {
			inProgress++
		}
	}
	return defaultIcon, "todo", fmt.Sprintf("%d items (%d in progress)", len(in.Todos), inProgress)
}

func skillLine(b transcript.Block) (icon, name, summary string) {
	var in struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	return defaultIcon, "skill", in.ID
}

func unknownLine(b transcript.Block) (icon, name, summary string) {
	return defaultIcon, b.Call.Name, truncateRunes(compactJSON(b.Call.Input), unknownRunes)
}

// compactJSON strips insignificant whitespace from raw, or returns it
// unchanged if it does not parse.
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// lineCount counts s's lines the way the write and edit tools count them: a
// single trailing newline does not add an extra line, and "" is 0 lines.
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// truncateRunes truncates s to at most n runes, appending truncateSuffix
// when it cuts.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + truncateSuffix
}
