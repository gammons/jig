package agent

import (
	"context"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// Complete makes one tool-less request to m with system as the system
// prompt and user as the only message, and returns the streamed text
// joined. Reasoning and tool-call events are ignored. The request carries
// no Model: m is already bound to one by its LLMSource. effort is sent as
// the request's Effort ("" for none).
func Complete(ctx context.Context, m core.LLM, system, user string, effort core.Effort) (string, error) {
	req := core.LLMRequest{
		System: []string{system},
		Effort: effort,
		Messages: []core.Message{{
			Role:   core.RoleUser,
			Parts:  []core.Part{{Kind: core.PartText, Text: user}},
			Status: core.StatusComplete,
		}},
	}
	var b strings.Builder
	for ev, err := range m.Stream(ctx, req) {
		if err != nil {
			return "", err
		}
		if ev.Kind == core.StreamText {
			b.WriteString(ev.Text)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
