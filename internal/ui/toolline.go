package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gammons/jig/internal/bubbles/ansi"
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
// It applies no styling or state suffix; render adds those, except that
// hasStatus tells render whether the summary already embeds its own
// finished-state indicator (only bash's "✓ exit 0"/"✗ exit N"/"✗ timed
// out", when it actually ran) so render does not add a redundant one, but
// also does not skip the generic one when bash never got that far (no
// result yet, or an agent-browser call, whose summary never embeds a
// status). b.Call is assumed non-nil (every Tool block has one).
func toolLine(b transcript.Block, dur time.Duration) (icon, name, summary string, hasStatus bool) {
	switch b.Call.Name {
	case "read":
		icon, name, summary = readLine(b)
	case "write":
		icon, name, summary = writeLine(b)
	case "edit":
		icon, name, summary = editLine(b)
	case "bash":
		icon, name, summary, hasStatus = bashLine(b, dur)
	case "glob":
		icon, name, summary = searchLine("glob", b)
	case "grep":
		icon, name, summary = searchLine("grep", b)
	case "todo":
		icon, name, summary = todoLine(b)
	case "skill":
		icon, name, summary = skillLine(b)
	default:
		icon, name, summary = unknownLine(b)
	}
	return
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
// and no name (browserSummary already reads as a full command; hasStatus
// is always false for these, since it never embeds a status itself),
// other commands show their truncated first line plus a finished-state
// suffix once they have a real result (hasStatus true only then). A
// denied or cancelled call's Output is a sentinel ("user denied: …",
// "cancelled"), not the command's own output, so it never reaches
// bashStatus; render's generic state suffix (⊘) is the only indicator
// for those, same as when there is no result yet.
func bashLine(b transcript.Block, dur time.Duration) (icon, name, summary string, hasStatus bool) {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	if strings.HasPrefix(in.Command, browserBinCmd) {
		return browserIcon, "", browserSummary(in.Command), false
	}
	first, _, _ := strings.Cut(in.Command, "\n")
	// Sanitize before truncating: escape-sequence bytes counted as runes
	// could otherwise consume the whole budget, leaving little or no
	// visible command once the summary is sanitized again downstream.
	cmd := truncateRunes(ansi.SanitizeLine(first), bashCmdRunes)
	switch {
	case b.Result == nil, b.State == transcript.StateDenied, b.State == transcript.StateCancelled:
		return defaultIcon, "bash", cmd, false
	}
	return defaultIcon, "bash", cmd + " " + bashStatus(b.Result, dur), true
}

// browserSummary formats an agent-browser bash command's subcommand and
// remaining arguments, its global flags removed by
// transcript.BrowserCommand, e.g. "--session s1 open x" -> "open x". A
// command BrowserCommand can't parse (should not happen: bashLine only
// calls this once the command starts with "agent-browser ") renders as
// "".
func browserSummary(command string) string {
	sub, args, ok := transcript.BrowserCommand(command)
	if !ok {
		return ""
	}
	return strings.Join(append([]string{sub}, args...), " ")
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
	if b.Result == nil || b.Result.IsError {
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
