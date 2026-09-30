package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gammons/jig/internal/core"
)

// sseEvent formats one Anthropic SSE event.
func sseEvent(typ, data string) string {
	return "event: " + typ + "\ndata: " + data + "\n\n"
}

func sseMessage(stop string, blocks ...string) string {
	s := sseEvent("message_start", `{"type":"message_start","message":{"model":"m1","id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`)
	for _, b := range blocks {
		s += b
	}
	s += sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+stop+`","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`)
	return s + sseEvent("message_stop", `{"type":"message_stop"}`)
}

func sseText(text string) string {
	return sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		sseEvent("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text)) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)
}

func sseGlobCall() string {
	return sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"glob","input":{}}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":\"*.txt\"}"}}`) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)
}

// fakeAnthropic answers the run's first step with a glob call, the step
// after the tool result with text, and tool-less (title) requests with a
// title.
func fakeAnthropic(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case strings.Contains(string(body), `"tool_result"`):
			io.WriteString(w, sseMessage("end_turn", sseText("Found notes.txt")))
		case strings.Contains(string(body), `"tools"`):
			io.WriteString(w, sseMessage("tool_use", sseGlobCall()))
		default:
			io.WriteString(w, sseMessage("end_turn", sseText("Glob title")))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRun_RunFailureExits1WithOneErrorLine(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)
	}))
	t.Cleanup(srv.Close)
	env.writeConfig(t, fmt.Sprintf(`
default_model = "fake/m1"
[providers.fake]
type = "anthropic"
base_url = %q
api_key = "k"
models = ["m1"]
`, srv.URL))

	code, out, stderr := env.run(t, "run", "--cwd", env.workDir, "hi")
	if code != exitRunFailed {
		t.Errorf("exit = %d, want %d", code, exitRunFailed)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(stderr, "error: ") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one error line", stderr)
	}
}

func TestRun_CancelDuringRunExits1(t *testing.T) {
	env := newTestEnv(t)
	streaming := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if !strings.Contains(string(body), `"tools"`) {
			io.WriteString(w, sseMessage("end_turn", sseText("Title")))
			return
		}
		io.WriteString(w, sseEvent("message_start", `{"type":"message_start","message":{"model":"m1","id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`))
		w.(http.Flusher).Flush()
		once.Do(func() { close(streaming) })
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	env.writeConfig(t, fmt.Sprintf(`
default_model = "fake/m1"
[providers.fake]
type = "anthropic"
base_url = %q
api_key = "k"
models = ["m1"]
`, srv.URL))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-streaming:
			cancel()
		case <-ctx.Done():
		}
	}()
	var out, errw bytes.Buffer
	code := Run(ctx, []string{"run", "--cwd", env.workDir, "hi"}, Stdio{Out: &out, Err: &errw}, env.getenv)
	if code != exitRunFailed {
		t.Errorf("exit = %d, want %d (stderr %q)", code, exitRunFailed, errw.String())
	}
	if !strings.HasPrefix(errw.String(), "error: ") || strings.Count(errw.String(), "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one error line", errw.String())
	}
}

func TestRun_HeadlessRendersToolAndText(t *testing.T) {
	env := newTestEnv(t)
	srv := fakeAnthropic(t)
	env.writeConfig(t, fmt.Sprintf(`
default_model = "fake/m1"
[providers.fake]
type = "anthropic"
base_url = %q
api_key = "k"
models = ["m1"]
`, srv.URL))
	if err := os.WriteFile(filepath.Join(env.workDir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, stderr := env.run(t, "run", "--cwd", env.workDir, "find", "text files")
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if out != "Found notes.txt\n" {
		t.Errorf("stdout = %q, want %q", out, "Found notes.txt\n")
	}
	if stderr != "→ glob {\"pattern\":\"*.txt\"}\n" {
		t.Errorf("stderr = %q, want the glob line only", stderr)
	}

	code, out, _ = env.run(t, "sessions")
	if code != exitOK || strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "ses_") {
		t.Errorf("sessions: exit %d, stdout %q; want one session", code, out)
	}
}

func TestWarnIgnoredEffort(t *testing.T) {
	providers := []core.ProviderStatus{{Info: core.ProviderInfo{ID: "anthropic", Models: []core.ModelInfo{
		{Ref: core.ModelRef{Provider: "anthropic", Model: "opus"}, Efforts: []core.Effort{core.EffortLow, core.EffortHigh}},
		{Ref: core.ModelRef{Provider: "anthropic", Model: "haiku"}},
	}}}}
	tests := []struct {
		name, model, effort, want string
	}{
		{"no levels", "anthropic/haiku", "high", "warning: anthropic/haiku has no reasoning effort levels; --effort is ignored\n"},
		{"has levels", "anthropic/opus", "high", ""},
		{"unknown model", "anthropic/nope", "high", ""},
		{"no effort", "anthropic/haiku", "", ""},
		{"no model", "", "high", ""},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		warnIgnoredEffort(&buf, providers, tt.model, tt.effort)
		if buf.String() != tt.want {
			t.Errorf("%s: output %q, want %q", tt.name, buf.String(), tt.want)
		}
	}
}
