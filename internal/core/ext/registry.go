package ext

import (
	"fmt"
	"sort"
)

// Registry collects extension points during startup. It is not safe for
// concurrent Add* calls: startup runs on a single goroutine. Once Freeze
// is called, every Add* method returns ErrFrozen; callers read the
// registry's contents through the View Freeze returns instead.
type Registry struct {
	frozen bool

	tools     []Tool
	toolNames map[string]int // Name() -> index into tools

	toolHooks []ToolHook

	transforms []ContextTransform

	subscribers []EventSubscriber

	commands     []Command
	commandNames map[string]int // Name() -> index into commands

	keybinds   []Keybind
	keybindIdx map[[2]string]int // {Mode, Key} -> index into keybinds

	providers   []ProviderFactory
	providerIdx map[string]int // Type() -> index into providers

	toolSource ToolSource
}

// NewRegistry returns an empty Registry ready for Add* calls.
func NewRegistry() *Registry {
	return &Registry{
		toolNames:    make(map[string]int),
		commandNames: make(map[string]int),
		keybindIdx:   make(map[[2]string]int),
		providerIdx:  make(map[string]int),
	}
}

// AddTool registers t. It returns an error if t is nil, r is frozen, or a
// tool with the same Name is already registered.
func (r *Registry) AddTool(t Tool) error {
	if r.frozen {
		return ErrFrozen
	}
	if t == nil {
		return fmt.Errorf("ext: AddTool: tool is nil")
	}
	name := t.Name()
	if _, ok := r.toolNames[name]; ok {
		return fmt.Errorf("ext: AddTool: duplicate tool name %q", name)
	}
	r.toolNames[name] = len(r.tools)
	r.tools = append(r.tools, t)
	return nil
}

// AddToolHook registers h. It returns an error if h is nil or r is frozen.
func (r *Registry) AddToolHook(h ToolHook) error {
	if r.frozen {
		return ErrFrozen
	}
	if h == nil {
		return fmt.Errorf("ext: AddToolHook: hook is nil")
	}
	r.toolHooks = append(r.toolHooks, h)
	return nil
}

// AddTransform registers t. It returns an error if t is nil or r is frozen.
func (r *Registry) AddTransform(t ContextTransform) error {
	if r.frozen {
		return ErrFrozen
	}
	if t == nil {
		return fmt.Errorf("ext: AddTransform: transform is nil")
	}
	r.transforms = append(r.transforms, t)
	return nil
}

// AddSubscriber registers s. It returns an error if s is nil or r is
// frozen.
func (r *Registry) AddSubscriber(s EventSubscriber) error {
	if r.frozen {
		return ErrFrozen
	}
	if s == nil {
		return fmt.Errorf("ext: AddSubscriber: subscriber is nil")
	}
	r.subscribers = append(r.subscribers, s)
	return nil
}

// AddCommand registers c. It returns an error if c is nil, r is frozen, or
// a command with the same Name is already registered.
func (r *Registry) AddCommand(c Command) error {
	if r.frozen {
		return ErrFrozen
	}
	if c == nil {
		return fmt.Errorf("ext: AddCommand: command is nil")
	}
	name := c.Name()
	if _, ok := r.commandNames[name]; ok {
		return fmt.Errorf("ext: AddCommand: duplicate command name %q", name)
	}
	r.commandNames[name] = len(r.commands)
	r.commands = append(r.commands, c)
	return nil
}

// AddKeybind registers k. When k shares its Mode and Key with a
// previously registered Keybind, k replaces it in place (the
// later-registered Keybind wins), keeping that slot's original position.
func (r *Registry) AddKeybind(k Keybind) error {
	if r.frozen {
		return ErrFrozen
	}
	key := [2]string{k.Mode, k.Key}
	if idx, ok := r.keybindIdx[key]; ok {
		r.keybinds[idx] = k
		return nil
	}
	r.keybindIdx[key] = len(r.keybinds)
	r.keybinds = append(r.keybinds, k)
	return nil
}

// AddProvider registers p. It returns an error if p is nil, r is frozen,
// or a provider with the same Type is already registered.
func (r *Registry) AddProvider(p ProviderFactory) error {
	if r.frozen {
		return ErrFrozen
	}
	if p == nil {
		return fmt.Errorf("ext: AddProvider: provider is nil")
	}
	typ := p.Type()
	if _, ok := r.providerIdx[typ]; ok {
		return fmt.Errorf("ext: AddProvider: duplicate provider type %q", typ)
	}
	r.providerIdx[typ] = len(r.providers)
	r.providers = append(r.providers, p)
	return nil
}

// SetToolSource registers s as the registry's one live ToolSource. It
// returns an error if s is nil, r is frozen, or a source is already set.
func (r *Registry) SetToolSource(s ToolSource) error {
	if r.frozen {
		return ErrFrozen
	}
	if s == nil {
		return fmt.Errorf("ext: SetToolSource: source is nil")
	}
	if r.toolSource != nil {
		return fmt.Errorf("ext: SetToolSource: a ToolSource is already set")
	}
	r.toolSource = s
	return nil
}

// Freeze marks r as frozen, so future Add* calls return ErrFrozen, and
// returns a View: an immutable snapshot of r's contents at this moment.
// Calling Freeze again returns an equivalent View.
func (r *Registry) Freeze() View {
	r.frozen = true
	return newView(r)
}

// View is a read-only, frozen snapshot of a Registry, taken at Freeze
// time. Every method returns a fresh copy of its backing slice, so callers
// cannot mutate the snapshot. View is safe for concurrent reads.
type View struct {
	tools   []Tool
	toolIdx map[string]int

	toolHooks []ToolHook

	transforms []ContextTransform

	subscribers []EventSubscriber

	commands []Command

	keybinds []Keybind

	providers   []ProviderFactory
	providerIdx map[string]int

	toolSource ToolSource
}

// newView copies r's contents into a View, sorting tools by Name and
// transforms by ascending Priority (stably, so equal priorities keep their
// registration order).
func newView(r *Registry) View {
	tools := append([]Tool(nil), r.tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name() < tools[j].Name() })
	toolIdx := make(map[string]int, len(tools))
	for i, t := range tools {
		toolIdx[t.Name()] = i
	}

	transforms := append([]ContextTransform(nil), r.transforms...)
	sort.SliceStable(transforms, func(i, j int) bool { return transforms[i].Priority() < transforms[j].Priority() })

	providers := append([]ProviderFactory(nil), r.providers...)
	providerIdx := make(map[string]int, len(providers))
	for i, p := range providers {
		providerIdx[p.Type()] = i
	}

	return View{
		tools:       tools,
		toolIdx:     toolIdx,
		toolHooks:   append([]ToolHook(nil), r.toolHooks...),
		transforms:  transforms,
		subscribers: append([]EventSubscriber(nil), r.subscribers...),
		commands:    append([]Command(nil), r.commands...),
		keybinds:    append([]Keybind(nil), r.keybinds...),
		providers:   providers,
		providerIdx: providerIdx,
		toolSource:  r.toolSource,
	}
}

// Tools returns every registered Tool, sorted by Name.
func (v View) Tools() []Tool {
	out := make([]Tool, len(v.tools))
	copy(out, v.tools)
	return out
}

// Tool returns the Tool registered under name, if any.
func (v View) Tool(name string) (Tool, bool) {
	i, ok := v.toolIdx[name]
	if !ok {
		return nil, false
	}
	return v.tools[i], true
}

// ToolHooks returns every registered ToolHook, in registration order.
func (v View) ToolHooks() []ToolHook {
	out := make([]ToolHook, len(v.toolHooks))
	copy(out, v.toolHooks)
	return out
}

// Transforms returns every registered ContextTransform, stable-sorted by
// ascending Priority.
func (v View) Transforms() []ContextTransform {
	out := make([]ContextTransform, len(v.transforms))
	copy(out, v.transforms)
	return out
}

// Subscribers returns every registered EventSubscriber, in registration
// order.
func (v View) Subscribers() []EventSubscriber {
	out := make([]EventSubscriber, len(v.subscribers))
	copy(out, v.subscribers)
	return out
}

// Commands returns every registered Command, in registration order.
func (v View) Commands() []Command {
	out := make([]Command, len(v.commands))
	copy(out, v.commands)
	return out
}

// Keybinds returns every registered Keybind, in registration order, with
// later-registered Keybinds for the same Mode+Key already applied.
func (v View) Keybinds() []Keybind {
	out := make([]Keybind, len(v.keybinds))
	copy(out, v.keybinds)
	return out
}

// Provider returns the ProviderFactory registered under typ, if any.
func (v View) Provider(typ string) (ProviderFactory, bool) {
	i, ok := v.providerIdx[typ]
	if !ok {
		return nil, false
	}
	return v.providers[i], true
}

// ToolSource returns the registry's one live ToolSource, or nil when none
// was registered.
func (v View) ToolSource() ToolSource {
	return v.toolSource
}
