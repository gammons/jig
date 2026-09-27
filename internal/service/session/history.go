package session

import (
	"context"

	"github.com/gammons/jig/internal/core"
)

// History returns the messages the model should see for id: the last
// compaction message and everything after it, or every message when the
// session has never been compacted.
func (s *Service) History(ctx context.Context, id core.SessionID) ([]core.Message, error) {
	msgs, err := s.d.Store.ListMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	if i := lastCompaction(msgs); i >= 0 {
		return msgs[i:], nil
	}
	return msgs, nil
}

// lastCompaction returns the index of the last message holding a
// PartCompaction, or -1.
func lastCompaction(msgs []core.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if isCompaction(msgs[i]) {
			return i
		}
	}
	return -1
}

func isCompaction(m core.Message) bool {
	for _, p := range m.Parts {
		if p.Kind == core.PartCompaction {
			return true
		}
	}
	return false
}
