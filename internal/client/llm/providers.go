package llm

import (
	"cmp"
	"context"
	"fmt"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// Factories returns the ext.ProviderFactory for every fantasy-backed
// provider type jig supports out of the box.
func Factories() []ext.ProviderFactory {
	return []ext.ProviderFactory{
		anthropicFactory{},
		openaiFactory{},
		openaiCompatFactory{},
		openrouterFactory{},
		googleFactory{},
	}
}

// newModel resolves model against p and wraps the result as a core.LLM.
// prepare, if non-nil, is wired into the returned adapter as its
// call-mutation hook (see adapter.prepare).
func newModel(p fantasy.Provider, model string, prepare func(*fantasy.Call)) (core.LLM, error) {
	lm, err := p.LanguageModel(context.Background(), model)
	if err != nil {
		return nil, fmt.Errorf("llm: resolving model %q: %w", model, err)
	}
	return &adapter{lm: lm, prepare: prepare}, nil
}

// baseURL returns cfg's base URL override, falling back to info's catalog
// endpoint.
func baseURL(info core.ProviderInfo, cfg core.ProviderConfig) string {
	return cmp.Or(cfg.BaseURL, info.Endpoint)
}

type anthropicFactory struct{}

func (anthropicFactory) Type() string { return "anthropic" }

func (anthropicFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []anthropic.Option{anthropic.WithAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, anthropic.WithBaseURL(url))
	}
	p, err := anthropic.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: anthropic: %w", err)
	}
	// Anthropic prompt caching is wired here, keyed on this factory's Type
	// ("anthropic"), not on info.ID: a custom-ID provider that is still
	// type "anthropic" (e.g. a proxy) gets it too.
	return newModel(p, model, applyAnthropicCacheToCall)
}

type openaiFactory struct{}

func (openaiFactory) Type() string { return "openai" }

func (openaiFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []openai.Option{openai.WithAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, openai.WithBaseURL(url))
	}
	p, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: openai: %w", err)
	}
	return newModel(p, model, nil)
}

type openaiCompatFactory struct{}

func (openaiCompatFactory) Type() string { return "openai-compat" }

func (openaiCompatFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []openaicompat.Option{openaicompat.WithAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, openaicompat.WithBaseURL(url))
	}
	p, err := openaicompat.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: openai-compat: %w", err)
	}
	return newModel(p, model, nil)
}

// openrouterFactory has no base-URL override: fantasy's openrouter package
// exposes no WithBaseURL option, always pointing at openrouter.DefaultURL.
type openrouterFactory struct{}

func (openrouterFactory) Type() string { return "openrouter" }

func (openrouterFactory) New(_ core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	p, err := openrouter.New(openrouter.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("llm: openrouter: %w", err)
	}
	return newModel(p, model, nil)
}

type googleFactory struct{}

func (googleFactory) Type() string { return "google" }

func (googleFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []google.Option{google.WithGeminiAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, google.WithBaseURL(url))
	}
	p, err := google.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: google: %w", err)
	}
	return newModel(p, model, nil)
}
