package agent

import (
	"context"
	"encoding/json"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// consume streams one attempt of req into msg. It reports whether any
// stream event arrived, which decides whether the error may be retried.
func (r *Runner) consume(ctx context.Context, st *run, req core.LLMRequest, msg *core.Message) (bool, error) {
	received := false
	for ev, err := range st.llm.Stream(ctx, req) {
		if err != nil {
			return received, err
		}
		received = true
		if err := r.apply(ctx, msg, ev); err != nil {
			return received, err
		}
	}
	return received, ctx.Err()
}

// apply folds one stream event into msg, publishing deltas and saving msg
// when its first tool call arrives.
func (r *Runner) apply(ctx context.Context, msg *core.Message, ev core.StreamEvent) error {
	base := event.Base{SessionID: msg.SessionID}
	switch ev.Kind {
	case core.StreamText:
		appendDelta(msg, core.PartText, ev.Text)
		r.d.Bus.Publish(event.TextDelta{Base: base, MessageID: msg.ID, Text: ev.Text})
	case core.StreamReasoning:
		appendDelta(msg, core.PartReasoning, ev.Text)
		r.d.Bus.Publish(event.ReasoningDelta{Base: base, MessageID: msg.ID, Text: ev.Text})
	case core.StreamToolCall:
		if ev.Call == nil {
			return nil
		}
		first := !hasToolCalls(*msg)
		call := *ev.Call
		call.Input = storableInput(call.Input)
		msg.Parts = append(msg.Parts, core.Part{Kind: core.PartToolCall, Call: &call})
		if first {
			return r.d.Store.SaveMessage(ctx, *msg)
		}
	case core.StreamFinish:
		msg.Usage = ev.Usage
	}
	return nil
}

// storableInput returns input unchanged when it is empty or valid JSON, and
// otherwise (e.g. arguments truncated at the token limit) as a JSON string
// holding the raw text, so the call can still be saved and execute answers
// it with an invalid-input error.
func storableInput(input json.RawMessage) json.RawMessage {
	if len(input) == 0 || json.Valid(input) {
		return input
	}
	b, err := json.Marshal(string(input))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return b
}

// appendDelta appends text to msg's last part when it is of kind, and
// starts a new part otherwise.
func appendDelta(msg *core.Message, kind core.PartKind, text string) {
	if n := len(msg.Parts); n > 0 && msg.Parts[n-1].Kind == kind {
		msg.Parts[n-1].Text += text
		return
	}
	msg.Parts = append(msg.Parts, core.Part{Kind: kind, Text: text})
}
