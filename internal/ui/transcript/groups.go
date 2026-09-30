package transcript

// groupPrefix starts every Group ID. It cannot collide with a block ID
// ("u/", "m/", "t/", "n/").
const groupPrefix = "g/"

// Group is a run of at least two consecutive exploration calls (read,
// grep, glob) with the reasoning blocks between them, shown as one line
// in the TUI (tool-call groups spec §3). ID is "g/" plus its first call's
// ID, so it stays the same as the group grows and across reloads.
type Group struct {
	ID      BlockID
	Members []BlockID // display order: calls and absorbed reasoning
}

// isExploration reports whether b is a read, grep, or glob call, in any
// state.
func isExploration(b *Block) bool {
	if b.Kind != KindTool || b.Call == nil {
		return false
	}
	switch b.Call.Name {
	case "read", "grep", "glob":
		return true
	}
	return false
}

// Groups returns blocks' groups in display order, or nil. A reasoning
// block joins a run only between two of its calls; reasoning before the
// first call or after the last is not a member. Any other block (text,
// user, notice, subagent, or another tool) ends the run.
func Groups(blocks []Block) []Group {
	var out []Group
	var members, pending []BlockID // pending: reasoning after the run's last call
	calls, first := 0, ""
	flush := func() {
		if calls >= 2 {
			out = append(out, Group{ID: BlockID(groupPrefix + first), Members: members})
		}
		members, pending, calls, first = nil, nil, 0, ""
	}
	for i := range blocks {
		b := &blocks[i]
		switch {
		case isExploration(b):
			if calls == 0 {
				first = b.Call.ID
			}
			members = append(append(members, pending...), b.ID)
			pending = nil
			calls++
		case b.Kind == KindReasoning:
			if calls > 0 {
				pending = append(pending, b.ID)
			}
		default:
			flush()
		}
	}
	flush()
	return out
}
