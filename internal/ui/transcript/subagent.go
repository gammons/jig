package transcript

import (
	"slices"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// lineage maps every known descendant session to the root subagent block
// that owns it, so a grandchild's events update the block of the root task
// call that started its subtree.
type lineage struct {
	owner map[core.SessionID]BlockID
	agent map[core.SessionID]string // the agent each descendant runs
}

func newLineage() lineage {
	return lineage{owner: make(map[core.SessionID]BlockID), agent: make(map[core.SessionID]string)}
}

// spawn records e's child: a child of root is owned by the block of the
// task call that spawned it (e.CallID, R5), and a deeper child by its
// parent's owner. A spawn under an unknown parent is ignored. Only a
// root spawn changes a block: it sets the child (and, when given, the agent
// and description) of an existing subagent block, so a resumed task updates
// its block rather than duplicating it.
func (g *lineage) spawn(l *blockList, root core.SessionID, e event.SubagentSpawned) []BlockID {
	id := toolBlockID(e.CallID)
	if e.Session() != root {
		var ok bool
		if id, ok = g.owner[e.Session()]; !ok {
			return nil
		}
	}
	g.owner[e.Child] = id
	if e.Agent != "" {
		g.agent[e.Child] = e.Agent
	}
	b, ok := l.get(id)
	if e.Session() != root || !ok || b.Sub == nil {
		return nil
	}
	b.Sub.Child = e.Child
	if e.Agent != "" {
		b.Sub.Agent = e.Agent
	}
	if e.Model != "" {
		b.Sub.Model = e.Model
	}
	if e.Description != "" {
		b.Sub.Description = e.Description
	}
	b.Version++
	return []BlockID{b.ID}
}

// seed records the children loaded subagent blocks name; a later block
// wins, since a resumed task reuses its child.
func (g *lineage) seed(l *blockList) {
	for _, b := range l.blocks {
		if b.Sub != nil && b.Sub.Child != "" {
			g.owner[b.Sub.Child] = b.ID
			if b.Sub.Agent != "" {
				g.agent[b.Sub.Child] = b.Sub.Agent
			}
		}
	}
}

// owned returns the subagent block that owns session s, or nil.
func (g *lineage) owned(l *blockList, s core.SessionID) *Block {
	id, ok := g.owner[s]
	if !ok {
		return nil
	}
	b, ok := l.get(id)
	if !ok || b.Sub == nil {
		return nil
	}
	return b
}

// descendant updates the owner block of a descendant session's tool call:
// a start counts it and names it Current, a finish clears Current. Nothing
// else a descendant publishes changes a block.
func (g *lineage) descendant(l *blockList, ev event.Event) []BlockID {
	b := g.owned(l, ev.Session())
	if b == nil {
		return nil
	}
	switch e := ev.(type) {
	case event.ToolCallStarted:
		b.Sub.Tools++
		b.Sub.Current = e.Call.Name
	case event.ToolCallFinished:
		b.Sub.Current = ""
	default:
		return nil
	}
	b.Version++
	return []BlockID{b.ID}
}

// permissions is the list of unresolved permission requests. A block shows
// its earliest unresolved request in Permission.
type permissions struct {
	pending []PendingPermission // request order
}

// request lists e and attaches it to its block: the tool block of the call
// for a root request, the owner subagent block for a descendant's. A
// request with no block is still listed (Block ""), so it can be answered.
func (ps *permissions) request(l *blockList, g *lineage, root core.SessionID, e event.PermissionRequested) []BlockID {
	pp := PendingPermission{
		RequestID: e.RequestID, Session: e.Session(), Tool: e.Tool, Subject: e.Subject, Call: cloneCall(e.Call),
	}
	var b *Block
	if e.Session() == root {
		b, _ = l.get(toolBlockID(e.Call.ID))
	} else if b = g.owned(l, e.Session()); b != nil {
		pp.Subagent = b.Sub.Agent
	}
	if a := g.agent[e.Session()]; a != "" {
		pp.Subagent = a
	}
	if b != nil {
		pp.Block = b.ID
	}
	ps.pending = append(ps.pending, pp)
	if b == nil || b.Permission != nil {
		return nil
	}
	b.show(&pp)
	b.Version++
	return []BlockID{b.ID}
}

// resolve drops request id and, if its block was showing it, shows the
// block's next unresolved request or, if none, clears it.
func (ps *permissions) resolve(l *blockList, id string) []BlockID {
	i := slices.IndexFunc(ps.pending, func(pp PendingPermission) bool { return pp.RequestID == id })
	if i < 0 {
		return nil
	}
	blk := ps.pending[i].Block
	ps.pending = slices.Delete(ps.pending, i, i+1)
	b, ok := l.get(blk)
	if blk == "" || !ok || b.Permission == nil || b.Permission.RequestID != id {
		return nil
	}
	b.show(ps.next(blk))
	b.Version++
	return []BlockID{b.ID}
}

// reattach shows each block's earliest unresolved request again after
// Load rebuilt the blocks.
func (ps *permissions) reattach(l *blockList) {
	for _, pp := range ps.pending {
		if b, ok := l.get(pp.Block); ok && pp.Block != "" && b.Permission == nil {
			b.show(&pp)
		}
	}
}

// next returns the earliest unresolved request for block id, or nil.
func (ps *permissions) next(id BlockID) *PendingPermission {
	for i := range ps.pending {
		if ps.pending[i].Block == id {
			return &ps.pending[i]
		}
	}
	return nil
}

// show sets (a copy of) pp as b's permission, moving a running block to
// awaiting; a nil pp clears it, moving an awaiting block back to running.
func (b *Block) show(pp *PendingPermission) {
	if pp == nil {
		b.Permission = nil
		if b.State == StateAwaiting {
			b.State = StateRunning
		}
		return
	}
	c := *pp
	c.Call = cloneCall(c.Call)
	b.Permission = &c
	if b.State == StateRunning {
		b.State = StateAwaiting
	}
}
