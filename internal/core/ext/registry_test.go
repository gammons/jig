package ext

import (
	"context"
	"sync"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// fakeTool is a minimal Tool stub for tests.
type fakeTool struct{ name string }

func (f fakeTool) Name() string           { return f.name }
func (f fakeTool) Description() string    { return "fake tool " + f.name }
func (f fakeTool) Schema() map[string]any { return nil }
func (f fakeTool) Concurrent() bool       { return false }
func (f fakeTool) Run(_ context.Context, _ RunContext, _ core.ToolCall) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}

// fakeToolHook is a minimal ToolHook stub for tests.
type fakeToolHook struct{}

func (fakeToolHook) Before(_ context.Context, _ RunContext, _ Tool, call core.ToolCall) (core.ToolCall, Verdict, error) {
	return call, Verdict{}, nil
}

func (fakeToolHook) After(_ context.Context, _ RunContext, _ Tool, _ core.ToolCall, res core.ToolResult) core.ToolResult {
	return res
}

// fakeTransform is a ContextTransform stub carrying an id so tests can
// distinguish transforms that share a Priority.
type fakeTransform struct {
	id       string
	priority int
}

func (f fakeTransform) Priority() int { return f.priority }
func (f fakeTransform) Transform(_ context.Context, _ RunContext, _ *core.LLMRequest) error {
	return nil
}

// fakeSubscriber is a minimal EventSubscriber stub for tests.
type fakeSubscriber struct{}

func (fakeSubscriber) Handle(_ context.Context, _ event.Event) {}

// fakeCommand is a minimal Command stub for tests.
type fakeCommand struct{ name string }

func (f fakeCommand) Name() string                            { return f.name }
func (f fakeCommand) Description() string                     { return "fake command " + f.name }
func (f fakeCommand) Run(_ context.Context, _ []string) error { return nil }

// fakeProvider is a minimal ProviderFactory stub for tests.
type fakeProvider struct{ typ string }

func (f fakeProvider) Type() string { return f.typ }
func (f fakeProvider) New(_ core.ProviderInfo, _ core.ProviderConfig, _ string) (core.LLM, error) {
	return nil, nil
}

func TestRegistry_DuplicateToolRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.AddTool(fakeTool{name: "bash"}); err != nil {
		t.Fatalf("AddTool #1: %v", err)
	}
	if err := r.AddTool(fakeTool{name: "bash"}); err == nil {
		t.Fatal("AddTool #2 with duplicate name: want error, got nil")
	}
}

func TestRegistry_NilArgsRejected(t *testing.T) {
	r := NewRegistry()

	if err := r.AddTool(nil); err == nil {
		t.Error("AddTool(nil): want error, got nil")
	}
	if err := r.AddToolHook(nil); err == nil {
		t.Error("AddToolHook(nil): want error, got nil")
	}
	if err := r.AddTransform(nil); err == nil {
		t.Error("AddTransform(nil): want error, got nil")
	}
	if err := r.AddSubscriber(nil); err == nil {
		t.Error("AddSubscriber(nil): want error, got nil")
	}
	if err := r.AddCommand(nil); err == nil {
		t.Error("AddCommand(nil): want error, got nil")
	}
	if err := r.AddProvider(nil); err == nil {
		t.Error("AddProvider(nil): want error, got nil")
	}
}

func TestRegistry_AddAfterFreezeFails(t *testing.T) {
	r := NewRegistry()
	r.Freeze()

	if err := r.AddTool(fakeTool{name: "t"}); err != ErrFrozen {
		t.Errorf("AddTool after Freeze: got %v, want ErrFrozen", err)
	}
	if err := r.AddToolHook(fakeToolHook{}); err != ErrFrozen {
		t.Errorf("AddToolHook after Freeze: got %v, want ErrFrozen", err)
	}
	if err := r.AddTransform(fakeTransform{id: "a"}); err != ErrFrozen {
		t.Errorf("AddTransform after Freeze: got %v, want ErrFrozen", err)
	}
	if err := r.AddSubscriber(fakeSubscriber{}); err != ErrFrozen {
		t.Errorf("AddSubscriber after Freeze: got %v, want ErrFrozen", err)
	}
	if err := r.AddCommand(fakeCommand{name: "c"}); err != ErrFrozen {
		t.Errorf("AddCommand after Freeze: got %v, want ErrFrozen", err)
	}
	if err := r.AddKeybind(Keybind{Mode: "normal", Key: "g", Command: "c"}); err != ErrFrozen {
		t.Errorf("AddKeybind after Freeze: got %v, want ErrFrozen", err)
	}
	if err := r.AddProvider(fakeProvider{typ: "p"}); err != ErrFrozen {
		t.Errorf("AddProvider after Freeze: got %v, want ErrFrozen", err)
	}
}

func TestRegistry_FreezeTwiceReturnsEquivalentView(t *testing.T) {
	r := NewRegistry()
	if err := r.AddTool(fakeTool{name: "bash"}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	v1 := r.Freeze()
	v2 := r.Freeze()

	if len(v1.Tools()) != len(v2.Tools()) {
		t.Fatalf("v1.Tools() has %d entries, v2.Tools() has %d", len(v1.Tools()), len(v2.Tools()))
	}
	if v1.Tools()[0].Name() != v2.Tools()[0].Name() {
		t.Errorf("v1 and v2 disagree on tool name: %q vs %q", v1.Tools()[0].Name(), v2.Tools()[0].Name())
	}
}

func TestView_TransformsSortedByPriorityStable(t *testing.T) {
	r := NewRegistry()
	order := []struct {
		id       string
		priority int
	}{
		{"a", 30},
		{"b", 10},
		{"c", 30},
		{"d", 20},
	}
	for _, o := range order {
		if err := r.AddTransform(fakeTransform{id: o.id, priority: o.priority}); err != nil {
			t.Fatalf("AddTransform(%s): %v", o.id, err)
		}
	}

	v := r.Freeze()
	got := v.Transforms()
	if len(got) != 4 {
		t.Fatalf("got %d transforms, want 4", len(got))
	}

	wantIDs := []string{"b", "d", "a", "c"} // 10, 20, 30(first registered), 30(second registered)
	for i, w := range wantIDs {
		ft, ok := got[i].(fakeTransform)
		if !ok {
			t.Fatalf("got[%d] is %T, want fakeTransform", i, got[i])
		}
		if ft.id != w {
			t.Errorf("got[%d].id = %q, want %q", i, ft.id, w)
		}
	}
}

func TestView_ToolsSortedByName(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"write", "bash", "edit", "glob"} {
		if err := r.AddTool(fakeTool{name: name}); err != nil {
			t.Fatalf("AddTool(%s): %v", name, err)
		}
	}

	v := r.Freeze()
	got := v.Tools()
	if len(got) != 4 {
		t.Fatalf("got %d tools, want 4", len(got))
	}
	want := []string{"bash", "edit", "glob", "write"}
	for i, w := range want {
		if got[i].Name() != w {
			t.Errorf("got[%d].Name() = %q, want %q", i, got[i].Name(), w)
		}
	}

	tool, ok := v.Tool("bash")
	if !ok {
		t.Fatal(`Tool("bash"): not found`)
	}
	if tool.Name() != "bash" {
		t.Errorf("Tool(%q).Name() = %q", "bash", tool.Name())
	}
	if _, ok := v.Tool("missing"); ok {
		t.Error(`Tool("missing"): want not found`)
	}
}

func TestView_KeybindLaterWins(t *testing.T) {
	r := NewRegistry()
	if err := r.AddKeybind(Keybind{Mode: "normal", Key: "g", Command: "goto-top"}); err != nil {
		t.Fatalf("AddKeybind #1: %v", err)
	}
	if err := r.AddKeybind(Keybind{Mode: "normal", Key: "q", Command: "quit"}); err != nil {
		t.Fatalf("AddKeybind #2: %v", err)
	}
	if err := r.AddKeybind(Keybind{Mode: "normal", Key: "g", Command: "goto-bottom"}); err != nil {
		t.Fatalf("AddKeybind #3: %v", err)
	}

	v := r.Freeze()
	got := v.Keybinds()
	if len(got) != 2 {
		t.Fatalf("got %d keybinds, want 2", len(got))
	}

	var found bool
	for _, k := range got {
		if k.Mode == "normal" && k.Key == "g" {
			found = true
			if k.Command != "goto-bottom" {
				t.Errorf("Keybind{normal,g}.Command = %q, want %q", k.Command, "goto-bottom")
			}
		}
	}
	if !found {
		t.Fatal("keybind {normal, g} not found")
	}
}

func TestRegistry_DuplicateCommandRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.AddCommand(fakeCommand{name: "help"}); err != nil {
		t.Fatalf("AddCommand #1: %v", err)
	}
	if err := r.AddCommand(fakeCommand{name: "help"}); err == nil {
		t.Fatal("AddCommand #2 with duplicate name: want error, got nil")
	}
}

func TestRegistry_DuplicateProviderTypeRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.AddProvider(fakeProvider{typ: "anthropic"}); err != nil {
		t.Fatalf("AddProvider #1: %v", err)
	}
	if err := r.AddProvider(fakeProvider{typ: "anthropic"}); err == nil {
		t.Fatal("AddProvider #2 with duplicate type: want error, got nil")
	}

	v := r.Freeze()
	p, ok := v.Provider("anthropic")
	if !ok {
		t.Fatal(`Provider("anthropic"): not found`)
	}
	if p.Type() != "anthropic" {
		t.Errorf("Provider(%q).Type() = %q", "anthropic", p.Type())
	}
	if _, ok := v.Provider("missing"); ok {
		t.Error(`Provider("missing"): want not found`)
	}
}

func TestView_ToolHooksSubscribersCommandsRegistrationOrder(t *testing.T) {
	r := NewRegistry()
	if err := r.AddToolHook(fakeToolHook{}); err != nil {
		t.Fatalf("AddToolHook: %v", err)
	}
	if err := r.AddToolHook(fakeToolHook{}); err != nil {
		t.Fatalf("AddToolHook: %v", err)
	}
	if err := r.AddSubscriber(fakeSubscriber{}); err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	for _, name := range []string{"z", "a", "m"} {
		if err := r.AddCommand(fakeCommand{name: name}); err != nil {
			t.Fatalf("AddCommand(%s): %v", name, err)
		}
	}

	v := r.Freeze()
	if got := len(v.ToolHooks()); got != 2 {
		t.Errorf("len(ToolHooks()) = %d, want 2", got)
	}
	if got := len(v.Subscribers()); got != 1 {
		t.Errorf("len(Subscribers()) = %d, want 1", got)
	}
	gotCmds := v.Commands()
	if len(gotCmds) != 3 {
		t.Fatalf("len(Commands()) = %d, want 3", len(gotCmds))
	}
	wantOrder := []string{"z", "a", "m"}
	for i, w := range wantOrder {
		if gotCmds[i].Name() != w {
			t.Errorf("Commands()[%d].Name() = %q, want %q (registration order)", i, gotCmds[i].Name(), w)
		}
	}
}

func TestView_ReturnedSlicesAreCopies(t *testing.T) {
	r := NewRegistry()
	if err := r.AddTool(fakeTool{name: "bash"}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	v := r.Freeze()

	got := v.Tools()
	got[0] = fakeTool{name: "mutated"}

	got2 := v.Tools()
	if got2[0].Name() != "bash" {
		t.Errorf("mutating a returned slice affected the View: got2[0].Name() = %q, want %q", got2[0].Name(), "bash")
	}
}

// fakeToolSource is a minimal ToolSource stub for tests.
type fakeToolSource struct{ tools []Tool }

func (f fakeToolSource) Tools() []Tool { return f.tools }

func TestRegistry_SetToolSource(t *testing.T) {
	t.Run("nil is rejected", func(t *testing.T) {
		r := NewRegistry()
		if err := r.SetToolSource(nil); err == nil {
			t.Fatal("SetToolSource(nil): want error, got nil")
		}
	})

	t.Run("a second call is rejected", func(t *testing.T) {
		r := NewRegistry()
		if err := r.SetToolSource(fakeToolSource{}); err != nil {
			t.Fatalf("SetToolSource #1: %v", err)
		}
		if err := r.SetToolSource(fakeToolSource{}); err == nil {
			t.Fatal("SetToolSource #2: want error, got nil")
		}
	})

	t.Run("after Freeze returns ErrFrozen", func(t *testing.T) {
		r := NewRegistry()
		r.Freeze()
		if err := r.SetToolSource(fakeToolSource{}); err != ErrFrozen {
			t.Errorf("SetToolSource after Freeze: got %v, want ErrFrozen", err)
		}
	})

	t.Run("View.ToolSource returns the source", func(t *testing.T) {
		r := NewRegistry()
		src := fakeToolSource{tools: []Tool{fakeTool{name: "mcp__s__t"}}}
		if err := r.SetToolSource(src); err != nil {
			t.Fatalf("SetToolSource: %v", err)
		}
		v := r.Freeze()
		got := v.ToolSource()
		if got == nil {
			t.Fatal("View.ToolSource(): got nil")
		}
		if len(got.Tools()) != 1 || got.Tools()[0].Name() != "mcp__s__t" {
			t.Errorf("View.ToolSource().Tools() = %+v", got.Tools())
		}
	})

	t.Run("with no source, View.ToolSource returns nil", func(t *testing.T) {
		r := NewRegistry()
		v := r.Freeze()
		if got := v.ToolSource(); got != nil {
			t.Errorf("View.ToolSource() = %v, want nil", got)
		}
	})
}

func TestView_ConcurrentReads(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"write", "bash", "edit", "glob"} {
		if err := r.AddTool(fakeTool{name: name}); err != nil {
			t.Fatalf("AddTool(%s): %v", name, err)
		}
	}
	if err := r.AddTransform(fakeTransform{id: "a", priority: 1}); err != nil {
		t.Fatalf("AddTransform: %v", err)
	}
	if err := r.AddKeybind(Keybind{Mode: "normal", Key: "g", Command: "goto-top"}); err != nil {
		t.Fatalf("AddKeybind: %v", err)
	}

	v := r.Freeze()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tools := v.Tools()
			if len(tools) != 4 {
				t.Errorf("len(Tools()) = %d, want 4", len(tools))
			}
			_ = v.Transforms()
			_ = v.Keybinds()
			_, _ = v.Tool("bash")
		}()
	}
	wg.Wait()
}
