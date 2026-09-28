package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core/event"
)

// eventMsg carries one bus event into the App's Update.
type eventMsg struct{ ev event.Event }

// waitEvent returns a Cmd that blocks for sub's next event and yields it
// as an eventMsg, or yields nil once sub is closed. The App re-arms it
// after every eventMsg, so events arrive one at a time and in order. A
// nil sub gives a nil Cmd.
func waitEvent(sub *event.Subscription) tea.Cmd {
	if sub == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-sub.C()
		if !ok {
			return nil
		}
		return eventMsg{ev: ev}
	}
}
