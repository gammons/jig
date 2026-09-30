// Package agent runs the streaming agent loop: one user turn in, a sequence
// of model steps (each one assistant message, with any tool calls executed
// and their results attached) out.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/ids"
)

// defaultMaxSteps bounds a run when the agent sets no MaxSteps.
const defaultMaxSteps = 100

// LLMSource resolves a model to its client and metadata.
type LLMSource interface {
	For(core.ModelRef) (core.LLM, core.ModelInfo, error)
}

// MessageStore persists messages.
type MessageStore interface {
	SaveMessage(ctx context.Context, m core.Message) error
}

// History returns the messages the model should see for a session.
type History interface {
	History(ctx context.Context, id core.SessionID) ([]core.Message, error)
}

// Deps are the Runner's collaborators.
type Deps struct {
	LLMs     LLMSource
	Ext      ext.View
	Store    MessageStore
	History  History
	Bus      event.Publisher
	Clock    clock.Clock
	IDs      *ids.Gen
	ToolsFor func(core.Agent, []ext.Tool) []ext.Tool
	Log      *slog.Logger // debug log; nil discards
}

// Runner drives agent turns, at most one at a time per session.
type Runner struct {
	d       Deps
	mu      sync.Mutex
	running map[core.SessionID]context.CancelFunc
	last    time.Time // latest CreatedAt handed out, for strict ordering
}

// NewRunner returns a Runner over d.
func NewRunner(d Deps) *Runner {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Runner{d: d, running: make(map[core.SessionID]context.CancelFunc)}
}

// run is the per-Run state shared by the step helpers.
type run struct {
	rc       ext.RunContext
	llm      core.LLM
	info     core.ModelInfo
	allowed  []ext.Tool
	maxSteps int
}

// Run appends userText (and any attachments, in order) to rc.SessionID's
// history and drives the model until it stops calling tools, max steps is
// reached, the run fails, or it is cancelled. It returns the last
// assistant message.
func (r *Runner) Run(ctx context.Context, rc ext.RunContext, userText string, atts ...core.Attachment) (_ core.Message, err error) {
	ctx, release, err := r.register(ctx, rc.SessionID)
	if err != nil {
		return core.Message{}, err
	}
	defer release()
	ctx = withRunLog(ctx, rc)
	rs := &runStats{started: r.d.Clock.Now()}
	defer func() { logRunEnd(ctx, r.d.Log, r.d.Clock.Now(), rs, err) }()

	user := r.newMessage(rc, core.RoleUser)
	user.Parts = append([]core.Part{{Kind: core.PartText, Text: userText}}, attachmentParts(atts)...)
	user.Status = core.StatusComplete
	if err := r.d.Store.SaveMessage(ctx, user); err != nil {
		return core.Message{}, r.failed(rc.SessionID, rc.RootID, err)
	}

	llm, info, err := r.d.LLMs.For(rc.Model)
	if err != nil {
		return core.Message{}, r.failed(rc.SessionID, rc.RootID, err)
	}
	st := &run{rc: rc, llm: llm, info: info, allowed: r.allowedTools(rc.Agent), maxSteps: rc.Agent.MaxSteps}
	if st.maxSteps <= 0 {
		st.maxSteps = defaultMaxSteps
	}
	debug(ctx, r.d.Log, "run", "run start", "model", rc.Model.String(), "max_steps", st.maxSteps,
		"effort", string(rc.Effort), "effort_sent", string(core.EffectiveEffort(info, rc.Effort)))

	var last core.Message
	for n := 1; n <= st.maxSteps; n++ {
		rs.steps = n
		msg, err := r.step(ctx, st, n)
		if err != nil {
			return msg, err
		}
		last = msg
		rs.usage = addUsage(rs.usage, msg.Usage)
		rs.cost += msg.CostUSD
		if !hasToolCalls(msg) {
			break
		}
		if n == st.maxSteps {
			if last, err = r.stopAtMaxSteps(ctx, st, last); err != nil {
				return last, err
			}
			rs.maxed = true
		}
	}
	return r.finish(rc, last, rs.usage, rs.cost), nil
}

// Exclusive runs fn while holding id's slot in the running map: a Run on id
// returns core.ErrBusy while fn runs, and Exclusive itself returns
// core.ErrBusy if a Run (or another Exclusive) already holds id. Cancel(id)
// cancels fn's ctx, same as it would a Run.
func (r *Runner) Exclusive(ctx context.Context, id core.SessionID, fn func(context.Context) error) error {
	ctx, release, err := r.register(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	return fn(ctx)
}

// Cancel cancels the running turn for id, if any.
func (r *Runner) Cancel(id core.SessionID) {
	r.mu.Lock()
	cancel := r.running[id]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// register claims id for one run, returning a ctx that Cancel(id) or a
// parent cancel will cancel, and a release func that frees id.
func (r *Runner) register(ctx context.Context, id core.SessionID) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.running[id]; busy {
		return nil, nil, core.ErrBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	r.running[id] = cancel
	return ctx, func() {
		r.mu.Lock()
		delete(r.running, id)
		r.mu.Unlock()
		cancel()
	}, nil
}

// step runs one model request into one assistant message, executes any
// tool calls it made, and saves it.
func (r *Runner) step(ctx context.Context, st *run, n int) (out core.Message, err error) {
	var tm stepTiming
	var streamDur, toolsDur time.Duration
	defer func() { logStepEnd(ctx, r.d.Log, n, out, &tm, streamDur, toolsDur) }()

	msg := r.newMessage(st.rc, core.RoleAssistant)
	msg.Status = core.StatusStreaming
	r.d.Bus.Publish(event.MessageStarted{
		Base: event.Base{SessionID: st.rc.SessionID, RootID: st.rc.RootID}, MessageID: msg.ID, Agent: msg.Agent, Model: msg.Model,
	})
	rc := st.rc
	rc.MessageID = msg.ID

	req, err := r.buildRequest(ctx, rc, st)
	if err != nil {
		return r.abort(ctx, msg, rc.RootID, err)
	}
	debug(ctx, r.d.Log, "step", "step start", "step", n, "messages", len(req.Messages), "tools", len(req.Tools))
	streamStart := r.d.Clock.Now()
	streamErr := r.stream(ctx, st, req, &msg, &tm)
	streamDur = r.d.Clock.Now().Sub(streamStart)
	if streamErr != nil {
		return r.abort(ctx, msg, rc.RootID, streamErr)
	}
	msg.CostUSD = st.info.Cost(msg.Usage)

	if calls := toolCalls(msg); len(calls) > 0 {
		toolsStart := r.d.Clock.Now()
		for _, res := range r.execute(ctx, rc, st.allowed, calls) {
			msg.Parts = append(msg.Parts, core.Part{Kind: core.PartToolResult, Result: &res})
		}
		toolsDur = r.d.Clock.Now().Sub(toolsStart)
		if err := ctx.Err(); err != nil {
			return r.abort(ctx, msg, rc.RootID, err)
		}
	}
	msg.Status = core.StatusComplete
	if err := r.d.Store.SaveMessage(ctx, msg); err != nil {
		return r.abort(ctx, msg, rc.RootID, err)
	}
	r.d.Bus.Publish(event.StepFinished{
		Base:      event.Base{SessionID: rc.SessionID, RootID: rc.RootID},
		MessageID: msg.ID,
		Usage:     msg.Usage,
		CostUSD:   msg.CostUSD,
	})
	return msg, nil
}

// stopAtMaxSteps appends the max-steps notice to last, publishes it as a
// TextDelta on last (after a "\n" when last already has text, matching how
// text parts are joined), and saves it.
func (r *Runner) stopAtMaxSteps(ctx context.Context, st *run, last core.Message) (core.Message, error) {
	notice := fmt.Sprintf("[stopped: reached max_steps (%d)]", st.maxSteps)
	delta := notice
	if hasText(last) {
		delta = "\n" + notice
	}
	last.Parts = append(last.Parts, core.Part{Kind: core.PartText, Text: notice})
	r.d.Bus.Publish(event.TextDelta{Base: event.Base{SessionID: last.SessionID, RootID: st.rc.RootID}, MessageID: last.ID, Text: delta})
	if err := r.d.Store.SaveMessage(ctx, last); err != nil {
		return r.abort(ctx, last, st.rc.RootID, err)
	}
	return last, nil
}

// hasText reports whether m has a non-empty text part.
func hasText(m core.Message) bool {
	for _, p := range m.Parts {
		if p.Kind == core.PartText && p.Text != "" {
			return true
		}
	}
	return false
}

// abort ends a run on err: an interrupted message (with every unanswered
// tool call answered "cancelled") when ctx is done, a failed one otherwise.
// It saves msg, publishes RunFailed, and returns the error to surface.
func (r *Runner) abort(ctx context.Context, msg core.Message, rootID core.SessionID, err error) (core.Message, error) {
	saveCtx := context.WithoutCancel(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		msg.Status = core.StatusInterrupted
		msg.Parts = answerPending(msg.Parts)
		_ = r.d.Store.SaveMessage(saveCtx, msg)
		r.d.Bus.Publish(event.RunFailed{Base: event.Base{SessionID: msg.SessionID, RootID: rootID}, Err: "cancelled"})
		return msg, ctxErr
	}
	msg.Status = core.StatusFailed
	_ = r.d.Store.SaveMessage(saveCtx, msg)
	return msg, r.failed(msg.SessionID, rootID, err)
}

// failed publishes RunFailed for err and returns err.
func (r *Runner) failed(sid, rootID core.SessionID, err error) error {
	r.d.Bus.Publish(event.RunFailed{Base: event.Base{SessionID: sid, RootID: rootID}, Err: err.Error()})
	return err
}

// finish publishes RunFinished with the run's summed usage and cost.
func (r *Runner) finish(rc ext.RunContext, last core.Message, usage core.Usage, cost float64) core.Message {
	r.d.Bus.Publish(event.RunFinished{
		Base: event.Base{SessionID: rc.SessionID, RootID: rc.RootID}, MessageID: last.ID, Usage: usage, CostUSD: cost,
	})
	return last
}

func (r *Runner) newMessage(rc ext.RunContext, role core.Role) core.Message {
	return core.Message{
		ID:        core.MessageID(r.d.IDs.Next("msg")),
		SessionID: rc.SessionID,
		Role:      role,
		Agent:     rc.Agent.Name,
		Model:     rc.Model.String(),
		CreatedAt: r.stamp(),
	}
}

// stamp returns a CreatedAt strictly after every one it returned before, at
// the store's millisecond resolution, so history order matches creation
// order even when the clock does not advance between messages.
func (r *Runner) stamp() time.Time {
	now := r.d.Clock.Now().Truncate(time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !now.After(r.last) {
		now = r.last.Add(time.Millisecond)
	}
	r.last = now
	return now
}

func (r *Runner) allowedTools(a core.Agent) []ext.Tool {
	all := r.d.Ext.Tools()
	if r.d.ToolsFor == nil {
		return all
	}
	return r.d.ToolsFor(a, all)
}

func addUsage(a, b core.Usage) core.Usage {
	return core.Usage{
		Input:      a.Input + b.Input,
		Output:     a.Output + b.Output,
		CacheRead:  a.CacheRead + b.CacheRead,
		CacheWrite: a.CacheWrite + b.CacheWrite,
	}
}

// attachmentParts converts atts into one PartAttachment per attachment, in
// order.
func attachmentParts(atts []core.Attachment) []core.Part {
	parts := make([]core.Part, len(atts))
	for i := range atts {
		parts[i] = core.Part{Kind: core.PartAttachment, Attachment: &atts[i]}
	}
	return parts
}

func toolCalls(m core.Message) []core.ToolCall {
	var calls []core.ToolCall
	for _, p := range m.Parts {
		if p.Kind == core.PartToolCall && p.Call != nil {
			calls = append(calls, *p.Call)
		}
	}
	return calls
}

func hasToolCalls(m core.Message) bool {
	return len(toolCalls(m)) > 0
}

// answerPending appends a "cancelled" error result for every tool call in
// parts that has no result yet.
func answerPending(parts []core.Part) []core.Part {
	answered := make(map[string]bool)
	for _, p := range parts {
		if p.Kind == core.PartToolResult && p.Result != nil {
			answered[p.Result.CallID] = true
		}
	}
	for _, p := range parts {
		if p.Kind != core.PartToolCall || p.Call == nil || answered[p.Call.ID] {
			continue
		}
		parts = append(parts, core.Part{Kind: core.PartToolResult, Result: &core.ToolResult{
			CallID: p.Call.ID, Name: p.Call.Name, Output: cancelledOutput, IsError: true,
		}})
	}
	return parts
}
