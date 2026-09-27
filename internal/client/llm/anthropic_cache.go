package llm

import (
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
)

// ephemeralCacheType is the Anthropic cache_control "type" value for a
// short-lived (5-minute) cache breakpoint.
const ephemeralCacheType = "ephemeral"

// cacheLastMessages is how many trailing messages get an ephemeral cache
// breakpoint, per Anthropic's recommended prompt-caching layout.
const cacheLastMessages = 2

// anthropicCacheOptions returns the fantasy.ProviderOptions that mark a
// message or part with an ephemeral Anthropic cache breakpoint.
func anthropicCacheOptions() fantasy.ProviderOptions {
	return anthropic.NewProviderCacheControlOptions(&anthropic.ProviderCacheControlOptions{
		CacheControl: anthropic.CacheControl{Type: ephemeralCacheType},
	})
}

// applyAnthropicCacheToCall is the adapter-level prepare hook anthropicFactory
// wires into every core.LLM it builds, keyed on the factory's Type (so a
// custom provider ID of type "anthropic" gets it too, and no other
// provider type ever does).
func applyAnthropicCacheToCall(call *fantasy.Call) {
	applyAnthropicCache(call.Prompt)
}

// applyAnthropicCache marks messages for Anthropic's ephemeral prompt
// caching: the last part of a leading system message, and the last
// cacheLastMessages messages overall (message-level, so it can land on a
// system, user, or tool message alike).
func applyAnthropicCache(messages []fantasy.Message) {
	if len(messages) == 0 {
		return
	}
	if messages[0].Role == fantasy.MessageRoleSystem {
		cacheLastPart(&messages[0])
	}
	start := len(messages) - cacheLastMessages
	if start < 0 {
		start = 0
	}
	for i := start; i < len(messages); i++ {
		messages[i].ProviderOptions = anthropicCacheOptions()
	}
}

// cacheLastPart marks m's last content part with an ephemeral cache
// breakpoint, if that part is a TextPart.
func cacheLastPart(m *fantasy.Message) {
	if len(m.Content) == 0 {
		return
	}
	i := len(m.Content) - 1
	text, ok := m.Content[i].(fantasy.TextPart)
	if !ok {
		return
	}
	text.ProviderOptions = anthropicCacheOptions()
	m.Content[i] = text
}
