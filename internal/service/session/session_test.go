package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agent"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/task"
)

var (
	_ core.SessionService = (*Service)(nil)
	_ task.Sessions       = (*Service)(nil)
	_ agent.History       = (*Service)(nil)
	_ Store               = (*store.Store)(nil)
	_ Agents              = (*agents.Service)(nil)
)

// fakeLLMs hands out one scripted client per model ref and records every
// ref asked for.
type fakeLLMs struct {
	mu      sync.Mutex
	clients map[core.ModelRef]*llmtest.Client
	errs    map[core.ModelRef]error
	asked   []core.ModelRef
}

func (f *fakeLLMs) For(ref core.ModelRef) (core.LLM, core.ModelInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, ref)
	if err := f.errs[ref]; err != nil {
		return nil, core.ModelInfo{}, err
	}
	c, ok := f.clients[ref]
	if !ok {
		return nil, core.ModelInfo{}, fmt.Errorf("unknown model %q", ref.String())
	}
	return c, core.ModelInfo{Ref: ref}, nil
}

type recorder struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *recorder) Publish(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) all() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}

var (
	mainModel  = core.ModelRef{Provider: "prov", Model: "main"}
	smallModel = core.ModelRef{Provider: "prov", Model: "small"}
)

type fixture struct {
	t    *testing.T
	st   *store.Store
	clk  *clock.Fake
	rec  *recorder
	llms *fakeLLMs
	svc  *Service
}

func newFixture(t *testing.T, cfg core.Config) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "jig.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ag, err := agents.New(cfg, agents.Sources{})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := &fixture{
		t:    t,
		st:   st,
		clk:  clk,
		rec:  &recorder{},
		llms: &fakeLLMs{clients: map[core.ModelRef]*llmtest.Client{}, errs: map[core.ModelRef]error{}},
	}
	f.svc = New(Deps{Store: st, LLMs: f.llms, Agents: ag, Bus: f.rec, Clock: clk, IDs: ids.New(clk, rand.Reader)})
	return f
}

func defaultCfg() core.Config {
	return core.Config{DefaultModel: mainModel.String(), SmallModel: smallModel.String()}
}

func (f *fixture) create(agentName string) core.Session {
	f.t.Helper()
	sess, err := f.svc.Create(context.Background(), agentName, "/work")
	if err != nil {
		f.t.Fatal(err)
	}
	return sess
}

func (f *fixture) save(sid core.SessionID, role core.Role, parts ...core.Part) core.Message {
	f.t.Helper()
	f.clk.Advance(time.Second)
	m := core.Message{
		ID:        core.MessageID(fmt.Sprintf("msg_%d", f.clk.Now().Unix())),
		SessionID: sid,
		Role:      role,
		Parts:     parts,
		Status:    core.StatusComplete,
		CreatedAt: f.clk.Now(),
	}
	if err := f.st.SaveMessage(context.Background(), m); err != nil {
		f.t.Fatal(err)
	}
	return m
}

func text(s string) core.Part { return core.Part{Kind: core.PartText, Text: s} }

func TestCreate_PublishesSessionCreated(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")

	if sess.ID == "" || sess.Agent != "build" || sess.Cwd != "/work" || sess.Title != "" {
		t.Errorf("session = %+v", sess)
	}
	if !sess.CreatedAt.Equal(f.clk.Now()) || !sess.UpdatedAt.Equal(f.clk.Now()) {
		t.Errorf("times = %v/%v, want %v", sess.CreatedAt, sess.UpdatedAt, f.clk.Now())
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sess.ID || got.Agent != "build" || got.Cwd != "/work" {
		t.Errorf("stored = %+v", got)
	}

	evs := f.rec.all()
	if len(evs) != 1 {
		t.Fatalf("events = %v, want 1", evs)
	}
	sc, ok := evs[0].(event.SessionCreated)
	if !ok {
		t.Fatalf("event = %T, want SessionCreated", evs[0])
	}
	if sc.Session() != sess.ID || sc.Info.ID != sess.ID || sc.Info.Agent != "build" {
		t.Errorf("SessionCreated = %+v", sc)
	}
}

func TestCreateChild_InheritsCwdAndSetsFields(t *testing.T) {
	f := newFixture(t, defaultCfg())
	parent := f.create("build")

	child, err := f.svc.CreateChild(context.Background(), parent.ID, "explore", smallModel, "look around")
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(context.Background(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := core.Session{ParentID: parent.ID, Agent: "explore", Model: "prov/small", Title: "look around", Cwd: "/work"}
	if got.ParentID != want.ParentID || got.Agent != want.Agent || got.Model != want.Model ||
		got.Title != want.Title || got.Cwd != want.Cwd {
		t.Errorf("child = %+v, want %+v", got, want)
	}

	roots, err := f.svc.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].ID != parent.ID {
		t.Errorf("List = %+v, want only the parent", roots)
	}
}

func TestCreateChild_MissingParent(t *testing.T) {
	f := newFixture(t, defaultCfg())
	if _, err := f.svc.CreateChild(context.Background(), "ses_missing", "explore", smallModel, "x"); err == nil {
		t.Error("CreateChild with missing parent: want error")
	}
}

func TestGet_MissingWrapsErrNotFound(t *testing.T) {
	f := newFixture(t, defaultCfg())
	if _, err := f.svc.Get(context.Background(), "ses_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: err = %v, want ErrNotFound", err)
	}
	sess := f.create("build")
	if _, err := f.svc.Get(context.Background(), sess.ID); err != nil {
		t.Errorf("Get existing: %v", err)
	}
}

func TestUpdate_PersistsTitle(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")
	sess.Title = "renamed"
	if err := f.svc.Update(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "renamed" {
		t.Errorf("Title = %q, want renamed", got.Title)
	}
}

func TestTouch_BumpsUpdatedAt(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")
	f.clk.Advance(time.Minute)
	if err := f.svc.Touch(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(f.clk.Now()) || !got.CreatedAt.Equal(sess.CreatedAt) {
		t.Errorf("times = %v/%v, want created %v updated %v", got.CreatedAt, got.UpdatedAt, sess.CreatedAt, f.clk.Now())
	}
}
