package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestExec_AfterHooksRunOnToolErrorPanicAndCancel(t *testing.T) {
	cases := map[string]func(ctx context.Context, call core.ToolCall) (core.ToolResult, error){
		"error": func(context.Context, core.ToolCall) (core.ToolResult, error) {
			return core.ToolResult{}, errors.New("disk on fire")
		},
		"panic": func(context.Context, core.ToolCall) (core.ToolResult, error) {
			panic("kaboom")
		},
		"cancel": func(ctx context.Context, _ core.ToolCall) (core.ToolResult, error) {
			<-ctx.Done()
			return core.ToolResult{}, ctx.Err()
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			log := &callLog{}
			r, _ := newExecRunner(stubHook{name: "h", log: log})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tool := stubTool{name: "x", run: func(c context.Context, call core.ToolCall) (core.ToolResult, error) {
				if name == "cancel" {
					cancel()
				}
				return run(c, call)
			}}
			res := r.execute(ctx, execRC(), []ext.Tool{tool}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})
			if !res[0].IsError {
				t.Errorf("result = %+v, want an error result", res[0])
			}
			if got := strings.Join(log.all(), ","); got != "before:h:c1,after:h:c1" {
				t.Errorf("hook log = %s, want After to run", got)
			}
		})
	}
}

func TestExec_PanicResultMessageUnchanged(t *testing.T) {
	r, _ := newExecRunner()
	boom := stubTool{name: "boom", run: func(context.Context, core.ToolCall) (core.ToolResult, error) { panic("kaboom") }}
	res := r.execute(context.Background(), execRC(), []ext.Tool{boom}, []core.ToolCall{llmtest.Call("c1", "boom", `{}`)})
	if res[0].Output != "tool boom panicked: kaboom" {
		t.Errorf("Output = %q", res[0].Output)
	}
}

func TestExec_OutputTruncatedAt50KB(t *testing.T) {
	// "é" is two bytes; an odd prefix forces the 50 KB cut mid-rune.
	big := "a" + strings.Repeat("é", 40*1024)
	tool := stubTool{name: "x", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		return core.ToolResult{Output: big}, nil
	}}
	r, _ := newExecRunner()
	res := r.execute(context.Background(), execRC(), []ext.Tool{tool}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})

	const marker = "\n[output truncated at 50 KB]"
	out := res[0].Output
	if !strings.HasSuffix(out, marker) {
		t.Fatalf("output does not end with the truncation marker; len=%d", len(out))
	}
	body := strings.TrimSuffix(out, marker)
	if len(body) > 50*1024 || len(body) < 50*1024-utf8.UTFMax {
		t.Errorf("body len = %d, want just under %d", len(body), 50*1024)
	}
	if !utf8.ValidString(body) || !strings.HasPrefix(big, body) {
		t.Error("body is not a valid UTF-8 prefix of the output")
	}
}

func TestExec_OutputAt50KBUntouched(t *testing.T) {
	exact := strings.Repeat("a", 50*1024)
	tool := stubTool{name: "x", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		return core.ToolResult{Output: exact}, nil
	}}
	r, _ := newExecRunner()
	res := r.execute(context.Background(), execRC(), []ext.Tool{tool}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})
	if res[0].Output != exact {
		t.Errorf("output of exactly 50 KB was changed (len %d)", len(res[0].Output))
	}
}

func TestRunner_MaxStepsNoticePublishedAsTextDelta(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c2", "echo", `{}`)),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	f.rc.Agent.MaxSteps = 2
	got, err := NewRunner(f.deps).Run(context.Background(), f.rc, "loop")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var deltas []event.TextDelta
	for _, e := range f.rec.all() {
		if d, ok := e.(event.TextDelta); ok {
			deltas = append(deltas, d)
		}
	}
	if len(deltas) != 1 {
		t.Fatalf("TextDeltas = %+v, want exactly the notice", deltas)
	}
	d := deltas[0]
	if d.Text != "[stopped: reached max_steps (2)]" || d.MessageID != got.ID || d.Session() != f.rc.SessionID {
		t.Errorf("delta = %+v, want the notice on message %s", d, got.ID)
	}
}

func TestRetryDelay_RetryAfterCappedAt60s(t *testing.T) {
	err := &core.LLMError{Retryable: true, RetryAfter: 10 * time.Minute}
	if d, ok := retryDelay(err, false, 1); !ok || d != 60*time.Second {
		t.Errorf("retryDelay = %v, %v; want 60s, true", d, ok)
	}
}
