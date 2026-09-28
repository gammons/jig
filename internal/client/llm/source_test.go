package llm

import (
	"context"
	"os"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// fakeCatalog is a minimal Catalog for Source tests.
type fakeCatalog struct {
	providers map[string]core.ProviderInfo
	models    map[core.ModelRef]core.ModelInfo
}

func (c fakeCatalog) Provider(id string) (core.ProviderInfo, bool) {
	p, ok := c.providers[id]
	return p, ok
}

func (c fakeCatalog) Model(ref core.ModelRef) (core.ModelInfo, bool) {
	m, ok := c.models[ref]
	return m, ok
}

// fakeFactory is a test ext.ProviderFactory that records the New call it
// receives and returns a nil core.LLM.
type fakeFactory struct {
	typ      string
	gotInfo  core.ProviderInfo
	gotCfg   core.ProviderConfig
	gotModel string
}

func (f *fakeFactory) Type() string { return f.typ }

func (f *fakeFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	f.gotInfo = info
	f.gotCfg = cfg
	f.gotModel = model
	return nil, nil
}

func viewWith(factories ...ext.ProviderFactory) ext.View {
	reg := ext.NewRegistry()
	for _, f := range factories {
		if err := reg.AddProvider(f); err != nil {
			panic(err)
		}
	}
	return reg.Freeze()
}

func noEnv(string) string { return "" }

func TestSource_UnknownProvider(t *testing.T) {
	cat := fakeCatalog{}
	s := NewSource(cat, viewWith(), nil, noEnv, nil)

	_, _, err := s.For(core.ModelRef{Provider: "nope", Model: "m"})

	want := `unknown provider "nope" (see: jig models)`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSource_UnknownModel(t *testing.T) {
	cat := fakeCatalog{providers: map[string]core.ProviderInfo{
		"anthropic": {ID: "anthropic", Type: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
	}}
	s := NewSource(cat, viewWith(), nil, noEnv, nil)

	_, _, err := s.For(core.ModelRef{Provider: "anthropic", Model: "claude-nope"})

	want := `unknown model "anthropic/claude-nope" (see: jig models anthropic)`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSource_MissingCredentialsNamesEnvVar(t *testing.T) {
	ref := core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			"anthropic": {ID: "anthropic", Type: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	s := NewSource(cat, viewWith(), nil, noEnv, nil)

	_, _, err := s.For(ref)

	want := `no credentials for provider "anthropic": set ANTHROPIC_API_KEY or providers.anthropic.api_key in config.toml`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSource_ConfigKeyBeatsEnv(t *testing.T) {
	ref := core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			"anthropic": {ID: "anthropic", Type: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	factory := &fakeFactory{typ: "anthropic"}
	providers := map[string]core.ProviderConfig{"anthropic": {APIKey: "cfg-key"}}
	getenv := func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "env-key"
		}
		return ""
	}
	s := NewSource(cat, viewWith(factory), providers, getenv, nil)

	_, info, err := s.For(ref)
	if err != nil {
		t.Fatalf("For: unexpected error: %v", err)
	}
	if info.Ref != ref {
		t.Errorf("returned ModelInfo.Ref: got %v, want %v", info.Ref, ref)
	}
	if factory.gotCfg.APIKey != "cfg-key" {
		t.Errorf("factory saw APIKey %q, want %q (config beats env)", factory.gotCfg.APIKey, "cfg-key")
	}
	if factory.gotModel != "claude-haiku-4-5" {
		t.Errorf("factory saw model %q, want %q", factory.gotModel, "claude-haiku-4-5")
	}
}

func TestSource_ResolvesEndpointPlaceholderFromEnv_Empty(t *testing.T) {
	ref := core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			// Mirrors catwalk's built-in entry: an unresolved "$ENV_VAR"
			// placeholder, not a real URL.
			"anthropic": {ID: "anthropic", Type: "anthropic", Endpoint: "$ANTHROPIC_API_ENDPOINT"},
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	factory := &fakeFactory{typ: "anthropic"}
	s := NewSource(cat, viewWith(factory), map[string]core.ProviderConfig{"anthropic": {APIKey: "k"}}, noEnv, nil)

	if _, _, err := s.For(ref); err != nil {
		t.Fatalf("For: unexpected error: %v", err)
	}
	if factory.gotInfo.Endpoint != "" {
		t.Errorf("factory saw Endpoint %q, want \"\" (unset env var must resolve to empty, not the literal placeholder)", factory.gotInfo.Endpoint)
	}
}

func TestSource_ResolvesEndpointPlaceholderFromEnv_Set(t *testing.T) {
	ref := core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			"anthropic": {ID: "anthropic", Type: "anthropic", Endpoint: "$ANTHROPIC_API_ENDPOINT"},
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	factory := &fakeFactory{typ: "anthropic"}
	getenv := func(k string) string {
		if k == "ANTHROPIC_API_ENDPOINT" {
			return "https://custom.example.com"
		}
		return ""
	}
	s := NewSource(cat, viewWith(factory), map[string]core.ProviderConfig{"anthropic": {APIKey: "k"}}, getenv, nil)

	if _, _, err := s.For(ref); err != nil {
		t.Fatalf("For: unexpected error: %v", err)
	}
	if factory.gotInfo.Endpoint != "https://custom.example.com" {
		t.Errorf("factory saw Endpoint %q, want %q", factory.gotInfo.Endpoint, "https://custom.example.com")
	}
}

func TestSource_EndpointWithoutPlaceholderPassesThrough(t *testing.T) {
	ref := core.ModelRef{Provider: "myproxy", Model: "m"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			"myproxy": {ID: "myproxy", Type: "openai-compat", Endpoint: "https://proxy.example.com/v1"},
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	factory := &fakeFactory{typ: "openai-compat"}
	s := NewSource(cat, viewWith(factory), nil, noEnv, nil)

	if _, _, err := s.For(ref); err != nil {
		t.Fatalf("For: unexpected error: %v", err)
	}
	if factory.gotInfo.Endpoint != "https://proxy.example.com/v1" {
		t.Errorf("factory saw Endpoint %q, want unchanged %q", factory.gotInfo.Endpoint, "https://proxy.example.com/v1")
	}
}

func TestSource_UnsupportedProviderType(t *testing.T) {
	ref := core.ModelRef{Provider: "myprovider", Model: "m"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			"myprovider": {ID: "myprovider", Type: "carrier-pigeon"},
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	s := NewSource(cat, viewWith(), nil, noEnv, nil)

	_, _, err := s.For(ref)

	want := `provider type "carrier-pigeon" is not supported`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSource_NoAPIKeyEnvAllowsThrough(t *testing.T) {
	ref := core.ModelRef{Provider: "ollama", Model: "llama3"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{
			"ollama": {ID: "ollama", Type: "openai-compat"}, // no APIKeyEnv, no config key
		},
		models: map[core.ModelRef]core.ModelInfo{ref: {Ref: ref}},
	}
	factory := &fakeFactory{typ: "openai-compat"}
	s := NewSource(cat, viewWith(factory), nil, noEnv, nil)

	_, _, err := s.For(ref)
	if err != nil {
		t.Fatalf("For: unexpected error for a keyless local provider: %v", err)
	}
	if factory.gotCfg.APIKey != "" {
		t.Errorf("factory saw APIKey %q, want empty", factory.gotCfg.APIKey)
	}
}

func TestSource_HasCredentials(t *testing.T) {
	cat := fakeCatalog{providers: map[string]core.ProviderInfo{
		"anthropic": {ID: "anthropic", Type: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
		"ollama":    {ID: "ollama", Type: "openai-compat"}, // no key required
	}}

	t.Run("env var set", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "ANTHROPIC_API_KEY" {
				return "env-key"
			}
			return ""
		}
		s := NewSource(cat, viewWith(), nil, getenv, nil)
		if !s.HasCredentials("anthropic") {
			t.Error("HasCredentials(anthropic) = false, want true (env var set)")
		}
	})

	t.Run("no key configured", func(t *testing.T) {
		s := NewSource(cat, viewWith(), nil, noEnv, nil)
		if s.HasCredentials("anthropic") {
			t.Error("HasCredentials(anthropic) = true, want false (no key)")
		}
	})

	t.Run("config key set", func(t *testing.T) {
		s := NewSource(cat, viewWith(), map[string]core.ProviderConfig{"anthropic": {APIKey: "cfg-key"}}, noEnv, nil)
		if !s.HasCredentials("anthropic") {
			t.Error("HasCredentials(anthropic) = false, want true (config key set)")
		}
	})

	t.Run("no key required", func(t *testing.T) {
		s := NewSource(cat, viewWith(), nil, noEnv, nil)
		if !s.HasCredentials("ollama") {
			t.Error("HasCredentials(ollama) = false, want true (no APIKeyEnv)")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		s := NewSource(cat, viewWith(), nil, noEnv, nil)
		if s.HasCredentials("nope") {
			t.Error("HasCredentials(nope) = true, want false (unknown provider)")
		}
	})
}

// TestLive_Anthropic is a real, non-mocked call to the Anthropic API. It is
// skipped unless JIG_LIVE_TESTS=1 and ANTHROPIC_API_KEY are both set.
func TestLive_Anthropic(t *testing.T) {
	if os.Getenv("JIG_LIVE_TESTS") != "1" {
		t.Skip("set JIG_LIVE_TESTS=1 to run live provider tests")
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	factory := anthropicFactory{}
	client, err := factory.New(
		core.ProviderInfo{ID: "anthropic", Type: "anthropic"},
		core.ProviderConfig{APIKey: apiKey},
		"claude-haiku-4-5-20251001",
	)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}

	req := core.LLMRequest{
		Model:    core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5-20251001"},
		Messages: []core.Message{{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "say hi"}}}},
	}

	var gotText bool
	for ev, err := range client.Stream(context.Background(), req) {
		if err != nil {
			t.Fatalf("Stream: unexpected error: %v", err)
		}
		if ev.Kind == core.StreamText && ev.Text != "" {
			gotText = true
		}
	}
	if !gotText {
		t.Error("got no text from the live model")
	}
}
