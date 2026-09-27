package agent

import (
	"context"
	"fmt"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// buildRequest assembles one step's LLMRequest from the session history and
// the run's allowed tools, then applies every ContextTransform in order.
func (r *Runner) buildRequest(ctx context.Context, rc ext.RunContext, st *run) (core.LLMRequest, error) {
	history, err := r.d.History.History(ctx, rc.SessionID)
	if err != nil {
		return core.LLMRequest{}, fmt.Errorf("agent: loading history: %w", err)
	}
	req := core.LLMRequest{
		Model:           rc.Model,
		Messages:        history,
		Tools:           toolSpecs(st.allowed),
		MaxOutputTokens: st.info.DefaultMaxTokens,
	}
	for _, t := range r.d.Ext.Transforms() {
		if err := t.Transform(ctx, rc, &req); err != nil {
			return core.LLMRequest{}, fmt.Errorf("agent: context transform: %w", err)
		}
	}
	return req, nil
}

func toolSpecs(tools []ext.Tool) []core.ToolSpec {
	specs := make([]core.ToolSpec, 0, len(tools))
	for _, t := range tools {
		specs = append(specs, core.ToolSpec{Name: t.Name(), Description: t.Description(), Schema: t.Schema()})
	}
	return specs
}
