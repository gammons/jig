package prompt

// historyState tracks walking through prompt history with ↑/↓, saving the
// in-progress draft on the first step so it can be restored once the walk
// returns past the newest entry.
type historyState struct {
	entries []string // oldest first
	idx     int      // index into entries currently shown; -1 = showing the draft
	draft   string   // saved live text, valid while idx >= 0
}

// newHistoryState returns a historyState with no entries and no walk in
// progress.
func newHistoryState() historyState {
	return historyState{idx: -1}
}

// setEntries replaces the history (oldest first) and cancels any walk in
// progress.
func (h *historyState) setEntries(entries []string) {
	h.entries = append([]string(nil), entries...)
	h.idx = -1
	h.draft = ""
}

// prev walks one entry further into the past, saving current as the draft
// on the first step. It reports false when there is no older entry (or no
// history at all), leaving the state unchanged.
func (h *historyState) prev(current string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if h.idx == -1 {
		h.draft = current
		h.idx = len(h.entries) - 1
		return h.entries[h.idx], true
	}
	if h.idx == 0 {
		return "", false
	}
	h.idx--
	return h.entries[h.idx], true
}

// next walks one entry back toward the present, restoring the saved draft
// once it passes the newest entry. It reports false when not currently
// walking.
func (h *historyState) next() (string, bool) {
	if h.idx == -1 {
		return "", false
	}
	if h.idx == len(h.entries)-1 {
		h.idx = -1
		return h.draft, true
	}
	h.idx++
	return h.entries[h.idx], true
}
