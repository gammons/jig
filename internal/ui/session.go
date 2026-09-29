package ui

import (
	"context"
	"slices"
	"time"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/transcript"
)

// runState is the root's run, gated so a run-end event can only ever
// belong to the current send. A send is in flight (inFlight) until
// Chat.Send returns — after the Runner has released the session — and
// cancel cancels its context. If its run started, the send stays
// unsettled (awaitEnd) until the run's RunFinished/RunFailed arrives
// (ended), since the bus may deliver it after Send's return; the next
// send waits for both. started records the current run's MessageStarted
// (a RunFinished always follows one; an earlier RunFinished is stale).
// running is what the UI shows. userID and text are the send's user
// block and prompt text, to undo a send that never ran. frame is the
// spinner frame, advanced by streamTick. cancelledAt is when an INSERT
// ctrl+c last cancelled this run (sender.ctrlC's grace period).
type runState struct {
	running     bool
	inFlight    bool
	started     bool
	ended       bool
	awaitEnd    bool
	startedAt   time.Time
	cancelledAt time.Time
	cancel      context.CancelFunc
	userID      transcript.BlockID
	text        string
	frame       int
}

// busy reports whether a new send must wait (be queued).
func (r *runState) busy() bool { return r.inFlight || r.awaitEnd }

// end folds the root's RunFinished (finished) or RunFailed into the run.
// An end that can't be the current send's is ignored. It reports whether
// the send just settled (Send had already returned), freeing the queue.
func (r *runState) end(finished bool) bool {
	switch {
	case r.awaitEnd:
	case r.inFlight && !r.ended && (r.started || !finished):
	default:
		return false
	}
	r.ended, r.running = true, false
	settled := r.awaitEnd
	r.awaitEnd = false
	return settled
}

// returned records Chat.Send returning; ran says whether its run started.
// It reports whether the send is settled (no run end still to come).
func (r *runState) returned(ran bool) bool {
	if r.cancel != nil {
		r.cancel()
	}
	r.inFlight, r.cancel = false, nil
	if ran && !r.ended {
		r.awaitEnd = true
		return false
	}
	r.running = false
	return true
}

// blockTimes measures root blocks by ID, on the App clock: when each tool
// call (R22) or reasoning block started, and the duration of each
// finished one. Loaded blocks have none.
type blockTimes struct {
	starts map[transcript.BlockID]time.Time
	durs   map[transcript.BlockID]time.Duration
}

// catalog caches the read-only port lists the App consults per frame, and
// the configured default model (the fallback for an agent without one).
type catalog struct {
	agents       []core.Agent
	providers    []core.ProviderStatus
	defaultModel string
}

// idSet is an insertion-ordered set of block IDs. Blocks are created in
// event order, so a set of new blocks upserts in display order.
type idSet struct {
	order []transcript.BlockID
	has   map[transcript.BlockID]bool
}

func (s *idSet) add(id transcript.BlockID) {
	if s.has == nil {
		s.has = map[transcript.BlockID]bool{}
	}
	if !s.has[id] {
		s.has[id] = true
		s.order = append(s.order, id)
	}
}

// take returns the IDs in insertion order and empties the set.
func (s *idSet) take() []transcript.BlockID {
	out := s.order
	*s = idSet{}
	return out
}

// sessionState reconciles the root session's display state: its
// projection, the session itself (info.Agent/Model are the agent and
// model the next send uses), the run, the queue, the last step's usage,
// the summed cost, todos, tool durations, the blocklist item versions it
// has issued, dirty streaming blocks (rendered on the next streamTick),
// and live blocks (running tools/subagents, whose spinner each tick
// advances). model is the model the last root step reported. attach
// holds the paths the file picker inserted as @mentions; a send attaches
// those whose token survives in its text.
type sessionState struct {
	attach   []string
	proj     *transcript.Projection
	info     core.Session
	model    string
	run      runState
	queued   bool
	usage    core.Usage
	cost     float64
	todos    []core.Todo
	times    blockTimes
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
	cat      catalog
	clk      clock.Clock
}

// newSessionState starts with root id ("" for a session the first send
// will create) and the configured default model.
func newSessionState(id core.SessionID, clk clock.Clock, defaultModel string) *sessionState {
	s := &sessionState{
		proj: transcript.New(id), info: core.Session{ID: id}, clk: clk,
		versions: map[transcript.BlockID]int{}, cat: catalog{defaultModel: defaultModel},
	}
	s.resetBlocks()
	return s
}

// resetBlocks clears the per-block state a new projection invalidates.
func (s *sessionState) resetBlocks() {
	s.times = blockTimes{starts: map[transcript.BlockID]time.Time{}, durs: map[transcript.BlockID]time.Duration{}}
	s.dirty = idSet{}
	s.live = map[transcript.BlockID]bool{}
}

// applyResult is what the App must do after sessionState.apply: re-render
// upsert now, rebuild the whole list (reload), or send the queue now the
// previous send has settled (settled).
type applyResult struct {
	upsert  []transcript.BlockID
	reload  bool
	settled bool
}

// apply folds one bus event into the state. Deltas only mark their blocks
// dirty (streamTick renders them); every other change is returned for an
// immediate render, preceded by the dirty blocks so new blocks keep their
// order.
func (s *sessionState) apply(ev event.Event) applyResult {
	if e, ok := ev.(event.SessionCreated); ok {
		if e.RootID != e.SessionID || e.Info.ParentID != "" || !s.adopt(withID(e.Info, e.SessionID)) {
			return applyResult{}
		}
		return applyResult{reload: true}
	}
	if s.info.ID == "" || ev.Root() != s.info.ID {
		return applyResult{}
	}
	ids := s.proj.Apply(ev)
	if ev.Session() != s.info.ID {
		return applyResult{upsert: s.withDirty(ids)}
	}
	s.times.thinking(s.proj, ev, ids, s.clk.Now())
	switch e := ev.(type) {
	case event.TextDelta, event.ReasoningDelta:
		for _, id := range ids {
			s.dirty.add(id)
		}
		return applyResult{}
	case event.MessageStarted:
		s.run.started = s.run.started || s.run.inFlight
		if e.Model != "" {
			s.model = e.Model
		}
	case event.ToolCallStarted, event.ToolCallFinished:
		s.timeTools(ev, ids)
	case event.StepFinished:
		s.usage = e.Usage
		s.cost += e.CostUSD
	case event.RunFinished, event.RunFailed:
		_, finished := ev.(event.RunFinished)
		settled := s.run.end(finished)
		return applyResult{upsert: s.withDirty(ids), settled: settled}
	case event.SessionUpdated:
		s.info = withID(e.Info, s.info.ID)
	case event.TodosUpdated:
		s.todos = slices.Clone(e.Todos)
	}
	if len(ids) == 0 {
		return applyResult{}
	}
	return applyResult{upsert: s.withDirty(ids)}
}

// withDirty prepends the dirty blocks (emptying the set) to ids, without
// repeating any.
func (s *sessionState) withDirty(ids []transcript.BlockID) []transcript.BlockID {
	if len(s.dirty.order) == 0 {
		return ids
	}
	out := s.dirty.take()
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// timeTools records a root tool call's start, or its duration on finish.
func (s *sessionState) timeTools(ev event.Event, ids []transcript.BlockID) {
	now := s.clk.Now()
	for _, id := range ids {
		if _, ok := ev.(event.ToolCallStarted); ok {
			s.times.starts[id] = now
			continue
		}
		if start, ok := s.times.starts[id]; ok {
			s.times.durs[id] = now.Sub(start)
			delete(s.times.starts, id)
		}
	}
}

// thinking runs after every root event: it records when a reasoning
// block starts thinking (its first delta) and, once an event ends that
// thinking (text or a tool call follows, the step or run ends), its
// duration. It is measured at the event, not the next streamTick, so the
// tick interval never pads it. The block itself stays live until a tick
// re-renders it (sessionState.item), which picks the duration up.
func (t blockTimes) thinking(proj *transcript.Projection, ev event.Event, ids []transcript.BlockID, now time.Time) {
	if _, ok := ev.(event.ReasoningDelta); ok {
		for _, id := range ids {
			if _, started := t.starts[id]; started {
				continue
			}
			if b, ok := proj.Block(id); ok && b.Thinking {
				t.starts[id] = now
			}
		}
	}
	for id, start := range t.starts {
		b, ok := proj.Block(id)
		if !ok || b.Kind != transcript.KindReasoning || b.Thinking {
			continue
		}
		t.durs[id] = now.Sub(start)
		delete(t.starts, id)
	}
}

// adopt makes info (a new session, from its SessionCreated or from the
// Send result, whichever arrives first) the root, if a send is in flight
// with no root yet, and reports whether it did. The live user blocks
// move to a projection of the new root; the agent and model chosen before
// the send are kept unless the session names its own.
func (s *sessionState) adopt(info core.Session) bool {
	if s.info.ID != "" || !s.run.inFlight || info.ID == "" {
		return false
	}
	old := s.proj
	s.proj = transcript.New(info.ID)
	for _, b := range old.Blocks() {
		if b.Kind == transcript.KindUser {
			id := s.proj.AddUser(b.Text, b.Attachments)
			if b.ID == s.run.userID {
				s.run.userID = id
			}
		}
	}
	prev := s.info
	s.info = info
	if s.info.Agent == "" {
		s.info.Agent = prev.Agent
	}
	if s.info.Model == "" {
		s.info.Model = prev.Model
	}
	s.resetBlocks()
	return true
}

// withID returns info with its ID forced to id.
func withID(info core.Session, id core.SessionID) core.Session {
	info.ID = id
	return info
}

// startRun marks a send of text (shown as block userID) in flight under
// a context cancel cancels.
func (s *sessionState) startRun(userID transcript.BlockID, text string, cancel context.CancelFunc) {
	s.run = runState{
		running: true, inFlight: true, startedAt: s.clk.Now(),
		cancel: cancel, userID: userID, text: text, frame: s.run.frame,
	}
}

// endSend records that Chat.Send returned; ran says whether its run
// started. It reports whether the send is settled, and the dirty blocks
// to render when it is.
func (s *sessionState) endSend(ran bool) (bool, []transcript.BlockID) {
	if !s.run.returned(ran) {
		return false, nil
	}
	return true, s.dirty.take()
}

// dropUser removes the block of a send that never ran and returns every
// remaining block's item at its current version (nothing re-renders).
func (s *sessionState) dropUser(id transcript.BlockID) []blocklist.Item {
	s.proj.DropUser(id)
	delete(s.versions, id)
	blocks := s.proj.Blocks()
	out := make([]blocklist.Item, len(blocks))
	for i, b := range blocks {
		out[i] = blocklist.Item{
			ID:        string(b.ID),
			Version:   s.versions[b.ID],
			Data:      s.data(b),
			HalfEdges: b.Kind == transcript.KindUser,
		}
	}
	return out
}

// load replaces the projection's blocks with msgs, the root's stored
// history, and recomputes the summed cost and last-step usage from it.
// Only call it while idle: a mid-step Load drops unsaved blocks.
func (s *sessionState) load(info core.Session, msgs []core.Message, todos []core.Todo) {
	agent := s.info.Agent
	s.info = withID(info, s.info.ID)
	if s.info.Agent == "" {
		s.info.Agent = agent
	}
	s.proj.Load(msgs)
	s.resetBlocks()
	s.cost, s.usage, s.model = 0, core.Usage{}, ""
	for _, m := range msgs {
		s.cost += m.CostUSD
		if m.Role == core.RoleAssistant {
			s.usage, s.model = m.Usage, m.Model
		}
	}
	s.todos = slices.Clone(todos)
}

// tick advances the spinner and returns the blocks to re-render: every
// dirty one, then every live one.
func (s *sessionState) tick() []transcript.BlockID {
	s.run.frame++
	ids := s.dirty.take()
	live := make([]transcript.BlockID, 0, len(s.live))
	for id := range s.live {
		live = append(live, id)
	}
	slices.Sort(live)
	for _, id := range live {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// items builds the blocklist items for ids, each at a new version, and
// tracks which of them are live.
func (s *sessionState) items(ids []transcript.BlockID) []blocklist.Item {
	out := make([]blocklist.Item, 0, len(ids))
	for _, id := range ids {
		b, ok := s.proj.Block(id)
		if !ok {
			continue
		}
		out = append(out, s.item(b))
	}
	return out
}

// allItems builds an item for every block, in display order.
func (s *sessionState) allItems() []blocklist.Item {
	blocks := s.proj.Blocks()
	out := make([]blocklist.Item, len(blocks))
	for i, b := range blocks {
		out[i] = s.item(b)
	}
	return out
}

// item builds b's blocklist item at its next version. A reasoning block
// the model is still thinking in counts as live (its spinner animates);
// it stays in s.live until a tick re-renders it after thinking ends.
func (s *sessionState) item(b transcript.Block) blocklist.Item {
	s.versions[b.ID]++
	data := s.data(b)
	if isLive(b) || data.Thinking {
		s.live[b.ID] = true
	} else {
		delete(s.live, b.ID)
	}
	// A user block is a panel with ▄/▀ edges (renderUser).
	return blocklist.Item{
		ID:        string(b.ID),
		Version:   s.versions[b.ID],
		Data:      data,
		HalfEdges: b.Kind == transcript.KindUser,
	}
}

// data builds b's blockData from the session's current run state.
func (s *sessionState) data(b transcript.Block) blockData {
	return blockData{
		Block:    b,
		Duration: s.times.durs[b.ID],
		Frame:    s.run.frame,
		Thinking: b.Thinking,
	}
}

// isLive reports whether b shows an animated spinner: a running tool, or
// a subagent not yet in a final state.
func isLive(b transcript.Block) bool {
	switch b.Kind {
	case transcript.KindTool:
		return b.State == transcript.StateRunning
	case transcript.KindSubagent:
		switch b.State {
		case transcript.StateOK, transcript.StateError, transcript.StateDenied, transcript.StateCancelled, transcript.StatePending:
			return false
		}
		return true
	}
	return false
}

// agentIndex returns the index of the current agent in the cached list,
// or -1.
func (s *sessionState) agentIndex() int {
	return slices.IndexFunc(s.cat.agents, func(a core.Agent) bool { return a.Name == s.info.Agent })
}

// cycleAgent moves to the next (delta 1) or previous (-1) primary agent,
// wrapping, and reports whether the agent changed.
func (s *sessionState) cycleAgent(delta int) bool {
	n := len(s.cat.agents)
	if n == 0 {
		return false
	}
	i := s.agentIndex()
	if i < 0 {
		i = 0
		if delta > 0 {
			i = n - 1
		}
	}
	next := s.cat.agents[((i+delta)%n+n)%n].Name
	changed := next != s.info.Agent
	s.info.Agent = next
	return changed
}
