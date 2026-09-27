package chat

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agent"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/session"
)

var (
	_ core.ChatService = (*Service)(nil)
	_ Sessions         = (*session.Service)(nil)
	_ Agents           = (*agents.Service)(nil)
	_ Runner           = (*agent.Runner)(nil)
	_ LLMSource        = (*fakeLLMs)(nil)
)

// closeTimeout bounds how long a test waits for Close.
const closeTimeout = 5 * time.Second

type fakeLLMs struct {
	mu      sync.Mutex
	clients map[core.ModelRef]*llmtest.Client
	errs    map[core.ModelRef]error
}

func (f *fakeLLMs) For(ref core.ModelRef) (core.LLM, core.ModelInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[ref]; err != nil {
		return nil, core.ModelInfo{}, err
	}
	c, ok := f.clients[ref]
	if !ok {
		return nil, core.ModelInfo{}, fmt.Errorf("unknown model %q", ref.String())
	}
	return c, core.ModelInfo{Ref: ref}, nil
}

// touchCounter counts Touch calls on the real session service.
type touchCounter struct {
	*session.Service
	mu sync.Mutex
	n  int
}

func (c *touchCounter) Touch(ctx context.Context, id core.SessionID) error {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.Service.Touch(ctx, id)
}

func (c *touchCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type nopBus struct{}

func (nopBus) Publish(event.Event) {}

func mainModel() core.ModelRef  { return core.ModelRef{Provider: "prov", Model: "main"} }
func smallModel() core.ModelRef { return core.ModelRef{Provider: "prov", Model: "small"} }

type fixture struct {
	t        *testing.T
	llms     *fakeLLMs
	sessions *session.Service
	touches  *touchCounter
	svc      *Service
	workDir  string
	deps     Deps
}

// newFixture wires a chat Service over the real session service, agents
// service, agent Runner, and a SQLite store in t.TempDir(). The main and
// small models get the given scripted clients.
func newFixture(t *testing.T, main, small *llmtest.Client) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "jig.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ag, err := agents.New(core.Config{
		DefaultModel: mainModel().String(), SmallModel: smallModel().String(),
		ModelAliases: map[string]string{"alt": "other/m"},
	}, agents.Sources{})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	gen := ids.New(clk, rand.Reader)
	llms := &fakeLLMs{
		clients: map[core.ModelRef]*llmtest.Client{mainModel(): main, smallModel(): small},
		errs:    map[core.ModelRef]error{},
	}
	sessions := session.New(session.Deps{Store: st, LLMs: llms, Agents: ag, Bus: nopBus{}, Clock: clk, IDs: gen})
	runner := agent.NewRunner(agent.Deps{
		LLMs: llms, Ext: ext.NewRegistry().Freeze(), Store: st, History: sessions,
		Bus: nopBus{}, Clock: clk, IDs: gen,
	})
	f := &fixture{t: t, llms: llms, sessions: sessions, touches: &touchCounter{Service: sessions}, workDir: t.TempDir()}
	f.deps = Deps{Sessions: f.touches, Agents: ag, LLMs: llms, Runner: runner, WorkDir: f.workDir}
	f.svc = New(f.deps)
	t.Cleanup(func() { f.close() })
	return f
}

func (f *fixture) close() {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := f.svc.Close(ctx); err != nil {
		f.t.Fatalf("Close: %v", err)
	}
}

func (f *fixture) get(id core.SessionID) core.Session {
	f.t.Helper()
	sess, err := f.sessions.Get(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return sess
}

func (f *fixture) roots() []core.Session {
	f.t.Helper()
	list, err := f.sessions.List(context.Background(), 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return list
}

func textOf(m core.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == core.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func wantConfigError(t *testing.T, err error) *ConfigError {
	t.Helper()
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T), want *ConfigError", err, err)
	}
	return ce
}

func TestSend_DefaultsToBuildAndCreatesSession(t *testing.T) {
	f := newFixture(t, llmtest.New(llmtest.Text("hi!")), llmtest.New(llmtest.Text("Greeting")))

	res, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hello there"})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID == "" || textOf(res.Message) != "hi!" {
		t.Errorf("result = %+v", res)
	}
	if res.Message.Agent != "build" || res.Message.Model != mainModel().String() {
		t.Errorf("message agent/model = %q/%q, want build/%s", res.Message.Agent, res.Message.Model, mainModel())
	}
	sess := f.get(res.SessionID)
	if sess.Agent != "build" || sess.Cwd != f.workDir {
		t.Errorf("session = %+v", sess)
	}
	if roots := f.roots(); len(roots) != 1 {
		t.Errorf("sessions = %d, want 1", len(roots))
	}
	if n := f.touches.count(); n != 1 {
		t.Errorf("Touch calls = %d, want 1", n)
	}
}

func TestSend_SubagentRejectedAsConfigError(t *testing.T) {
	main := llmtest.New()
	f := newFixture(t, main, llmtest.New())

	_, err := f.svc.Send(context.Background(), core.SendRequest{Agent: "explore", Text: "hi"})
	ce := wantConfigError(t, err)
	if want := `agent "explore" is a subagent; primary agents: build, plan`; ce.Error() != want {
		t.Errorf("err = %q, want %q", ce.Error(), want)
	}
	if roots := f.roots(); len(roots) != 0 {
		t.Errorf("sessions = %d, want 0", len(roots))
	}
	if n := len(main.Requests()); n != 0 {
		t.Errorf("model requests = %d, want 0", n)
	}
}

func TestSend_UnknownAgentIsConfigError(t *testing.T) {
	f := newFixture(t, llmtest.New(), llmtest.New())
	_, err := f.svc.Send(context.Background(), core.SendRequest{Agent: "nope", Text: "hi"})
	wantConfigError(t, err)
}

func TestSend_ModelFlagStoredOnSession(t *testing.T) {
	main := llmtest.New()
	f := newFixture(t, main, llmtest.New(llmtest.Text("T")))
	other := core.ModelRef{Provider: "other", Model: "m"}
	f.llms.clients[other] = llmtest.New(llmtest.Text("from other"))

	res, err := f.svc.Send(context.Background(), core.SendRequest{Model: "other/m", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.get(res.SessionID).Model; got != "other/m" {
		t.Errorf("session model = %q, want other/m", got)
	}
	if textOf(res.Message) != "from other" || res.Message.Model != "other/m" {
		t.Errorf("message = %+v", res.Message)
	}
	if n := len(main.Requests()); n != 0 {
		t.Errorf("main model requests = %d, want 0", n)
	}

	_, err = f.svc.Send(context.Background(), core.SendRequest{Model: "not-a-ref", Text: "hi"})
	wantConfigError(t, err)
}

func TestSend_PreflightCredentialErrorIsConfigError(t *testing.T) {
	main := llmtest.New()
	f := newFixture(t, main, llmtest.New())
	missing := errors.New(`missing API key: set PROV_API_KEY`)
	f.llms.errs[mainModel()] = missing

	_, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hi"})
	wantConfigError(t, err)
	if !errors.Is(err, missing) {
		t.Errorf("err = %v, want it to wrap %v", err, missing)
	}
	if roots := f.roots(); len(roots) != 0 {
		t.Errorf("sessions = %d, want 0", len(roots))
	}
}

func TestSend_TitleGeneratedInBackground(t *testing.T) {
	f := newFixture(t, llmtest.New(llmtest.Text("ok")), llmtest.New(llmtest.Text(`"Scripted Title"`)))

	res, err := f.svc.Send(context.Background(), core.SendRequest{Text: "please fix the flaky test\nmore detail"})
	if err != nil {
		t.Fatal(err)
	}
	f.close()
	if got := f.get(res.SessionID).Title; got != "Scripted Title" {
		t.Errorf("title = %q, want Scripted Title", got)
	}
}

func TestSend_TitleFailureKeepsPlaceholder(t *testing.T) {
	f := newFixture(t, llmtest.New(llmtest.Text("ok")), llmtest.New())

	res, err := f.svc.Send(context.Background(), core.SendRequest{Text: "please fix the flaky test\nmore detail"})
	if err != nil {
		t.Fatal(err)
	}
	f.close()
	if got := f.get(res.SessionID).Title; got != "please fix the flaky test" {
		t.Errorf("title = %q, want the placeholder", got)
	}
}

func TestSend_ResumeExistingSession(t *testing.T) {
	small := llmtest.New(llmtest.Text("Title"))
	f := newFixture(t, llmtest.New(llmtest.Text("first"), llmtest.Text("second")), small)

	first, err := f.svc.Send(context.Background(), core.SendRequest{Agent: "plan", Text: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.Send(context.Background(), core.SendRequest{SessionID: first.SessionID, Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID != first.SessionID || textOf(second.Message) != "second" {
		t.Errorf("second = %+v", second)
	}
	if second.Message.Agent != "plan" {
		t.Errorf("resumed agent = %q, want plan", second.Message.Agent)
	}
	msgs, err := f.sessions.Messages(context.Background(), first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Errorf("messages = %d, want 4", len(msgs))
	}
	if roots := f.roots(); len(roots) != 1 {
		t.Errorf("sessions = %d, want 1", len(roots))
	}
	f.close()
	if n := len(small.Requests()); n != 1 {
		t.Errorf("title requests = %d, want 1", n)
	}

	_, err = f.svc.Send(context.Background(), core.SendRequest{SessionID: "ses_missing", Text: "x"})
	wantConfigError(t, err)
}

func TestSend_RunErrorReturnsSessionAndBumpsUpdatedAt(t *testing.T) {
	f := newFixture(t, llmtest.New(), llmtest.New(llmtest.Text("T")))

	res, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hi"})
	if err == nil {
		t.Fatal("want run error")
	}
	var ce *ConfigError
	if errors.As(err, &ce) {
		t.Errorf("run error %v should not be a ConfigError", err)
	}
	if res.SessionID == "" {
		t.Error("SessionID empty on run error")
	}
	f.get(res.SessionID)
	if n := f.touches.count(); n != 1 {
		t.Errorf("Touch calls = %d, want 1", n)
	}
}

func TestClose_IdempotentAndRejectsSend(t *testing.T) {
	f := newFixture(t, llmtest.New(), llmtest.New())
	f.close()
	f.close()
	if _, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hi"}); err == nil {
		t.Error("Send after Close: want error")
	}
}

func TestConfigError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := error(&ConfigError{Err: inner})
	if !errors.Is(err, inner) || err.Error() != "inner" {
		t.Errorf("ConfigError = %v", err)
	}
}
