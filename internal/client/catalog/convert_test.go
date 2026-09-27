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
