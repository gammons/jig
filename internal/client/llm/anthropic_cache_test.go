package llm

import (
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/gammons/jig/internal/core"
)

func TestApplyAnthropicCache_MarksLastSystemPartAndLastMessages(t *testing.T) {
	messages := []fantasy.Message{
		fantasy.NewSystemMessage("s1", "s2"),
		fantasy.NewUserMessage("one"),
		fantasy.NewUserMessage("two"),
		fantasy.NewUserMessage("three"),
	}

	applyAnthropicCache(messages)

	sys := messages[0]
	last := sys.Content[len(sys.Content)-1]
	if cc := anthropic.GetCacheControl(last.Options()); cc == nil || cc.Type != ephemeralCacheType {
		t.Errorf("last system part cache control: got %+v, want ephemeral", cc)
	}
	if first := sys.Content[0]; anthropic.GetCacheControl(first.Options()) != nil {
		t.Errorf("first system part must not carry cache control")
	}

	for i, msg := range messages {
		want := i >= len(messages)-cacheLastMessages
		got := anthropic.GetCacheControl(msg.ProviderOptions) != nil
		if got != want {
			t.Errorf("message %d cache control: got %v, want %v", i, got, want)
		}
	}
}

func TestApplyAnthropicCache_NoMessagesIsNoop(t *testing.T) {
	// Must not panic on an empty prompt.
	applyAnthropicCache(nil)
}

func TestApplyAnthropicCache_LeadingNonSystemMessageGetsNoPartCache(t *testing.T) {
	messages := []fantasy.Message{fantasy.NewUserMessage("hi")}

	applyAnthropicCache(messages)

	// The single message still gets message-level caching (it's within the
	// last cacheLastMessages), but there is no system message to mark at
	// the part level.
	if cc := anthropic.GetCacheControl(messages[0].ProviderOptions); cc == nil {
		t.Error("sole message must still get message-level cache control")
	}
}

func TestAnthropicPrepare_MutatesCallPrompt(t *testing.T) {
	call := fantasy.Call{Prompt: []fantasy.Message{
		fantasy.NewSystemMessage("s1"),
		fantasy.NewUserMessage("hi"),
	}}

	anthropicPrepare(&call, core.LLMRequest{})

	last := call.Prompt[len(call.Prompt)-1]
	if anthropic.GetCacheControl(last.ProviderOptions) == nil {
		t.Error("anthropicPrepare must mark the call's trailing messages")
	}
}
