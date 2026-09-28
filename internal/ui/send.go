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

// sender is the App's send path: sending, queueing while a run is in
// flight, and the ctrl+c ladder.
type sender struct{ a *App }

// submit sends text, or, while a run is in flight, queues the prompt: the
// text stays in it (still editable) and is sent when the run ends.
func (s sender) submit(text string) tea.Cmd {
	a := s.a
	if a.sess.run.running {
		a.sess.queued = true
		a.w.prompt.SetQueued(true)
		return nil
	}
	return s.send(text)
}

// send starts a run: the user block appears at once, the prompt resets,
// the text joins the project's history, and streamTick starts. A new
// session is created with the chosen agent and model.
func (s sender) send(text string) tea.Cmd {
	a := s.a
	req := core.SendRequest{SessionID: a.sess.info.ID, Text: text}
	if req.SessionID == "" {
		req.Agent, req.Model = a.sess.info.Agent, a.sess.info.Model
	}
	a.flush([]transcript.BlockID{a.sess.proj.AddUser(text, nil)})
	a.sess.startRun()
	a.w.prompt.Reset()
	cmds := []tea.Cmd{sendCmd(a.ctx, a.ports, req), s.remember(text)}
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
	case a.sess.queued || a.sess.run.running:
		return s.cancelRun()
	case a.w.prompt.Value() != "":
		a.w.prompt.Reset()
		return nil
	}
	return a.quit()
}

// cancelRun drops a queued send, then cancels the root's run.
func (s sender) cancelRun() tea.Cmd {
	a := s.a
	s.unqueue()
	if !a.sess.run.running || a.sess.info.ID == "" {
		return nil
	}
	return cancelCmd(a.ports, a.sess.info.ID)
}

// unqueue drops a queued send; its text stays in the prompt.
func (s sender) unqueue() {
	s.a.sess.queued = false
	s.a.w.prompt.SetQueued(false)
}

// afterRun sends the queued prompt, if any, once the root's run ended.
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

// done handles Chat.Send returning. A send that failed before its run
// published anything (bad config, a busy session) ends the run here —
// no RunFailed will come — and drops the queue; its error becomes the
// hint. A run that started reports its own end through events.
func (s sender) done(msg sendDoneMsg) tea.Cmd {
	a := s.a
	if msg.err == nil || errors.Is(msg.err, context.Canceled) {
		return nil
	}
	if a.sess.run.running && !a.sess.run.sawEvent {
		a.sess.run.running = false
		s.unqueue()
	}
	if !a.sess.run.sawEvent {
		a.view.hint = "send: " + ansi.SanitizeLine(msg.err.Error())
	}
	return nil
}
