package plain

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

func base(id string) event.Base { return event.Base{SessionID: core.SessionID(id)} }

func render(evs ...event.Event) (string, string) {
	var out, errw bytes.Buffer
	ch := make(chan event.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	New(&out, &errw).Run(ch)
	return out.String(), errw.String()
}

func call(name, input string) core.ToolCall {
	return core.ToolCall{ID: "c1", Name: name, Input: json.RawMessage(input)}
}

func TestPlain_RootTextToStdoutToolsToStderr(t *testing.T) {
	long := `{"pattern":"` + strings.Repeat("x", 100) + `"}`
	out, errw := render(
		event.TextDelta{Base: base("root"), Text: "hello "},
		event.ReasoningDelta{Base: base("root"), Text: "thinking hard"},
		event.ToolCallStarted{Base: base("root"), Call: call("glob", "{\n  \"pattern\": \"*.go\"\n}")},
		event.ToolCallFinished{Base: base("root"), Result: core.ToolResult{Name: "glob", Output: "a.go"}},
		event.ToolCallStarted{Base: base("root"), Call: call("grep", long)},
		event.ToolCallFinished{Base: base("root"), Result: core.ToolResult{Name: "grep", Output: "bad pattern\nmore detail", IsError: true}},
		event.TextDelta{Base: base("root"), Text: "world\n"},
		event.RunFailed{Base: base("root"), Err: "boom"},
	)
	if out != "hello world\n" {
		t.Errorf("stdout = %q, want %q", out, "hello world\n")
	}
	want := "→ glob {\"pattern\":\"*.go\"}\n" +
		"→ grep " + string([]rune(long)[:80]) + "\n" +
		"  ✗ bad pattern\n" +
		"error: boom\n"
	if errw != want {
		t.Errorf("stderr =\n%q\nwant\n%q", errw, want)
	}
	if strings.Contains(out+errw, "thinking") {
		t.Error("reasoning was printed")
	}
}

func TestPlain_ChildIndentedAndTextSuppressed(t *testing.T) {
	out, errw := render(
		event.TextDelta{Base: base("root"), Text: "delegating"},
		event.SubagentSpawned{Base: base("root"), Child: "kid", Agent: "explore", Description: "find files"},
		event.TextDelta{Base: base("kid"), Text: "child text"},
		event.ToolCallStarted{Base: base("kid"), Call: call("read", `{"path":"a"}`)},
		event.ToolCallFinished{Base: base("kid"), Result: core.ToolResult{Output: "nope", IsError: true}},
		event.SubagentSpawned{Base: base("kid"), Child: "grandkid", Agent: "general", Description: "deeper"},
		event.ToolCallStarted{Base: base("grandkid"), Call: call("bash", `{"command":"ls"}`)},
		event.RunFailed{Base: base("kid"), Err: "child failed"},
		event.RunFinished{Base: base("kid")},
		event.RunFinished{Base: base("root")},
	)
	if out != "delegating\n" {
		t.Errorf("stdout = %q, want %q", out, "delegating\n")
	}
	want := "↳ explore: find files\n" +
		"  → read {\"path\":\"a\"}\n" +
		"    ✗ nope\n" +
		"  ↳ general: deeper\n" +
		"    → bash {\"command\":\"ls\"}\n"
	if errw != want {
		t.Errorf("stderr =\n%q\nwant\n%q", errw, want)
	}
}

func TestPlain_TrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		evs  []event.Event
		want string
	}{
		{"adds newline", []event.Event{
			event.TextDelta{Base: base("r"), Text: "done"},
			event.RunFinished{Base: base("r")},
		}, "done\n"},
		{"keeps existing newline", []event.Event{
			event.TextDelta{Base: base("r"), Text: "done\n"},
			event.RunFinished{Base: base("r")},
		}, "done\n"},
		{"no text no newline", []event.Event{
			event.RunFinished{Base: base("r")},
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := render(tc.evs...)
			if out != tc.want {
				t.Errorf("stdout = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestPlain_BlankLineBetweenSteps(t *testing.T) {
	out, _ := render(
		event.MessageStarted{Base: base("root"), MessageID: "m1"},
		event.TextDelta{Base: base("root"), MessageID: "m1", Text: "Let me look."},
		event.ToolCallStarted{Base: base("root"), Call: call("read", `{"path":"a"}`)},
		event.ToolCallFinished{Base: base("root"), Result: core.ToolResult{Name: "read", Output: "x"}},
		event.MessageStarted{Base: base("root"), MessageID: "m2"},
		event.TextDelta{Base: base("root"), MessageID: "m2", Text: "Done."},
		event.RunFinished{Base: base("root")},
	)
	if want := "Let me look.\n\nDone.\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestPlain_StepSeparatorAfterTrailingNewline(t *testing.T) {
	out, _ := render(
		event.MessageStarted{Base: base("root"), MessageID: "m1"},
		event.TextDelta{Base: base("root"), MessageID: "m1", Text: "one\n"},
		event.MessageStarted{Base: base("root"), MessageID: "m2"},
		event.MessageStarted{Base: base("kid"), MessageID: "k1"},
		event.MessageStarted{Base: base("root"), MessageID: "m3"},
		event.TextDelta{Base: base("root"), MessageID: "m3", Text: "two"},
		event.RunFinished{Base: base("root")},
	)
	if want := "one\n\ntwo\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestPlain_NoSeparatorBeforeFirstTextOrForTextlessStep(t *testing.T) {
	out, _ := render(
		event.MessageStarted{Base: base("root"), MessageID: "m1"},
		event.ToolCallStarted{Base: base("root"), Call: call("read", `{}`)},
		event.MessageStarted{Base: base("root"), MessageID: "m2"},
		event.TextDelta{Base: base("root"), MessageID: "m2", Text: "only"},
		event.MessageStarted{Base: base("root"), MessageID: "m3"},
		event.RunFinished{Base: base("root")},
	)
	if want := "only\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}
