package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/gammons/jig/internal/core"
)

// adapterOf builds the adapter the factory of type typ makes.
func adapterOf(t *testing.T, typ string) *adapter {
	t.Helper()
	for _, f := range Factories() {
		if f.Type() != typ {
			continue
		}
		c, err := f.New(core.ProviderInfo{Type: typ, Endpoint: "http://example.invalid"}, core.ProviderConfig{APIKey: "k"}, "m")
		if err != nil {
			t.Fatalf("New(%s): %v", typ, err)
		}
		a, ok := c.(*adapter)
		if !ok {
			t.Fatalf("%s: got %T, want *adapter", typ, c)
		}
		return a
	}
	t.Fatalf("no factory of type %q", typ)
	return nil
}

func prepared(t *testing.T, typ string, effort core.Effort) fantasy.Call {
	t.Helper()
	req := core.LLMRequest{
		Effort:   effort,
		System:   []string{"sys"},
		Messages: []core.Message{{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}}},
	}
	call := ToFantasy(req)
	if a := adapterOf(t, typ); a.prepare != nil {
		a.prepare(&call, req)
	}
	return call
}

func TestPrepare_SetsEffortPerProviderType(t *testing.T) {
	checks := map[string]func(t *testing.T, o fantasy.ProviderOptions){
		"anthropic": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[anthropic.Name].(*anthropic.ProviderOptions)
			if !ok || p.Effort == nil || *p.Effort != anthropic.EffortHigh {
				t.Errorf("anthropic options = %#v, want Effort high", o[anthropic.Name])
			}
		},
		"openai": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[openai.Name].(*openai.ProviderOptions)
			if !ok || p.ReasoningEffort == nil || *p.ReasoningEffort != openai.ReasoningEffortHigh {
				t.Errorf("openai options = %#v, want ReasoningEffort high", o[openai.Name])
			}
		},
		"openai-compat": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[openaicompat.Name].(*openaicompat.ProviderOptions)
			if !ok || p.ReasoningEffort == nil || *p.ReasoningEffort != openai.ReasoningEffortHigh {
				t.Errorf("openai-compat options = %#v, want ReasoningEffort high", o[openaicompat.Name])
			}
		},
		"openrouter": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[openrouter.Name].(*openrouter.ProviderOptions)
			if !ok || p.Reasoning == nil || p.Reasoning.Effort == nil || *p.Reasoning.Effort != openrouter.ReasoningEffortHigh {
				t.Errorf("openrouter options = %#v, want Reasoning.Effort high", o[openrouter.Name])
			}
		},
		"google": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[google.Name].(*google.ProviderOptions)
			if !ok || p.ThinkingConfig == nil || p.ThinkingConfig.ThinkingLevel == nil || *p.ThinkingConfig.ThinkingLevel != "HIGH" ||
				p.ThinkingConfig.IncludeThoughts == nil || !*p.ThinkingConfig.IncludeThoughts {
				t.Errorf("google options = %#v, want ThinkingLevel HIGH with thoughts", o[google.Name])
			}
		},
	}
	for typ, check := range checks {
		t.Run(typ, func(t *testing.T) {
			check(t, prepared(t, typ, core.EffortHigh).ProviderOptions)
			if o := prepared(t, typ, "").ProviderOptions; len(o) != 0 {
				t.Errorf("no effort: ProviderOptions = %#v, want none", o)
			}
		})
	}
}

func TestPrepare_AnthropicKeepsCacheWithEffort(t *testing.T) {
	call := prepared(t, "anthropic", core.EffortHigh)
	if anthropic.GetCacheControl(call.Prompt[len(call.Prompt)-1].ProviderOptions) == nil {
		t.Error("last message lost its cache_control breakpoint")
	}
}

func TestAnthropicFactory_SendsEffortOnStream(t *testing.T) {
	fixture, err := os.ReadFile("testdata/anthropic/text_and_tool.sse")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	c, err := anthropicFactory{}.New(core.ProviderInfo{Type: "anthropic"}, core.ProviderConfig{APIKey: "k", BaseURL: srv.URL}, "claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}
	req := core.LLMRequest{Effort: core.EffortLow, Messages: []core.Message{{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}}}}
	for range c.Stream(context.Background(), req) {
	}
	for _, want := range []string{`"effort":"low"`, `"adaptive"`} {
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("request body lacks %s: %s", want, body)
		}
	}
}
