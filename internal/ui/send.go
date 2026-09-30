package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

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

// submit sends text, or, while the previous send is unsettled, queues
// the prompt: the text stays in it (still editable) and is sent once
// Chat.Send has returned and the run's end event has arrived.
func (s sender) submit(text string) tea.Cmd {
	a := s.a
	if a.sess.run.busy() {
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
// chosen agent, model, and effort.
func (s sender) send(text string) tea.Cmd {
	a := s.a
	req := core.SendRequest{SessionID: a.sess.info.ID, Text: text, Attachments: attachments(text, a.sess.attach)}
	if req.SessionID == "" {
		req.Agent, req.Model, req.Effort = a.sess.info.Agent, a.sess.info.Model, string(a.sess.info.Effort)
	}
	// A sent path is consumed: a later message repeating its "@<path>"
	// token (by coincidence, or by re-typing it) must not re-attach it.
	a.sess.attach = nil
	id := a.sess.main.proj.AddUser(text, req.Attachments)
	before, _ := a.sess.main.list.Selected()
	a.flush(a.sess.main.withDirty([]transcript.BlockID{id}))
	// Sending always jumps the view to the bottom, even if the user had
	// scrolled up, so the new message and the reply are in view.
	a.sess.main.list.Bottom()
	ctx, cancel := context.WithCancel(a.ctx)
	a.sess.startRun(id, text, cancel)
	a.w.prompt.Reset()
	cmds := []tea.Cmd{sendCmd(ctx, a.ports, req), s.remember(text), normalKeys{a}.syncDetails(before)}
	if !a.view.stream.ticking {
		a.view.stream.ticking = true
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

// cancelGrace is how long after a ctrl+c that cancelled a run further
// ctrl+c presses do nothing, so a double tap can't quit.
const cancelGrace = time.Second

// runHint is the hint ctrl+d shows instead of quitting mid-run.
const runHint = "run in progress · ctrl+c to cancel"

// ctrlC is INSERT's ctrl+c ladder: clear a queued send and cancel the run
// (one press), else cancel the run, else clear the prompt, else quit. For
// cancelGrace after a press that cancelled, a press does nothing.
func (s sender) ctrlC() tea.Cmd {
	a := s.a
	now := a.opts.Clock.Now()
	switch {
	case !a.sess.run.cancelledAt.IsZero() && now.Sub(a.sess.run.cancelledAt) < cancelGrace:
		return nil
	case s.inRun():
		a.sess.run.cancelledAt = now
		return s.cancelRun()
	case a.w.prompt.Value() != "":
		a.w.prompt.Reset()
		return nil
	}
	return a.quit()
}

// normalCtrlC is NORMAL's ctrl+c: cancel the run only, never clear or
// quit. A cancel starts the same cancelGrace as INSERT's, so a quick
// `i` then ctrl+c can't clear the prompt or quit; for cancelGrace after
// that cancel, a further NORMAL ctrl+c also does nothing, the same as
// INSERT's ladder.
func (s sender) normalCtrlC() tea.Cmd {
	a := s.a
	now := a.opts.Clock.Now()
	if !a.sess.run.cancelledAt.IsZero() && now.Sub(a.sess.run.cancelledAt) < cancelGrace {
		return nil
	}
	if !s.inRun() {
		return nil
	}
	a.sess.run.cancelledAt = now
	return s.cancelRun()
}

// inRun reports whether a send is in flight, unsettled, or queued.
func (s sender) inRun() bool {
	return s.a.sess.queued || s.a.sess.run.busy()
}

// ctrlD is ctrl+d in any mode (in INSERT, on an empty prompt): quit when
// idle; during a run, a hint instead.
func (s sender) ctrlD() tea.Cmd {
	if s.inRun() {
		s.a.view.hint = runHint
		return nil
	}
	return s.a.quit()
}

// cancelRun drops a queued send, then cancels the send in flight: its
// context (which reaches the run even before the session is known) and,
// once the root is known, the root's run through Chat.Cancel. A send
// whose Send already returned but whose run end never arrived (a commit
// failure publishes none) is settled by hand, so the UI can't stay stuck.
func (s sender) cancelRun() tea.Cmd {
	a := s.a
	s.unqueue()
	if a.sess.run.awaitEnd {
		a.sess.run.awaitEnd, a.sess.run.running = false, false
		return s.idle()
	}
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

// done handles Chat.Send returning. The result says whether the run
// started: a send rejected up front reports no session, and ErrBusy
// means another run held it; nothing was stored or published for either.
// Such a send is undone — its user block removed, its text back in the
// prompt, the queue dropped — and its error becomes the hint. A root not
// yet adopted (SessionCreated still on its way) is adopted from the
// result. A run that started settles when its end event arrives (maybe
// already); only then is a queued prompt sent.
func (s sender) done(msg sendDoneMsg) tea.Cmd {
	a := s.a
	if a.sess.adopt(core.Session{ID: msg.res.SessionID}) {
		a.w.setItems(a.sess.main, a.sess.allItems())
	}
	ran := msg.res.SessionID != "" && !errors.Is(msg.err, core.ErrBusy)
	run := a.sess.run
	settled, dirty := a.sess.endSend(ran)
	a.flush(dirty)
	if ran && msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		// The run started (its user message is stored) but failed; the
		// projection's own RunFailed notice covers the transcript, but the
		// status hint would otherwise stay empty since this path never
		// reaches undo below.
		a.view.hint = "send: " + ansi.SanitizeLine(msg.err.Error())
	}
	if !ran && msg.err != nil {
		s.undo(run.userID, run.text, msg.err)
		return s.idle()
	}
	if !settled {
		return nil
	}
	return s.afterRun()
}

// undo reverts a send that never ran: the block goes, the text returns
// to the prompt (ahead of anything typed since), and the queue is
// dropped.
func (s sender) undo(id transcript.BlockID, text string, err error) {
	a := s.a
	a.w.setItems(a.sess.main, a.sess.dropUser(id))
	columnDropStale(a)
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

// afterRun sends the queued prompt, if any; otherwise the App is idle.
func (s sender) afterRun() tea.Cmd {
	a := s.a
	if a.sess.queued {
		s.unqueue()
		if text := a.w.prompt.Value(); strings.TrimSpace(text) != "" {
			return s.send(text)
		}
	}
	return s.idle()
}

// idle runs what waited for no send to be in flight: re-reading the git
// branch (the run may have switched it) and a session whose resume
// arrived mid-send.
func (s sender) idle() tea.Cmd {
	a := s.a
	branch := branchCmd(a.ctx, a.ports)
	id := a.view.resumeHeld
	if id == "" || a.sess.run.busy() {
		return branch
	}
	a.view.resumeHeld = ""
	return tea.Batch(branch, resumeCmd(a.ctx, a.ports, id))
}
