package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

// detailsMsg carries the details pane's content for one block, once a
// buildDetails Cmd's port call (a file read, a blob open, or a child
// session's messages) resolves.
type detailsMsg struct {
	Block   transcript.BlockID
	Content details.Content
	Image   *imgrender.Result
}

// readNumberedLinePattern matches one line of the read tool's "<n>: <line>"
// output, capturing the number and the code.
const readNumberedLinePattern = `^(\d+): (.*)$`

// buildDetails returns b's immediate details content, plus a Cmd for
// anything needing a port (file context for an edit, blob bytes for an
// image, a subagent's child messages), that resolves to a detailsMsg. A
// port error never panics: it becomes a readable, sanitized line in the
// content instead (or, for edit, falls back to the bare diff already
// shown).
func buildDetails(b transcript.Block, width, height int, r *renderer, p Ports, img *imgrender.Renderer) (details.Content, tea.Cmd) {
	switch b.Kind {
	case transcript.KindTool:
		return buildToolDetails(b, width, height, r, p, img)
	case transcript.KindSubagent:
		return buildSubagentDetails(b, p)
	case transcript.KindText, transcript.KindReasoning, transcript.KindUser:
		return buildTextDetails(b, r, width), nil
	default:
		return details.Content{}, nil
	}
}

// buildToolDetails dispatches a Tool block by its call name (spec §5.4).
func buildToolDetails(b transcript.Block, width, height int, r *renderer, p Ports, img *imgrender.Renderer) (details.Content, tea.Cmd) {
	if b.Call == nil {
		return details.Content{}, nil
	}
	switch b.Call.Name {
	case "edit":
		return buildEditDetails(b, r, p)
	case "write":
		return buildWriteDetails(b, r), nil
	case "read":
		return buildReadDetails(b, width, height, r, p, img)
	case "bash":
		return buildBashDetails(b, width, height, p, img)
	case "glob":
		return buildSearchDetails("glob", b), nil
	case "grep":
		return buildSearchDetails("grep", b), nil
	default:
		return details.Content{}, nil
	}
}

// editInput is the edit tool's call input.
type editInput struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// buildEditDetails shows a bare old_string→new_string diff immediately,
// then a Cmd rebuilds it with 3 lines of surrounding file context (R23)
// once Project.ReadFile resolves. An unreadable file keeps the bare diff.
func buildEditDetails(b transcript.Block, r *renderer, p Ports) (details.Content, tea.Cmd) {
	var in editInput
	_ = json.Unmarshal(b.Call.Input, &in)
	base := filepath.Base(in.Path)
	oldS, newS := ansi.Sanitize(in.OldString), ansi.Sanitize(in.NewString)

	lines, hunks := coderender.Diff(in.Path, oldS, newS, 0, r.set.Code)
	content := details.Content{Header: editHeader(base, hunks), Lines: lines}

	blockID, path := b.ID, in.Path
	cmd := readFileCmd(p, path, func(data []byte, err error) tea.Msg {
		if err != nil {
			return detailsMsg{Block: blockID, Content: content}
		}
		file := ansi.Sanitize(string(data))
		if strings.Count(file, newS) != 1 {
			return detailsMsg{Block: blockID, Content: content}
		}
		before := strings.Replace(file, newS, oldS, 1)
		ctxLines, ctxHunks := coderender.Diff(path, before, file, 3, r.set.Code)
		return detailsMsg{Block: blockID, Content: details.Content{
			Header: editHeader(base, ctxHunks), Lines: ctxLines,
		}}
	})
	return content, cmd
}

func editHeader(base string, hunks int) string {
	return fmt.Sprintf("edit · %s · %d hunks", base, hunks)
}

// buildWriteDetails shows the written content, syntax-highlighted.
func buildWriteDetails(b transcript.Block, r *renderer) details.Content {
	var in struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	content := ""
	if in.Content != nil {
		content = ansi.Sanitize(*in.Content)
	}
	header := fmt.Sprintf("write · %s · %d lines", filepath.Base(in.Path), lineCount(content))
	return details.Content{Header: header, Lines: coderender.Highlight(in.Path, content, r.set.Code)}
}

// buildReadDetails shows a text read's numbered, highlighted lines, or,
// for an image read, defers to buildImageDetails.
func buildReadDetails(b transcript.Block, width, height int, r *renderer, p Ports, img *imgrender.Renderer) (details.Content, tea.Cmd) {
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	base := filepath.Base(in.Path)

	if b.Result == nil {
		return details.Content{Header: "read · " + base}, nil
	}
	if !b.Result.IsError && len(b.Result.Media) > 0 {
		return buildImageDetails(b.ID, "read", base, b.Result.Media[0].Ref, width, height, p, img)
	}
	if b.Result.IsError {
		return details.Content{Header: "read · " + base, Lines: sanitizedLines(b.Result.Output)}, nil
	}
	return buildReadTextDetails(b.Result.Output, in.Path, base, r), nil
}

// buildReadTextDetails splits the read tool's "<n>: <line>" output back
// into line numbers and code, highlights the code for path, and prepends
// each line's number back in the gutter style.
func buildReadTextDetails(output, path, base string, r *renderer) details.Content {
	out := ansi.Sanitize(output)
	rows := strings.Split(out, "\n")
	nums := make([]string, len(rows))
	code := make([]string, len(rows))
	lineRE := regexp.MustCompile(readNumberedLinePattern)
	for i, row := range rows {
		if m := lineRE.FindStringSubmatch(row); m != nil {
			nums[i], code[i] = m[1], m[2]
			continue
		}
		code[i] = row
	}
	highlighted := coderender.Highlight(path, strings.Join(code, "\n"), r.set.Code)
	body := make([]string, len(highlighted))
	for i, h := range highlighted {
		if nums[i] == "" {
			body[i] = h
			continue
		}
		body[i] = r.set.Code.Gutter.Render(fmt.Sprintf("%4s ", nums[i])) + h
	}
	return details.Content{Header: fmt.Sprintf("read · %s · %d lines", base, len(rows)), Lines: body}
}

// spillPrefix is the marker bash.go's formatBashResult prepends when it
// spilled the full output to disk.
const spillPrefix = "[output truncated; full output: "

// buildBashDetails shows "$ <command>", a blank line, the sanitized
// output, and "exit N", naming the spill file when the output was
// truncated to it. An agent-browser screenshot result shows the image
// instead, like a read.
func buildBashDetails(b transcript.Block, width, height int, p Ports, img *imgrender.Renderer) (details.Content, tea.Cmd) {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	cmd := ansi.SanitizeLine(in.Command)
	header := "bash · " + truncateRunes(cmd, bashCmdRunes)

	if b.Result != nil && len(b.Result.Media) > 0 {
		return buildImageDetails(b.ID, "bash", "screenshot", b.Result.Media[0].Ref, width, height, p, img)
	}

	lines := []string{"$ " + cmd, ""}
	if b.Result == nil {
		return details.Content{Header: header, Lines: lines}, nil
	}
	out := ansi.Sanitize(b.Result.Output)
	spill := ""
	if rest, ok := strings.CutPrefix(out, spillPrefix); ok {
		if path, after, ok := strings.Cut(rest, "]"); ok {
			spill = path
			out = strings.TrimPrefix(after, "\n")
		}
	}
	lines = append(lines, strings.Split(out, "\n")...)
	lines = append(lines, "exit "+bashExitCode(b.Result))
	if spill != "" {
		lines = append(lines, "spill: "+ansi.SanitizeLine(spill))
	}
	return details.Content{Header: header, Lines: lines}, nil
}

// bashExitCode reads the exit code bash.go's formatBashResult embedded, or
// falls back to "?" for a result with no exit marker (a launch failure) or
// "0" for a successful call that (unusually) has none.
func bashExitCode(r *core.ToolResult) string {
	if m := regexp.MustCompile(bashExitPattern).FindStringSubmatch(r.Output); m != nil {
		return m[1]
	}
	if r.IsError {
		return "?"
	}
	return "0"
}

// buildSearchDetails shows a glob/grep call's result lines.
func buildSearchDetails(name string, b transcript.Block) details.Content {
	var in struct {
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal(b.Call.Input, &in)
	header := fmt.Sprintf("%s · %s", name, ansi.SanitizeLine(in.Pattern))
	var lines []string
	if b.Result != nil {
		if out := ansi.Sanitize(b.Result.Output); out != "" {
			lines = strings.Split(out, "\n")
		}
	}
	return details.Content{Header: header, Lines: lines}
}

// buildImageDetails shows a placeholder header immediately, then a Cmd
// opens ref through p.Blobs, decodes it, and renders it with img at
// width×(height-2), keyed by ref so re-renders of the same image reuse its
// protocol state (e.g. a kitty image id). A port or decode error becomes a
// sanitized line instead of an image.
func buildImageDetails(block transcript.BlockID, kind, subject, ref string, width, height int, p Ports, img *imgrender.Renderer) (details.Content, tea.Cmd) {
	header := fmt.Sprintf("%s · %s · image", kind, subject)
	content := details.Content{Header: header}

	cmd := openBlobCmd(p, ref, func(data []byte, _ string, err error) tea.Msg {
		if err != nil {
			return detailsMsg{Block: block, Content: errorContent(header, "could not open image", err)}
		}
		decoded, err := imgrender.Decode(data)
		if err != nil {
			return detailsMsg{Block: block, Content: errorContent(header, "could not decode image", err)}
		}
		res := img.Render(ref, decoded, width, height-2)
		return detailsMsg{Block: block, Content: details.Content{Header: header, Lines: res.Lines}, Image: &res}
	})
	return content, cmd
}

// buildSubagentDetails shows the running task's agent and description
// immediately, then a Cmd loads the child session's messages and renders
// its tool one-liners (toolLine) followed by its final text.
func buildSubagentDetails(b transcript.Block, p Ports) (details.Content, tea.Cmd) {
	sub := b.Sub
	if sub == nil {
		return details.Content{}, nil
	}
	header := fmt.Sprintf("subagent · %s · %s", ansi.SanitizeLine(sub.Agent), ansi.SanitizeLine(sub.Description))
	content := details.Content{Header: header}
	if sub.Child == "" {
		return content, nil
	}

	block, child := b.ID, sub.Child
	cmd := sessionMessagesCmd(p, child, func(msgs []core.Message, err error) tea.Msg {
		if err != nil {
			return detailsMsg{Block: block, Content: errorContent(header, "could not load subagent", err)}
		}
		return detailsMsg{Block: block, Content: details.Content{Header: header, Lines: subagentLines(msgs)}}
	})
	return content, cmd
}

// subagentLines renders a child session's tool calls as toolLine
// one-liners, in call order, followed by a blank line and its last
// assistant text part, if any.
func subagentLines(msgs []core.Message) []string {
	var order []string
	blocks := map[string]*transcript.Block{}
	var final string
	for _, m := range msgs {
		for _, part := range m.Parts {
			switch part.Kind {
			case core.PartToolCall:
				if part.Call == nil {
					continue
				}
				blocks[part.Call.ID] = &transcript.Block{Call: part.Call}
				order = append(order, part.Call.ID)
			case core.PartToolResult:
				if part.Result == nil {
					continue
				}
				if blk, ok := blocks[part.Result.CallID]; ok {
					res := *part.Result
					blk.Result = &res
				}
			case core.PartText:
				if part.Text != "" {
					final = part.Text
				}
			}
		}
	}

	lines := make([]string, 0, len(order)+2)
	for _, id := range order {
		lines = append(lines, ansi.SanitizeLine(oneLiner(*blocks[id])))
	}
	if final != "" {
		lines = append(lines, "", ansi.Sanitize(final))
	}
	return lines
}

// oneLiner renders b's toolLine (icon, name, summary) as one plain line,
// with no styling or state suffix (the subagent details pane shows a
// child's calls as a flat log, not a live blocklist).
func oneLiner(b transcript.Block) string {
	icon, name, summary, _ := toolLine(b, 0)
	line := icon + " "
	if name != "" {
		line += name + "  "
	}
	return line + summary
}

// buildTextDetails shows a Text, Reasoning, or User block's full text:
// markdown-rendered for Text, plain for the others.
func buildTextDetails(b transcript.Block, r *renderer, width int) details.Content {
	text := ansi.Sanitize(b.Text)
	switch b.Kind {
	case transcript.KindText:
		return details.Content{Header: "text", Lines: r.md.Render(text, width)}
	case transcript.KindReasoning:
		return details.Content{Header: "reasoning", Lines: strings.Split(text, "\n")}
	default: // KindUser
		return details.Content{Header: "user", Lines: strings.Split(text, "\n")}
	}
}

// sanitizedLines splits s into sanitized lines.
func sanitizedLines(s string) []string {
	return strings.Split(ansi.Sanitize(s), "\n")
}

// errorContent renders a port error as a single sanitized line, never the
// raw error's path or content (the error text itself may echo untrusted
// input, e.g. a blob ref).
func errorContent(header, prefix string, err error) details.Content {
	return details.Content{Header: header, Lines: []string{ansi.SanitizeLine(prefix + ": " + err.Error())}}
}
