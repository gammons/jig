package catalog

import (
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
)

func TestConvert_PricingMapping(t *testing.T) {
	p := catwalk.Provider{
		ID: "testprov",
		Models: []catwalk.Model{
			{
				ID:                 "model-1",
				Name:               "Model One",
				CostPer1MIn:        3,
				CostPer1MOut:       15,
				CostPer1MInCached:  3.75,
				CostPer1MOutCached: 0.3,
				ContextWindow:      200000,
				DefaultMaxTokens:   8192,
				CanReason:          true,
			},
		},
	}

	infos := convertProviders([]catwalk.Provider{p})
	if len(infos) != 1 || len(infos[0].Models) != 1 {
		t.Fatalf("convertProviders() = %+v, want one provider with one model", infos)
	}
	m := infos[0].Models[0]

	want := core.ModelInfo{
		Ref:              core.ModelRef{Provider: "testprov", Model: "model-1"},
		Name:             "Model One",
		ContextWindow:    200000,
		DefaultMaxTokens: 8192,
		CostIn:           3,
		CostOut:          15,
		CostCacheWrite:   3.75,
		CostCacheRead:    0.3,
		CanReason:        true,
	}
	if m != want {
		t.Errorf("convertModel() = %+v, want %+v", m, want)
	}
}

func TestConvert_APIKeyEnv(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "dollar prefix", key: "$ANTHROPIC_API_KEY", want: "ANTHROPIC_API_KEY"},
		{name: "empty", key: "", want: ""},
		{name: "no dollar sign", key: "ANTHROPIC_API_KEY", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := catwalk.Provider{ID: "testprov", APIKey: tt.key}
			infos := convertProviders([]catwalk.Provider{p})
			if got := infos[0].APIKeyEnv; got != tt.want {
				t.Errorf("APIKeyEnv(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestCustomProviderFromConfig(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	c := New(Options{
		CachePath: filepath.Join(dir, "catalog.json"),
		Clock:     clk,
		Custom: map[string]core.ProviderConfig{
			"ollama": {
				Type:    "openai-compat",
				BaseURL: "http://localhost:11434/v1",
				Models:  []string{"qwen3"},
			},
		},
	})

	m, ok := c.Model(core.ModelRef{Provider: "ollama", Model: "qwen3"})
	if !ok {
		t.Fatal("Model(ollama/qwen3) not found, want it resolvable from custom config")
	}
	if m.Name != "qwen3" {
		t.Errorf("Name = %q, want %q", m.Name, "qwen3")
	}

	p, ok := c.Provider("ollama")
	if !ok {
		t.Fatal("Provider(ollama) not found")
	}
	if p.Name != "ollama" {
		t.Errorf("Provider.Name = %q, want %q", p.Name, "ollama")
	}
	if p.Type != "openai-compat" {
		t.Errorf("Provider.Type = %q, want %q", p.Type, "openai-compat")
	}
	if p.Endpoint != "http://localhost:11434/v1" {
		t.Errorf("Provider.Endpoint = %q, want %q", p.Endpoint, "http://localhost:11434/v1")
	}
}

// TestCustomProviderOverridesExistingCatalogProvider pins the collision
// branch of mergeCustom: when a custom config entry's ID matches a provider
// already in the catalog (here "anthropic", from the embedded fallback),
// the config's BaseURL overrides Endpoint and its models are appended
// alongside the existing ones, but Name/Type are left as the catalog's.
func TestCustomProviderOverridesExistingCatalogProvider(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	base, ok := New(Options{
		CachePath: filepath.Join(dir, "catalog.json"),
		Clock:     clk,
	}).Provider("anthropic")
	if !ok {
		t.Fatal("Provider(anthropic) not found in embedded fallback, want it present as the collision target")
	}
	if len(base.Models) == 0 {
		t.Fatal("embedded anthropic provider has no models, want at least one to check existing models survive the merge")
	}

	c := New(Options{
		CachePath: filepath.Join(dir, "catalog.json"),
		Clock:     clk,
		Custom: map[string]core.ProviderConfig{
			"anthropic": {
				Type:    "should-be-ignored",
				BaseURL: "http://localhost:9999/v1",
				Models:  []string{"custom-model"},
			},
		},
	})

	p, ok := c.Provider("anthropic")
	if !ok {
		t.Fatal("Provider(anthropic) not found after custom override")
	}

	// Endpoint overridden.
	if p.Endpoint != "http://localhost:9999/v1" {
		t.Errorf("Provider.Endpoint = %q, want %q", p.Endpoint, "http://localhost:9999/v1")
	}
	// Name/Type unchanged from the catalog, not taken from the custom config.
	if p.Name != base.Name {
		t.Errorf("Provider.Name = %q, want unchanged %q", p.Name, base.Name)
	}
	if p.Type != base.Type {
		t.Errorf("Provider.Type = %q, want unchanged %q (not the custom config's %q)", p.Type, base.Type, "should-be-ignored")
	}
	// Existing models still present.
	if len(p.Models) != len(base.Models)+1 {
		t.Fatalf("Provider.Models has %d entries, want %d existing + 1 custom", len(p.Models), len(base.Models))
	}
	for _, m := range base.Models {
		if _, ok := c.Model(m.Ref); !ok {
			t.Errorf("existing model %v no longer resolvable after custom override", m.Ref)
		}
	}
	// New model resolvable via Model().
	custom, ok := c.Model(core.ModelRef{Provider: "anthropic", Model: "custom-model"})
	if !ok {
		t.Fatal("Model(anthropic/custom-model) not found, want the appended custom model resolvable")
	}
	if custom.Name != "custom-model" {
		t.Errorf("custom model Name = %q, want %q", custom.Name, "custom-model")
	}
}

func TestConvertModel_SupportsImages(t *testing.T) {
	p := catwalk.Provider{ID: "testprov", Models: []catwalk.Model{
		{ID: "vision", SupportsImages: true},
		{ID: "text"},
	}}
	models := convertProviders([]catwalk.Provider{p})[0].Models
	if !models[0].SupportsImages {
		t.Errorf("vision: SupportsImages = false, want true")
	}
	if models[1].SupportsImages {
		t.Errorf("text: SupportsImages = true, want false")
	}
}

func TestCustomProvider_ImageModels(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(Options{
		CachePath: filepath.Join(t.TempDir(), "catalog.json"),
		Clock:     clk,
		Custom: map[string]core.ProviderConfig{
			"local": {Type: "openai-compat", Models: []string{"see", "blind"}, ImageModels: []string{"see"}},
		},
	})
	for model, want := range map[string]bool{"see": true, "blind": false} {
		m, ok := c.Model(core.ModelRef{Provider: "local", Model: model})
		if !ok {
			t.Fatalf("Model(local/%s) not found", model)
		}
		if m.SupportsImages != want {
			t.Errorf("local/%s SupportsImages = %v, want %v", model, m.SupportsImages, want)
		}
	}
}
