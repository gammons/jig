package llm

import (
	"fmt"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// Catalog looks up providers and models by ref. *catalog.Catalog satisfies
// it.
type Catalog interface {
	Provider(id string) (core.ProviderInfo, bool)
	Model(ref core.ModelRef) (core.ModelInfo, bool)
}

// Source resolves a core.ModelRef into a credentialed core.LLM, dispatching
// to the ext.ProviderFactory registered for the provider's Type.
type Source struct {
	cat       Catalog
	reg       ext.View
	providers map[string]core.ProviderConfig
	getenv    func(string) string
}

// NewSource returns a Source that resolves models against cat, builds
// clients through the factories registered in reg, overlays providers
// (user config, keyed by provider ID) onto the catalog's credentials, and
// reads credential environment variables through getenv.
func NewSource(cat Catalog, reg ext.View, providers map[string]core.ProviderConfig, getenv func(string) string) *Source {
	return &Source{cat: cat, reg: reg, providers: providers, getenv: getenv}
}

// For resolves ref into a core.LLM and its core.ModelInfo. It fails if the
// provider or model is unknown, if no API key is configured or set in the
// environment for a provider that requires one, or if no factory is
// registered for the provider's Type.
func (s *Source) For(ref core.ModelRef) (core.LLM, core.ModelInfo, error) {
	info, ok := s.cat.Provider(ref.Provider)
	if !ok {
		return nil, core.ModelInfo{}, fmt.Errorf("unknown provider %q (see: jig models)", ref.Provider)
	}
	model, ok := s.cat.Model(ref)
	if !ok {
		return nil, core.ModelInfo{}, fmt.Errorf("unknown model %q (see: jig models %s)", ref.String(), ref.Provider)
	}
	info.Endpoint = s.resolveEndpoint(info.Endpoint)

	cfg := s.providers[ref.Provider]
	apiKey, err := s.resolveAPIKey(ref.Provider, info, cfg)
	if err != nil {
		return nil, core.ModelInfo{}, err
	}
	cfg.APIKey = apiKey

	factory, ok := s.reg.Provider(info.Type)
	if !ok {
		return nil, core.ModelInfo{}, fmt.Errorf("provider type %q is not supported", info.Type)
	}

	client, err := factory.New(info, cfg, ref.Model)
	if err != nil {
		return nil, core.ModelInfo{}, err
	}
	return client, model, nil
}

// resolveEndpoint substitutes a catwalk-style "$ENV_VAR" placeholder
// endpoint (e.g. "$ANTHROPIC_API_ENDPOINT", used by catwalk's built-in
// anthropic/openai/gemini entries) with that environment variable's value
// via s.getenv. If the variable is unset, it resolves to "" so the
// factory falls back to the SDK's own default base URL instead of trying
// to dial a literal "$ANTHROPIC_API_ENDPOINT" host. An endpoint with no
// leading "$" (a real URL, or already empty) passes through unchanged.
func (s *Source) resolveEndpoint(endpoint string) string {
	name, ok := strings.CutPrefix(endpoint, "$")
	if !ok {
		return endpoint
	}
	return s.getenv(name)
}

// resolveAPIKey returns cfg.APIKey if set, else the value of
// info.APIKeyEnv from the environment. It errors only when a key is
// required (info.APIKeyEnv is non-empty) but neither source has one; a
// provider with no APIKeyEnv (e.g. a local server) is allowed through with
// no key at all.
func (s *Source) resolveAPIKey(providerID string, info core.ProviderInfo, cfg core.ProviderConfig) (string, error) {
	if cfg.APIKey != "" {
		return cfg.APIKey, nil
	}
	if info.APIKeyEnv == "" {
		return "", nil
	}
	if key := s.getenv(info.APIKeyEnv); key != "" {
		return key, nil
	}
	return "", fmt.Errorf(
		"no credentials for provider %q: set %s or providers.%s.api_key in config.toml",
		providerID, info.APIKeyEnv, providerID,
	)
}
