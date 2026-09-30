package llm

import (
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/gammons/jig/internal/core"
)

// The prepare hooks below map req.Effort, already clamped to the
// model's levels by the Runner, onto each provider type's call options.
// With no effort they set nothing, leaving the provider's default.

// setProviderOption stores opts under name in call's provider options.
func setProviderOption(call *fantasy.Call, name string, opts fantasy.ProviderOptionsData) {
	if call.ProviderOptions == nil {
		call.ProviderOptions = fantasy.ProviderOptions{}
	}
	call.ProviderOptions[name] = opts
}

// anthropicPrepare applies prompt caching and, with an effort, adaptive
// thinking at that output effort (fantasy asks for summarized thinking
// where a model hides it by default).
func anthropicPrepare(call *fantasy.Call, req core.LLMRequest) {
	applyAnthropicCache(call.Prompt)
	if req.Effort == "" {
		return
	}
	e := anthropic.Effort(req.Effort)
	setProviderOption(call, anthropic.Name, &anthropic.ProviderOptions{Effort: &e})
}

func openaiPrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	e := openai.ReasoningEffort(req.Effort)
	setProviderOption(call, openai.Name, &openai.ProviderOptions{ReasoningEffort: &e})
}

func openaiCompatPrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	e := openai.ReasoningEffort(req.Effort)
	setProviderOption(call, openaicompat.Name, &openaicompat.ProviderOptions{ReasoningEffort: &e})
}

func openrouterPrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	e := openrouter.ReasoningEffort(req.Effort)
	setProviderOption(call, openrouter.Name, &openrouter.ProviderOptions{Reasoning: &openrouter.ReasoningOptions{Effort: &e}})
}

// googlePrepare sets Gemini's thinking level and asks for thought
// summaries, so its reasoning shows like Claude's.
func googlePrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	level := strings.ToUpper(string(req.Effort))
	include := true
	setProviderOption(call, google.Name, &google.ProviderOptions{
		ThinkingConfig: &google.ThinkingConfig{ThinkingLevel: &level, IncludeThoughts: &include},
	})
}
