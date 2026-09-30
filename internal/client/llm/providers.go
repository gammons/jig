package llm

import (
	"cmp"
	"context"
	"fmt"
	"net/http"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// Option configures Factories.
type Option func(*factoryOpts)

type factoryOpts struct {
	hc *http.Client
}

// WithHTTPClient makes every factory's provider send its requests through
// c. A nil c leaves the providers' default HTTP client in place.
func WithHTTPClient(c *http.Client) Option {
	return func(o *factoryOpts) { o.hc = c }
}

// Factories returns the ext.ProviderFactory for every fantasy-backed
// provider type jig supports out of the box.
func Factories(opts ...Option) []ext.ProviderFactory {
	var o factoryOpts
	for _, opt := range opts {
		opt(&o)
	}
	hc := o.hc
	return []ext.ProviderFactory{
		anthropicFactory{hc: hc},
		openaiFactory{hc: hc},
		openaiCompatFactory{hc: hc},
		openrouterFactory{hc: hc},
		googleFactory{hc: hc},
	}
}

// newModel resolves model against p and wraps the result as a core.LLM.
// prepare, if non-nil, is wired into the returned adapter as its
// call-mutation hook (see adapter.prepare).
func newModel(p fantasy.Provider, model string, prepare func(*fantasy.Call, core.LLMRequest)) (core.LLM, error) {
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

type anthropicFactory struct{ hc *http.Client }

func (anthropicFactory) Type() string { return "anthropic" }

func (f anthropicFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []anthropic.Option{anthropic.WithAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, anthropic.WithBaseURL(url))
	}
	if f.hc != nil {
		opts = append(opts, anthropic.WithHTTPClient(f.hc))
	}
	p, err := anthropic.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: anthropic: %w", err)
	}
	// Anthropic prompt caching and effort are wired here, keyed on this
	// factory's Type ("anthropic"), not on info.ID: a custom-ID provider
	// that is still type "anthropic" (e.g. a proxy) gets them too.
	return newModel(p, model, anthropicPrepare)
}

type openaiFactory struct{ hc *http.Client }

func (openaiFactory) Type() string { return "openai" }

func (f openaiFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []openai.Option{openai.WithAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, openai.WithBaseURL(url))
	}
	if f.hc != nil {
		opts = append(opts, openai.WithHTTPClient(f.hc))
	}
	p, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: openai: %w", err)
	}
	return newModel(p, model, openaiPrepare)
}

type openaiCompatFactory struct{ hc *http.Client }

func (openaiCompatFactory) Type() string { return "openai-compat" }

func (f openaiCompatFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []openaicompat.Option{openaicompat.WithAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, openaicompat.WithBaseURL(url))
	}
	if f.hc != nil {
		opts = append(opts, openaicompat.WithHTTPClient(f.hc))
	}
	p, err := openaicompat.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: openai-compat: %w", err)
	}
	return newModel(p, model, openaiCompatPrepare)
}

// openrouterFactory has no base-URL override: fantasy's openrouter package
// exposes no WithBaseURL option, always pointing at openrouter.DefaultURL.
type openrouterFactory struct{ hc *http.Client }

func (openrouterFactory) Type() string { return "openrouter" }

func (f openrouterFactory) New(_ core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []openrouter.Option{openrouter.WithAPIKey(cfg.APIKey)}
	if f.hc != nil {
		opts = append(opts, openrouter.WithHTTPClient(f.hc))
	}
	p, err := openrouter.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: openrouter: %w", err)
	}
	return newModel(p, model, openrouterPrepare)
}

type googleFactory struct{ hc *http.Client }

func (googleFactory) Type() string { return "google" }

func (f googleFactory) New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	opts := []google.Option{google.WithGeminiAPIKey(cfg.APIKey)}
	if url := baseURL(info, cfg); url != "" {
		opts = append(opts, google.WithBaseURL(url))
	}
	if f.hc != nil {
		opts = append(opts, google.WithHTTPClient(f.hc))
	}
	p, err := google.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: google: %w", err)
	}
	return newModel(p, model, googlePrepare)
}
