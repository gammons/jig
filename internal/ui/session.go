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

// runState is the root's run. A send is in flight (inFlight) from the
// send until Chat.Send returns — which happens only after the Runner has
// released the session — and cancel cancels its context. running is what
// the UI shows: set on send, cleared by the root's RunFinished/RunFailed
// or when Send returns, whichever comes first. sawEvent records whether
// any run event (a step, a delta, a tool call, the run's end) arrived: a
// send that fails before its run starts publishes none. userID and text
// are the send's user block and prompt text, to undo a send that never
// ran. frame is the spinner frame, advanced by streamTick.
type runState struct {
	running   bool
	inFlight  bool
	startedAt time.Time
	sawEvent  bool
	cancel    context.CancelFunc
	userID    transcript.BlockID
	text      string
	frame     int
}

// toolTimes measures root tool calls (R22) by block: when each started,
// and the duration of each finished one. Loaded blocks have none.
type toolTimes struct {
	starts map[transcript.BlockID]time.Time
	durs   map[transcript.BlockID]time.Duration
}

// catalog caches the read-only port lists the App consults per frame.
type catalog struct {
	agents    []core.Agent
	providers []core.ProviderStatus
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
// advances). model is the model the last root step reported.
type sessionState struct {
	proj     *transcript.Projection
	info     core.Session
	model    string
	run      runState
	queued   bool
	usage    core.Usage
	cost     float64
	todos    []core.Todo
	tools    toolTimes
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
	cat      catalog
	clk      clock.Clock
}

// newSessionState starts with root id ("" for a session the first send
// will create).
func newSessionState(id core.SessionID, clk clock.Clock) *sessionState {
	s := &sessionState{proj: transcript.New(id), info: core.Session{ID: id}, clk: clk, versions: map[transcript.BlockID]int{}}
	s.resetBlocks()
	return s
}

// resetBlocks clears the per-block state a new projection invalidates.
func (s *sessionState) resetBlocks() {
	s.tools = toolTimes{starts: map[transcript.BlockID]time.Time{}, durs: map[transcript.BlockID]time.Duration{}}
	s.dirty = idSet{}
	s.live = map[transcript.BlockID]bool{}
}

// applyResult is what the App must do after sessionState.apply: re-render
// upsert now, or rebuild the whole list (reload).
type applyResult struct {
	upsert []transcript.BlockID
	reload bool
}

// apply folds one bus event into the state. Deltas only mark their blocks
// dirty (streamTick renders them); every other change is returned for an
// immediate render, preceded by the dirty blocks so new blocks keep their
// order.
func (s *sessionState) apply(ev event.Event) applyResult {
	if e, ok := ev.(event.SessionCreated); ok {
		return s.adopt(e)
	}
	if s.info.ID == "" || ev.Root() != s.info.ID {
		return applyResult{}
	}
	ids := s.proj.Apply(ev)
	if ev.Session() != s.info.ID {
		return applyResult{upsert: s.withDirty(ids)}
	}
	if s.run.inFlight && isRunEvent(ev) {
		s.run.sawEvent = true
	}
	switch e := ev.(type) {
	case event.TextDelta, event.ReasoningDelta:
		for _, id := range ids {
			s.dirty.add(id)
		}
		return applyResult{}
	case event.MessageStarted:
		if e.Model != "" {
			s.model = e.Model
		}
	case event.ToolCallStarted, event.ToolCallFinished:
		s.timeTools(ev, ids)
	case event.StepFinished:
		s.usage = e.Usage
		s.cost += e.CostUSD
	case event.RunFinished, event.RunFailed:
		s.run.running = false
		return applyResult{upsert: s.withDirty(ids)}
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

// isRunEvent reports whether ev is evidence that a run actually started.
func isRunEvent(ev event.Event) bool {
	switch ev.(type) {
	case event.MessageStarted, event.TextDelta, event.ReasoningDelta, event.ToolCallStarted,
		event.ToolCallFinished, event.StepFinished, event.RunFinished, event.RunFailed:
		return true
	}
	return false
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
			s.tools.starts[id] = now
			continue
		}
		if start, ok := s.tools.starts[id]; ok {
			s.tools.durs[id] = now.Sub(start)
			delete(s.tools.starts, id)
		}
	}
}

// adopt makes a new session the root (spec: the first SessionCreated
// whose RootID is its own SessionID, arriving while a send is in flight
// with no root yet). The live user blocks move to a projection of the new
// root; the agent and model chosen before the send are kept unless the
// session names its own.
func (s *sessionState) adopt(e event.SessionCreated) applyResult {
	if s.info.ID != "" || !s.run.running || e.RootID != e.SessionID || e.Info.ParentID != "" {
		return applyResult{}
	}
	old := s.proj
	s.proj = transcript.New(e.SessionID)
	for _, b := range old.Blocks() {
		if b.Kind == transcript.KindUser {
			id := s.proj.AddUser(b.Text, b.Attachments)
			if b.ID == s.run.userID {
				s.run.userID = id
			}
		}
	}
	prev := s.info
	s.info = withID(e.Info, e.SessionID)
	if s.info.Agent == "" {
		s.info.Agent = prev.Agent
	}
	if s.info.Model == "" {
		s.info.Model = prev.Model
	}
	s.resetBlocks()
	return applyResult{reload: true}
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

// endSend records that Chat.Send returned: the run is over (the Runner
// has released the session). It returns the dirty blocks to render.
func (s *sessionState) endSend() []transcript.BlockID {
	if s.run.cancel != nil {
		s.run.cancel()
	}
	s.run.inFlight, s.run.running, s.run.cancel = false, false, nil
	return s.dirty.take()
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
			ID:      string(b.ID),
			Version: s.versions[b.ID],
			Data:    blockData{Block: b, Duration: s.tools.durs[b.ID], Frame: s.run.frame},
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

// item builds b's blocklist item at its next version.
func (s *sessionState) item(b transcript.Block) blocklist.Item {
	s.versions[b.ID]++
	if isLive(b) {
		s.live[b.ID] = true
	} else {
		delete(s.live, b.ID)
	}
	return blocklist.Item{
		ID:      string(b.ID),
		Version: s.versions[b.ID],
		Data:    blockData{Block: b, Duration: s.tools.durs[b.ID], Frame: s.run.frame},
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
