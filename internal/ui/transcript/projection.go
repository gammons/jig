package transcript

import (
	"slices"
	"strconv"
	"strings"

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
	users   int                       // next "u/pending/<k>" ordinal; never reset
	tree    lineage                   // live: survives Load
	perms   permissions               // live: survives Load
	derived derived
}

// pendingUserPrefix starts the ID of every block AddUser appends.
const pendingUserPrefix = "u/pending/"

// New returns an empty projection of root's transcript.
func New(root core.SessionID) *Projection {
	p := &Projection{root: root, tree: newLineage()}
	p.reset()
	return p
}

// AddUser appends a block for a user message the caller just sent, which
// no event announces, and returns its ID, "u/pending/<k>". k counts from 0
// and is never reused. The next Load replaces it with the stored message.
func (p *Projection) AddUser(text string, attachments []string) BlockID {
	id := BlockID(pendingUserPrefix + strconv.Itoa(p.users))
	p.users++
	p.list.add(&Block{ID: id, Kind: KindUser, Text: text, Attachments: slices.Clone(attachments)})
	return id
}

// DropUser removes a block AddUser appended ("u/pending/<k>"), for a send
// that failed before its run started, and reports whether it did. Stored
// user blocks are never removed.
func (p *Projection) DropUser(id BlockID) bool {
	if !strings.HasPrefix(string(id), pendingUserPrefix) {
		return false
	}
	return p.list.remove(id)
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
	out := append([]PendingPermission(nil), p.perms.pending...)
	for i := range out {
		out[i].Call = cloneCall(out[i].Call)
	}
	return out
}

// LastBrowserURL returns the URL of the root session's most recent
// agent-browser navigation, or "".
func (p *Projection) LastBrowserURL() string { return p.derived.url }

// reset clears the state Load rebuilds from stored messages. The counters
// and the live-run state (the descendant lineage and unresolved permission
// requests, which stored messages don't record) survive.
func (p *Projection) reset() {
	p.list = blockList{index: make(map[BlockID]int)}
	p.open = make(map[core.MessageID]*Block)
	p.parts = make(map[core.MessageID]int)
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
// add appends b, first clearing Thinking on its message's previous
// newest block: whatever follows a reasoning block ends that thinking.
func (l *blockList) add(b *Block) *Block {
	for i := len(l.blocks) - 1; i >= 0; i-- {
		if prev := l.blocks[i]; prev.MessageID == b.MessageID && b.MessageID != "" {
			if prev.Thinking {
				prev.Thinking = false
				prev.Version++
			}
			break
		}
	}
	b.Version = 1
	l.index[b.ID] = len(l.blocks)
	l.blocks = append(l.blocks, b)
	return b
}

// remove deletes id's block, reporting whether it existed.
func (l *blockList) remove(id BlockID) bool {
	i, ok := l.index[id]
	if !ok {
		return false
	}
	l.blocks = slices.Delete(l.blocks, i, i+1)
	delete(l.index, id)
	for j := i; j < len(l.blocks); j++ {
		l.index[l.blocks[j].ID] = j
	}
	return true
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
	call = cloneCall(call)
	b := &Block{ID: toolBlockID(call.ID), Kind: KindTool, MessageID: msg, Call: &call, State: state}
	if call.Name == taskTool {
		b.Kind = KindSubagent
		b.Sub = subagentOf(call)
	}
	return b
}

// finish records (a copy of) r on b and sets its final state.
func (b *Block) finish(r core.ToolResult) {
	r = cloneResult(r)
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
