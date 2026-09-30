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

// blockTimes measures a pane's blocks by ID, on the App clock: when each
// tool call (R22) or reasoning block started, and the duration of each
// finished one. Loaded blocks have none.
type blockTimes struct {
	starts map[transcript.BlockID]time.Time
	durs   map[transcript.BlockID]time.Duration
}

// catalog caches the read-only port lists the App consults per frame, the
// configured default model and effort (the fallbacks for an agent
// without one), and the workdir's git branch (sanitized; refreshed at
// startup and after each run).
type catalog struct {
	agents        []core.Agent
	providers     []core.ProviderStatus
	defaultModel  string
	defaultEffort core.Effort
	branch        string
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

// sessionState reconciles the root session's display state: the session
// itself (info.Agent/Model are the agent and model the next send uses),
// the run, the queue, the last step's usage, the summed cost, todos, and
// the catalog. model is the model the last root step reported. attach
// holds the paths the file picker inserted as @mentions; a send attaches
// those whose token survives in its text. main is the root transcript
// pane: its projection, blocklist, and render bookkeeping.
type sessionState struct {
	attach []string
	main   *pane
	info   core.Session
	model  string
	run    runState
	queued bool
	usage  core.Usage
	cost   float64
	todos  []core.Todo
	cat    catalog
	clk    clock.Clock
}

// newSessionState starts with root id ("" for a session the first send
// will create) and the configured defaults in cat. main is the root
// transcript pane, already built by the caller (its blocklist styled
// from the App's theme).
func newSessionState(id core.SessionID, clk clock.Clock, cat catalog, main *pane) *sessionState {
	return &sessionState{
		main: main, info: core.Session{ID: id}, clk: clk, cat: cat,
	}
}

// applyResult is what the App must do after sessionState.apply: re-render
// upsert now (with relist, by rebuilding the list from the new layout),
// rebuild the whole list (reload), or send the queue now the previous
// send has settled (settled).
type applyResult struct {
	upsert  []transcript.BlockID
	reload  bool
	relist  bool
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
	main := s.main
	ids := main.proj.Apply(ev)
	if ev.Session() != s.info.ID {
		return applyResult{upsert: main.withDirty(ids)}
	}
	main.times.thinking(main.proj, ev, ids, s.clk.Now())
	res := applyResult{relist: regroupsOn(ev) && main.track.regroup(main.proj)}
	switch e := ev.(type) {
	case event.TextDelta, event.ReasoningDelta:
		for _, id := range ids {
			main.track.dirty.add(id)
		}
		return applyResult{}
	case event.MessageStarted:
		s.run.started = s.run.started || s.run.inFlight
		if e.Model != "" {
			s.model = e.Model
		}
	case event.ToolCallStarted, event.ToolCallFinished:
		main.timeTools(ev, ids, s.clk.Now())
	case event.StepFinished:
		s.usage = e.Usage
		s.cost += e.CostUSD
	case event.RunFinished, event.RunFailed:
		_, finished := ev.(event.RunFinished)
		res.settled = s.run.end(finished)
		res.upsert = main.withDirty(ids)
		return res
	case event.SessionUpdated:
		s.info = withID(e.Info, s.info.ID)
	case event.TodosUpdated:
		s.todos = slices.Clone(e.Todos)
	}
	if len(ids) == 0 && !res.relist {
		return applyResult{}
	}
	res.upsert = main.withDirty(ids)
	return res
}

// regroupsOn reports whether ev can change how the root's blocks group:
// a tool call starting or finishing, or a run settling its calls, or a
// permission request holding a group open or releasing it.
func regroupsOn(ev event.Event) bool {
	switch ev.(type) {
	case event.ToolCallStarted, event.ToolCallFinished, event.RunFinished, event.RunFailed,
		event.PermissionRequested, event.PermissionResolved:
		return true
	}
	return false
}

// thinking runs after every root event: it records when a reasoning
// block starts thinking (its first delta) and, once an event ends that
// thinking (text or a tool call follows, the step or run ends), its
// duration. It is measured at the event, not the next streamTick, so the
// tick interval never pads it. The block itself stays live until a tick
// re-renders it (itemTrack.entryItem), which picks the duration up.
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
// move to a projection of the new root; the agent, model, and effort chosen before
// the send are kept unless the session names its own.
func (s *sessionState) adopt(info core.Session) bool {
	if s.info.ID != "" || !s.run.inFlight || info.ID == "" {
		return false
	}
	main := s.main
	old := main.proj
	main.proj = transcript.New(info.ID)
	main.session = info.ID
	for _, b := range old.Blocks() {
		if b.Kind == transcript.KindUser {
			id := main.proj.AddUser(b.Text, b.Attachments)
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
	if s.info.Effort == "" {
		s.info.Effort = prev.Effort
	}
	main.resetBlocks()
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
	return true, s.main.track.dirty.take()
}

// dropUser removes the block of a send that never ran from the root
// pane's projection and returns every remaining item at its current
// version (nothing re-renders).
func (s *sessionState) dropUser(id transcript.BlockID) []blocklist.Item {
	main := s.main
	main.proj.DropUser(id)
	delete(main.track.versions, id)
	entries, _ := main.track.fold.regroup(main.proj.Blocks())
	return main.track.layoutItems(main, entries, bumpNone, s.run.frame)
}

// load replaces the root pane's projection with msgs, the root's stored
// history, and recomputes the summed cost and last-step usage from it.
// Only call it while idle: a mid-step Load drops unsaved blocks.
func (s *sessionState) load(info core.Session, msgs []core.Message, todos []core.Todo) {
	agent := s.info.Agent
	s.info = withID(info, s.info.ID)
	if s.info.Agent == "" {
		s.info.Agent = agent
	}
	s.main.proj.Load(msgs)
	s.main.resetBlocks()
	s.cost, s.usage, s.model = 0, core.Usage{}, ""
	for _, m := range msgs {
		s.cost += m.CostUSD
		if m.Role == core.RoleAssistant {
			s.usage, s.model = m.Usage, m.Model
		}
	}
	s.todos = slices.Clone(todos)
}

// tick advances the spinner and returns the root pane's blocks to
// re-render: every dirty one, then every live one.
func (s *sessionState) tick() []transcript.BlockID {
	s.run.frame++
	main := s.main
	ids := main.track.dirty.take()
	live := make([]transcript.BlockID, 0, len(main.track.live))
	for id := range main.track.live {
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

// items builds, each at a new version, the items that show the blocks
// ids (a member of a collapsed group shows as its header), tracks which
// of them are live, and records the new ones in the fold layout.
func (s *sessionState) items(ids []transcript.BlockID) []blocklist.Item {
	return s.main.track.upsert(s.main, ids, s.run.frame)
}

// allItems regroups the blocks and builds every item of the layout, in
// display order, each at a new version.
func (s *sessionState) allItems() []blocklist.Item {
	main := s.main
	entries, _ := main.track.fold.regroup(main.proj.Blocks())
	return main.track.layoutItems(main, entries, bumpAll, s.run.frame)
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
