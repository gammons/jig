package core

import (
	"fmt"
	"strings"
)

// ModelRef identifies a model by provider and model name.
type ModelRef struct {
	Provider string
	Model    string
}

// ParseModelRef parses "provider/model" into a ModelRef, splitting on the
// first "/" so a provider name can never contain "/" but a model name can,
// e.g. "openrouter/anthropic/claude-sonnet-4" ->
// {Provider: "openrouter", Model: "anthropic/claude-sonnet-4"}.
func ParseModelRef(s string) (ModelRef, error) {
	provider, model, ok := strings.Cut(s, "/")
	if !ok || provider == "" || model == "" {
		return ModelRef{}, fmt.Errorf("core: invalid model ref %q, want \"provider/model\"", s)
	}
	return ModelRef{Provider: provider, Model: model}, nil
}

// String returns m in "provider/model" form.
func (m ModelRef) String() string {
	return m.Provider + "/" + m.Model
}

// IsZero reports whether m is the zero value.
func (m ModelRef) IsZero() bool {
	return m == ModelRef{}
}

// ModelInfo describes a model's capabilities and per-1M-token pricing.
type ModelInfo struct {
	Ref              ModelRef
	Name             string
	ContextWindow    int64
	DefaultMaxTokens int64
	CostIn           float64
	CostOut          float64
	CostCacheRead    float64
	CostCacheWrite   float64
	CanReason        bool
	SupportsImages   bool
}

// perMillionTokens is the unit m's Cost* fields are priced per.
const perMillionTokens = 1e6

// Cost returns the USD cost of u under m's per-1M-token prices.
func (m ModelInfo) Cost(u Usage) float64 {
	return float64(u.Input)*m.CostIn/perMillionTokens +
		float64(u.Output)*m.CostOut/perMillionTokens +
		float64(u.CacheRead)*m.CostCacheRead/perMillionTokens +
		float64(u.CacheWrite)*m.CostCacheWrite/perMillionTokens
}

// ProviderInfo describes an LLM provider and the models it offers.
type ProviderInfo struct {
	ID        string
	Name      string
	Type      string
	APIKeyEnv string
	Endpoint  string
	Models    []ModelInfo
}
