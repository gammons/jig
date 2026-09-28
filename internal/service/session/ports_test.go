package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/pathid"
)

func TestListForCwd_FiltersRootsByCanonicalCwd(t *testing.T) {
	f := newFixture(t, defaultCfg())
	dir := t.TempDir()
	other := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	create := func(cwd string) core.Session {
		t.Helper()
		sess, err := f.svc.Create(context.Background(), "build", cwd)
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}

	first := create(pathid.Key(dir))
	f.clk.Advance(time.Second)
	second := create(pathid.Key(dir))
	create(pathid.Key(other))
	if _, err := f.svc.CreateChild(context.Background(), first.ID, "explore", core.ModelRef{}, "child"); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.ListForCwd(context.Background(), dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("ListForCwd(dir) = %+v, want [second, first]", got)
	}

	gotLink, err := f.svc.ListForCwd(context.Background(), link, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotLink) != 2 || gotLink[0].ID != second.ID || gotLink[1].ID != first.ID {
		t.Errorf("ListForCwd(link) = %+v, want the same roots as ListForCwd(dir)", gotLink)
	}
}

func TestRename_TrimsCapsAndPublishes(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")

	if err := f.svc.Rename(context.Background(), sess.ID, " x "); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "x" {
		t.Errorf("Title = %q, want %q", got.Title, "x")
	}

	evs := f.rec.all()
	if len(evs) != 2 {
		t.Fatalf("events = %+v, want 2 (create, rename)", evs)
	}
	su, ok := evs[1].(event.SessionUpdated)
	if !ok {
		t.Fatalf("event[1] = %T, want SessionUpdated", evs[1])
	}
	if su.SessionID != sess.ID || su.RootID != sess.ID || su.Info.Title != "x" {
		t.Errorf("SessionUpdated = %+v, want session/root %q and Title %q", su, sess.ID, "x")
	}

	if err := f.svc.Rename(context.Background(), sess.ID, "  "); err == nil {
		t.Error("Rename(\"  \"): want error")
	}
	if len(f.rec.all()) != 2 {
		t.Errorf("events after failed Rename = %d, want 2 (no new event)", len(f.rec.all()))
	}
}

func TestConfigure_ValidatesAndPublishes(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")

	if err := f.svc.Configure(context.Background(), sess.ID, "nosuchagent", ""); err == nil {
		t.Error("Configure with unknown agent: want error")
	}
	if err := f.svc.Configure(context.Background(), sess.ID, "", "bad"); err == nil {
		t.Error("Configure with invalid model: want error")
	}
	if len(f.rec.all()) != 1 {
		t.Fatalf("events after invalid Configure calls = %d, want 1 (create only)", len(f.rec.all()))
	}

	if err := f.svc.Configure(context.Background(), sess.ID, "plan", "anthropic/m"); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "plan" || got.Model != "anthropic/m" {
		t.Errorf("session = %+v, want agent %q model %q", got, "plan", "anthropic/m")
	}
	evs := f.rec.all()
	if len(evs) != 2 {
		t.Fatalf("events = %+v, want 2 (create, configure)", evs)
	}
	su, ok := evs[1].(event.SessionUpdated)
	if !ok {
		t.Fatalf("event[1] = %T, want SessionUpdated", evs[1])
	}
	if su.SessionID != sess.ID || su.RootID != sess.ID || su.Info.Agent != "plan" || su.Info.Model != "anthropic/m" {
		t.Errorf("SessionUpdated = %+v", su)
	}

	if err := f.svc.Configure(context.Background(), sess.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.rec.all()) != 2 {
		t.Errorf("events after no-op Configure = %d, want 2 (no new event)", len(f.rec.all()))
	}
}

func TestConfigure_AcceptsAliasStoresCanonical(t *testing.T) {
	cfg := defaultCfg()
	cfg.ModelAliases = map[string]string{"fast": "anthropic/haiku"}
	f := newFixture(t, cfg)
	sess := f.create("build")

	if err := f.svc.Configure(context.Background(), sess.ID, "", "fast"); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "anthropic/haiku" {
		t.Errorf("Model = %q, want canonical %q", got.Model, "anthropic/haiku")
	}
}

func TestConfigure_RejectsNonPrimaryAgent(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")

	if err := f.svc.Configure(context.Background(), sess.ID, "explore", ""); err == nil {
		t.Error(`Configure with subagent "explore": want error`)
	}
	if err := f.svc.Configure(context.Background(), sess.ID, "title", ""); err == nil {
		t.Error(`Configure with hidden agent "title": want error`)
	}
	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "build" {
		t.Errorf("Agent = %q, want unchanged %q", got.Agent, "build")
	}
}

func TestGenerateTitle_DoesNotClobberRename(t *testing.T) {
	f := newFixture(t, defaultCfg())
	firstPrompt := "hello world"
	sess := f.withPlaceholder(PlaceholderTitle(firstPrompt))

	gate := make(chan struct{})
	turn := llmtest.Text("Generated Title")
	turn.Gate = gate
	f.llms.clients[smallModel] = llmtest.New(turn)

	done := make(chan error, 1)
	go func() {
		done <- f.svc.GenerateTitle(context.Background(), sess.ID, firstPrompt)
	}()

	if err := f.svc.Rename(context.Background(), sess.ID, "mine"); err != nil {
		t.Fatal(err)
	}
	close(gate)

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := f.title(sess.ID); got != "mine" {
		t.Errorf("title = %q, want %q", got, "mine")
	}
}

func TestModify_ConcurrentRenameAndConfigure(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")

	const n = 50
	titles := make([]string, n)
	agentNames := make([]string, n)
	models := make([]string, n)
	for i := range n {
		titles[i] = fmt.Sprintf("title-%d", i)
		if i%4 == 1 {
			agentNames[i] = "build"
		} else {
			agentNames[i] = "plan"
		}
		models[i] = fmt.Sprintf("prov/m%d", i)
	}

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if err := f.svc.Rename(context.Background(), sess.ID, titles[i]); err != nil {
					t.Error(err)
				}
				return
			}
			if err := f.svc.Configure(context.Background(), sess.ID, agentNames[i], models[i]); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	got, err := f.svc.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title == "" || got.Agent == "" || got.Model == "" {
		t.Fatalf("session = %+v, has an empty field", got)
	}

	titleOK, agentOK, modelOK := false, false, false
	for i := range n {
		if got.Title == titles[i] {
			titleOK = true
		}
		if got.Agent == agentNames[i] {
			agentOK = true
		}
		if got.Model == models[i] {
			modelOK = true
		}
	}
	if !titleOK {
		t.Errorf("Title = %q, not among written titles", got.Title)
	}
	if !agentOK {
		t.Errorf("Agent = %q, not among written agents", got.Agent)
	}
	if !modelOK {
		t.Errorf("Model = %q, not among written models", got.Model)
	}
}

func TestTodos_ReturnsStoredList(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")
	want := []core.Todo{
		{Content: "write tests", Status: "in_progress"},
		{Content: "implement", Status: "pending"},
	}
	if err := f.st.ReplaceTodos(context.Background(), sess.ID, want); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.Todos(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("Todos = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Todos[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
