package ui

import (
	"encoding/json"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/ui/transcript"
)

// yankText returns the text "y" copies b to the clipboard (spec §6.3):
// Text/User/Reasoning yank their text; a tool block yanks something
// specific to its call (bash's command, edit's diff, read/write's path,
// or another tool's output); a subagent block yanks its description. Every
// value comes from the block's own (untrusted) text, sanitized so it can
// never write a control sequence into the clipboard, but otherwise kept
// raw for fidelity.
func yankText(b transcript.Block) string {
	switch b.Kind {
	case transcript.KindText, transcript.KindUser, transcript.KindReasoning:
		return ansi.Sanitize(b.Text)
	case transcript.KindSubagent:
		if b.Sub == nil {
			return ""
		}
		return ansi.SanitizeLine(b.Sub.Description)
	case transcript.KindTool:
		return yankTool(b)
	default:
		return ""
	}
}

// yankTool returns the yanked text for a Tool block, by call name.
func yankTool(b transcript.Block) string {
	if b.Call == nil {
		return ""
	}
	switch b.Call.Name {
	case "bash":
		var in struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(b.Call.Input, &in)
		return ansi.Sanitize(in.Command) // a multi-line command keeps its lines
	case "edit":
		var in editInput
		_ = json.Unmarshal(b.Call.Input, &in)
		oldS, newS := ansi.Sanitize(in.OldString), ansi.Sanitize(in.NewString)
		return coderender.DiffText(oldS, newS, 3)
	case "read", "write":
		var in struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(b.Call.Input, &in)
		return ansi.SanitizeLine(in.Path)
	default:
		if b.Result == nil {
			return ""
		}
		return ansi.Sanitize(b.Result.Output)
	}
}
