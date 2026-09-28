package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/actions"
)

// testStart is the fake clock's start time for every App test.
func testStart() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }

// testWorkDir is the WorkDir (and ProjectKey) every test App runs in.
const testWorkDir = "/work"

// fakeChat implements core.ChatService, recording every call. Send returns
// immediately (the run itself is driven by the events a test delivers).
type fakeChat struct {
	mu      sync.Mutex
	sends   []core.SendRequest
	ctxs    []context.Context
	cancels []core.SessionID
	result  core.SendResult
	errs    []error // the n-th Send returns errs[n] (nil past the end)
}

func (f *fakeChat) Send(ctx context.Context, req core.SendRequest) (core.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, req)
	f.ctxs = append(f.ctxs, ctx)
	var err error
	if n := len(f.sends) - 1; n < len(f.errs) {
		err = f.errs[n]
	}
	return f.result, err
}

func (f *fakeChat) Compact(context.Context, core.SessionID) error { return nil }

func (f *fakeChat) Cancel(id core.SessionID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
}

func (f *fakeChat) Close(context.Context) error { return nil }

// configureCall is one recorded SessionService.Configure call.
type configureCall struct {
	ID           core.SessionID
	Agent, Model string
}

// recSessions implements core.SessionService over one stored session,
// recording Configure calls.
type recSessions struct {
	mu        sync.Mutex
	info      core.Session
	msgs      []core.Message
	todos     []core.Todo
	configure []configureCall
}

func (f *recSessions) List(context.Context, int) ([]core.Session, error) { return nil, nil }
func (f *recSessions) ListForCwd(context.Context, string, int) ([]core.Session, error) {
	return nil, nil
}

func (f *recSessions) Get(_ context.Context, id core.SessionID) (core.Session, error) {
	if id != f.info.ID {
		return core.Session{}, errors.New("not found")
	}
	return f.info, nil
}

func (f *recSessions) Messages(context.Context, core.SessionID) ([]core.Message, error) {
	return f.msgs, nil
}

func (f *recSessions) Todos(context.Context, core.SessionID) ([]core.Todo, error) {
	return f.todos, nil
}

func (f *recSessions) Rename(context.Context, core.SessionID, string) error { return nil }

func (f *recSessions) Configure(_ context.Context, id core.SessionID, agent, model string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configure = append(f.configure, configureCall{ID: id, Agent: agent, Model: model})
	return nil
}

// fakePrefs implements core.PrefsService in memory.
type fakePrefs struct {
	mu sync.Mutex
	p  core.Prefs
}

func (f *fakePrefs) Get() core.Prefs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.p
}

func (f *fakePrefs) Update(fn func(*core.Prefs)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.p)
	return nil
}

// fakeAgents implements core.AgentService.
type fakeAgents []core.Agent

func (f fakeAgents) Primary() []core.Agent { return slices.Clone(f) }

// fakeCatalog implements core.CatalogService.
type fakeCatalog []core.ProviderStatus

func (f fakeCatalog) Providers() []core.ProviderStatus { return slices.Clone(f) }

// deferredMsg is what a test App's after hook yields instead of sleeping:
// the message a tick would have delivered, and the delay it asked for.
type deferredMsg struct {
	d   time.Duration
	msg tea.Msg
}

// testConfig is what newTestApp builds an App from.
type testConfig struct {
	opts      Options
	w, h      int
	agents    fakeAgents
	catalog   fakeCatalog
	sessions  *recSessions
	prefs     *fakePrefs
	keyConfig map[string]string
}

// testOpt adjusts a testConfig.
type testOpt func(*testConfig)

// withSize sets the initial window size (default 120×30).
func withSize(w, h int) testOpt { return func(c *testConfig) { c.w, c.h = w, h } }

// withResume makes the App resume sess, whose stored history is msgs.
func withResume(sess core.Session, msgs []core.Message, todos []core.Todo) testOpt {
	return func(c *testConfig) {
		c.opts.Session = sess.ID
		c.sessions.info = sess
		c.sessions.msgs = msgs
		c.sessions.todos = todos
	}
}

// withKeybinds adds "[keybinds]" config entries to the resolved keymap.
func withKeybinds(cfg map[string]string) testOpt {
	return func(c *testConfig) { c.keyConfig = cfg }
}

// withPrefs seeds the stored prefs.
func withPrefs(p core.Prefs) testOpt { return func(c *testConfig) { c.prefs.p = p } }

// testAgents are the default primary agents: build then plan, both on
// anthropic/claude-sonnet-5.
func testAgents() fakeAgents {
	ref := core.ModelRef{Provider: "anthropic", Model: "claude-sonnet-5"}
	return fakeAgents{{Name: "build", Model: ref, Mode: core.ModePrimary}, {Name: "plan", Model: ref, Mode: core.ModePrimary}}
}

// testCatalog is one configured provider with claude-sonnet-5 (200k ctx).
func testCatalog() fakeCatalog {
	return fakeCatalog{{Configured: true, Info: core.ProviderInfo{
		ID: "anthropic", Name: "Anthropic",
		Models: []core.ModelInfo{{Ref: core.ModelRef{Provider: "anthropic", Model: "claude-sonnet-5"}, ContextWindow: 200000}},
	}}}
}

// testApp drives an App synchronously: every Cmd an Update returns runs
// at once and its message is fed back in, except ticks (collected in
// deferred, delivered by fire), Chat.Send's return (collected in
// returns, delivered by returnSend: a real Send returns only after its
// run ended), and the bus wait (tests deliver events with event
// directly).
type testApp struct {
	t        testing.TB
	app      *App
	chat     *fakeChat
	sessions *recSessions
	prefs    *fakePrefs
	clk      *clock.Fake
	deferred []deferredMsg
	returns  []sendDoneMsg
	quit     bool
}

// newTestApp builds an App over fakes with a fake clock, runs Init, and
// sizes it (120×30 unless withSize).
func newTestApp(t testing.TB, opts ...testOpt) *testApp {
	t.Helper()
	clk := clock.NewFake(testStart())
	cfg := &testConfig{
		w: 120, h: 30,
		agents:   testAgents(),
		catalog:  testCatalog(),
		sessions: &recSessions{},
		prefs:    &fakePrefs{},
		opts: Options{
			WorkDir: testWorkDir, ProjectKey: testWorkDir,
			Theme:  "dark",
			Images: imgrender.Env{Override: "blocks"},
			Clock:  clk,
		},
	}
	for _, o := range opts {
		o(cfg)
	}
	cat := actions.NewCatalogue(nil)
	km, _ := actions.Resolve(actions.DefaultBindings(), cfg.keyConfig, cat)
	cfg.opts.Actions, cfg.opts.Keymap = cat, km

	ta := &testApp{t: t, chat: &fakeChat{}, sessions: cfg.sessions, prefs: cfg.prefs, clk: clk}
	ports := Ports{
		Chat: ta.chat, Sessions: cfg.sessions, Prefs: cfg.prefs,
		Agents: cfg.agents, Catalog: cfg.catalog,
	}
	ta.app = New(ports, cfg.opts)
	ta.app.after = func(d time.Duration, msg tea.Msg) tea.Cmd {
		return func() tea.Msg { return deferredMsg{d: d, msg: msg} }
	}
	ta.run(ta.app.Init())
	ta.send(tea.WindowSizeMsg{Width: cfg.w, Height: cfg.h})
	return ta
}

// send delivers msg to the App and runs the resulting Cmds.
func (ta *testApp) send(msg tea.Msg) {
	ta.t.Helper()
	_, cmd := ta.app.Update(msg)
	ta.run(cmd)
}

// run executes cmd synchronously, feeding its message back into the App.
func (ta *testApp) run(cmd tea.Cmd) {
	ta.t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			ta.run(c)
		}
	case deferredMsg:
		ta.deferred = append(ta.deferred, msg)
	case sendDoneMsg:
		ta.returns = append(ta.returns, msg)
	case tea.QuitMsg:
		ta.quit = true
	default:
		ta.send(msg)
	}
}

// fire delivers every tick collected so far (not ones they schedule).
func (ta *testApp) fire() {
	ta.t.Helper()
	pending := ta.deferred
	ta.deferred = nil
	for _, d := range pending {
		ta.send(d.msg)
	}
}

// returnSend delivers the oldest pending Chat.Send return.
func (ta *testApp) returnSend() {
	ta.t.Helper()
	if len(ta.returns) == 0 {
		ta.t.Fatal("returnSend: no Send in flight")
	}
	msg := ta.returns[0]
	ta.returns = ta.returns[1:]
	ta.send(msg)
}

// event delivers ev as the bus bridge would.
func (ta *testApp) event(ev event.Event) {
	ta.t.Helper()
	ta.send(eventMsg{ev: ev})
}

// key presses one key, by its tea.Key String() form.
func (ta *testApp) key(k string) {
	ta.t.Helper()
	ta.send(keyPress(k))
}

// typeText types s one rune at a time.
func (ta *testApp) typeText(s string) {
	ta.t.Helper()
	for _, r := range s {
		ta.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// view returns the rendered frame's content.
func (ta *testApp) view() string { return ta.app.View().Content }

// keyPress builds the KeyPressMsg whose String() is k.
func keyPress(k string) tea.KeyPressMsg {
	named := map[string]tea.Key{
		"enter":     {Code: tea.KeyEnter},
		"esc":       {Code: tea.KeyEscape},
		"tab":       {Code: tea.KeyTab},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"up":        {Code: tea.KeyUp},
		"down":      {Code: tea.KeyDown},
	}
	if key, ok := named[k]; ok {
		return tea.KeyPressMsg(key)
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(rest)[0], Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// rootBase is the event Base of the root session "ses_1".
func rootBase() event.Base { return event.Base{SessionID: "ses_1", RootID: "ses_1"} }

// sendAndAdopt types text, presses enter, and delivers the SessionCreated
// that makes "ses_1" the root.
func (ta *testApp) sendAndAdopt(text string) {
	ta.t.Helper()
	ta.typeText(text)
	ta.key("enter")
	ta.event(event.SessionCreated{Base: rootBase(), Info: core.Session{ID: "ses_1", Agent: "build"}})
}
