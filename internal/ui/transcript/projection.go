package transcript

import (
	"strconv"

	"github.com/gammons/jig/internal/core"
)

// Projection is the display state of one root session and its descendants.
// It is not safe for concurrent use; its single owner (the TUI model)
// serializes Load and Apply.
type Projection struct {
	root    core.SessionID
	list    blockList
	open    map[core.MessageID]*Block // the text/reasoning block each message's deltas append to
	parts   map[core.MessageID]int    // next "m/<msg>/<n>" ordinal per message
	notices int                       // next "n/<k>" ordinal; never reset, so notice IDs never repeat
	owner   map[core.SessionID]BlockID
	pending []PendingPermission // request order
	derived derived
}

// derived holds state computed from tool results rather than shown as
// blocks: changed files (R16) and the last browser URL.
type derived struct {
	changed []FileChange
	url     string
}

// New returns an empty projection of root's transcript.
func New(root core.SessionID) *Projection {
	p := &Projection{root: root}
	p.reset()
	return p
}

// Blocks returns a copy of every block, in display order.
func (p *Projection) Blocks() []Block { return p.list.snapshot() }

// Block returns a copy of the block with id.
func (p *Projection) Block(id BlockID) (Block, bool) {
	b, ok := p.list.get(id)
	if !ok {
		return Block{}, false
	}
	return b.clone(), true
}

// ChangedFiles returns the files successful write and edit calls touched,
// in first-seen order.
func (p *Projection) ChangedFiles() []FileChange {
	return append([]FileChange(nil), p.derived.changed...)
}

// Pending returns the unresolved permission requests, in request order.
func (p *Projection) Pending() []PendingPermission {
	return append([]PendingPermission(nil), p.pending...)
}

// LastBrowserURL returns the URL of the root session's most recent
// agent-browser navigation, or "".
func (p *Projection) LastBrowserURL() string { return p.derived.url }

// reset clears everything but the notice counter.
func (p *Projection) reset() {
	p.list = blockList{index: make(map[BlockID]int)}
	p.open = make(map[core.MessageID]*Block)
	p.parts = make(map[core.MessageID]int)
	p.owner = make(map[core.SessionID]BlockID)
	p.pending = nil
	p.derived = derived{}
}

// addPart appends a text or reasoning block for msg with the next
// per-message ordinal.
func (p *Projection) addPart(msg core.MessageID, kind Kind, text string, streaming bool) *Block {
	n := p.parts[msg]
	p.parts[msg] = n + 1
	id := BlockID("m/" + string(msg) + "/" + strconv.Itoa(n))
	return p.list.add(&Block{ID: id, Kind: kind, MessageID: msg, Text: text, Streaming: streaming})
}

// addNotice appends a notice block with the next notice ordinal.
func (p *Projection) addNotice(msg core.MessageID, title, text string, level Level) *Block {
	id := BlockID("n/" + strconv.Itoa(p.notices))
	p.notices++
	return p.list.add(&Block{ID: id, Kind: KindNotice, MessageID: msg, Title: title, Text: text, Level: level})
}

// blockList is the ordered block store with an ID index.
type blockList struct {
	blocks []*Block
	index  map[BlockID]int
}

// add appends b at Version 1 and returns it.
func (l *blockList) add(b *Block) *Block {
	b.Version = 1
	l.index[b.ID] = len(l.blocks)
	l.blocks = append(l.blocks, b)
	return b
}

func (l *blockList) get(id BlockID) (*Block, bool) {
	i, ok := l.index[id]
	if !ok {
		return nil, false
	}
	return l.blocks[i], true
}

func (l *blockList) snapshot() []Block {
	out := make([]Block, len(l.blocks))
	for i, b := range l.blocks {
		out[i] = b.clone()
	}
	return out
}

// newToolBlock returns the block for call: a subagent block for a task
// call, whose agent, description and (on resume) child come from its input,
// and a tool block otherwise.
func newToolBlock(msg core.MessageID, call core.ToolCall, state ToolState) *Block {
	b := &Block{ID: BlockID(call.ID), Kind: KindTool, MessageID: msg, Call: &call, State: state}
	if call.Name == taskTool {
		b.Kind = KindSubagent
		b.Sub = subagentOf(call)
	}
	return b
}

// finish records r on b and sets its final state.
func (b *Block) finish(r core.ToolResult) {
	b.Result = &r
	b.State = stateOf(r)
	b.Permission = nil
	if b.Sub != nil {
		if child := childOf(r.Output); child != "" {
			b.Sub.Child = child
		}
		b.Sub.Current = ""
	}
}
