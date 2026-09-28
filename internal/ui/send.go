package ui

import (
	"context"
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

// maxHistory is how many prompts are kept per project (spec §7.3).
const maxHistory = 100

// sender is the App's send path: sending, queueing while a send is in
// flight, undoing a send that never ran, and the ctrl+c ladder.
type sender struct{ a *App }

// submit sends text, or, while a send is in flight, queues the prompt:
// the text stays in it (still editable) and is sent once Chat.Send
// returns.
func (s sender) submit(text string) tea.Cmd {
	a := s.a
	if a.sess.run.inFlight {
		a.sess.queued = true
		a.w.prompt.SetQueued(true)
		return nil
	}
	return s.send(text)
}

// send starts a run under its own cancellable context (so ctrl+c can
// cancel it even before the new session is known): the user block
// appears at once, the prompt resets, the text joins the project's
// history, and streamTick starts. A new session is created with the
// chosen agent and model.
func (s sender) send(text string) tea.Cmd {
	a := s.a
	req := core.SendRequest{SessionID: a.sess.info.ID, Text: text}
	if req.SessionID == "" {
		req.Agent, req.Model = a.sess.info.Agent, a.sess.info.Model
	}
	id := a.sess.proj.AddUser(text, nil)
	a.flush(a.sess.withDirty([]transcript.BlockID{id}))
	ctx, cancel := context.WithCancel(a.ctx)
	a.sess.startRun(id, text, cancel)
	a.w.prompt.Reset()
	cmds := []tea.Cmd{sendCmd(ctx, a.ports, req), s.remember(text)}
	if !a.view.ticking {
		a.view.ticking = true
		cmds = append(cmds, a.after(streamInterval, streamTickMsg{}))
	}
	return tea.Batch(cmds...)
}

// remember appends text to the project's prompt history, in the prompt
// at once and in prefs, keeping the last maxHistory.
func (s sender) remember(text string) tea.Cmd {
	a := s.a
	a.view.history = lastN(append(a.view.history, text), maxHistory)
	a.w.prompt.SetHistory(a.view.history)
	key := a.opts.ProjectKey
	return prefsUpdateCmd(a.ports, func(p *core.Prefs) {
		if p.History == nil {
			p.History = map[string][]string{}
		}
		p.History[key] = lastN(append(p.History[key], text), maxHistory)
	})
}

// lastN returns (a copy of) the last n entries of s.
func lastN(s []string, n int) []string {
	return slices.Clone(s[max(0, len(s)-n):])
}

// ctrlC is the ctrl+c ladder: clear a queued send and cancel the run (one
// press), else cancel the run, else clear the prompt, else quit.
func (s sender) ctrlC() tea.Cmd {
	a := s.a
	switch {
	case a.sess.queued || a.sess.run.inFlight:
		return s.cancelRun()
	case a.w.prompt.Value() != "":
		a.w.prompt.Reset()
		return nil
	}
	return a.quit()
}

// cancelRun drops a queued send, then cancels the send in flight: its
// context (which reaches the run even before the session is known) and,
// once the root is known, the root's run through Chat.Cancel.
func (s sender) cancelRun() tea.Cmd {
	a := s.a
	s.unqueue()
	if !a.sess.run.inFlight {
		return nil
	}
	if a.sess.run.cancel != nil {
		a.sess.run.cancel()
	}
	if a.sess.info.ID == "" {
		return nil
	}
	return cancelCmd(a.ports, a.sess.info.ID)
}

// unqueue drops a queued send; its text stays in the prompt.
func (s sender) unqueue() {
	s.a.sess.queued = false
	s.a.w.prompt.SetQueued(false)
}

// done handles Chat.Send returning, which means the run is over and the
// Runner has released the session: the run state always ends here. A
// send that failed before its run published any run event (bad config, a
// busy session, a cancel before it started) is undone — its user block
// removed, its text back in the prompt, the queue dropped — and its error
// becomes the hint. Otherwise a queued prompt is sent now.
func (s sender) done(msg sendDoneMsg) tea.Cmd {
	a := s.a
	run := a.sess.run
	a.flush(a.sess.endSend())
	if msg.err != nil && !run.sawEvent {
		s.undo(run.userID, run.text, msg.err)
		return nil
	}
	return s.afterRun()
}

// undo reverts a send that never ran: the block goes, the text returns
// to the prompt (ahead of anything typed since), and the queue is
// dropped.
func (s sender) undo(id transcript.BlockID, text string, err error) {
	a := s.a
	a.w.list.SetItems(a.sess.dropUser(id))
	if cur := a.w.prompt.Value(); cur != "" {
		text += "\n" + cur
	}
	a.w.prompt.Reset()
	a.w.prompt.Insert(text)
	s.unqueue()
	if !errors.Is(err, context.Canceled) {
		a.view.hint = "send: " + ansi.SanitizeLine(err.Error())
	}
}

// afterRun sends the queued prompt, if any.
func (s sender) afterRun() tea.Cmd {
	a := s.a
	if !a.sess.queued {
		return nil
	}
	s.unqueue()
	text := a.w.prompt.Value()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return s.send(text)
}
