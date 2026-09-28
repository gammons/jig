package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/core"
)

// Every port call the App makes happens inside one of these Cmds, never
// in Update or View. Calls that take a context get the App's base
// context, which is cancelled when the App quits.

// readFileCmd returns a Cmd that reads path through p.Project and hands
// the result to done, on ui's behalf: the only I/O here is the port call,
// never a direct file read.
func readFileCmd(ctx context.Context, p Ports, path string, done func([]byte, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		data, err := p.Project.ReadFile(ctx, path)
		return done(data, err)
	}
}

// openBlobCmd returns a Cmd that opens ref through p.Blobs and hands the
// result to done. BlobService.Open takes no context, so a cancelled ctx
// (the App quit) only skips the open.
func openBlobCmd(ctx context.Context, p Ports, ref string, done func([]byte, string, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if err := ctx.Err(); err != nil {
			return done(nil, "", err)
		}
		data, mime, err := p.Blobs.Open(ref)
		return done(data, mime, err)
	}
}

// sessionMessagesCmd returns a Cmd that lists id's messages through
// p.Sessions and hands the result to done.
func sessionMessagesCmd(ctx context.Context, p Ports, id core.SessionID, done func([]core.Message, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msgs, err := p.Sessions.Messages(ctx, id)
		return done(msgs, err)
	}
}

// sendDoneMsg is the outcome of a Chat.Send, which returns only once its
// run has ended.
type sendDoneMsg struct {
	res core.SendResult
	err error
}

// resumeMsg carries a resumed session and its history.
type resumeMsg struct {
	info  core.Session
	msgs  []core.Message
	todos []core.Todo
	err   error
}

// prefsMsg carries the stored prefs, read at startup.
type prefsMsg struct{ prefs core.Prefs }

// agentsMsg carries the primary agents.
type agentsMsg struct{ agents []core.Agent }

// catalogMsg carries the catalog's providers.
type catalogMsg struct{ providers []core.ProviderStatus }

// errMsg reports a failed port call; the App shows it as a status hint.
type errMsg struct {
	what string
	err  error
}

// editorExecMsg carries the command an EditorService prepared, to run
// with tea.Exec, and the func that reads the edited text back.
type editorExecMsg struct {
	cmd    core.ExecCommand
	result func() (string, error)
}

// editorExitedMsg reports that the editor command exited.
type editorExitedMsg struct {
	err    error
	result func() (string, error)
}

// sendCmd runs req through p.Chat under ctx.
func sendCmd(ctx context.Context, p Ports, req core.SendRequest) tea.Cmd {
	return func() tea.Msg {
		res, err := p.Chat.Send(ctx, req)
		return sendDoneMsg{res: res, err: err}
	}
}

// cancelCmd cancels id's run through p.Chat.
func cancelCmd(p Ports, id core.SessionID) tea.Cmd {
	return func() tea.Msg {
		p.Chat.Cancel(id)
		return nil
	}
}

// configureCmd sets id's agent and/or model through p.Sessions.
func configureCmd(ctx context.Context, p Ports, id core.SessionID, agent, model string) tea.Cmd {
	return func() tea.Msg {
		if err := p.Sessions.Configure(ctx, id, agent, model); err != nil {
			return errMsg{what: "configure", err: err}
		}
		return nil
	}
}

// resumeCmd loads id's session, messages, and todos through p.Sessions.
func resumeCmd(ctx context.Context, p Ports, id core.SessionID) tea.Cmd {
	return func() tea.Msg {
		info, err := p.Sessions.Get(ctx, id)
		if err != nil {
			return resumeMsg{err: err}
		}
		msgs, err := p.Sessions.Messages(ctx, id)
		if err != nil {
			return resumeMsg{err: err}
		}
		todos, err := p.Sessions.Todos(ctx, id)
		return resumeMsg{info: info, msgs: msgs, todos: todos, err: err}
	}
}

// prefsCmd reads the stored prefs through p.Prefs.
func prefsCmd(p Ports) tea.Cmd {
	if p.Prefs == nil {
		return nil
	}
	return func() tea.Msg { return prefsMsg{prefs: p.Prefs.Get()} }
}

// prefsUpdateCmd applies fn to the stored prefs through p.Prefs.
func prefsUpdateCmd(p Ports, fn func(*core.Prefs)) tea.Cmd {
	if p.Prefs == nil {
		return nil
	}
	return func() tea.Msg {
		if err := p.Prefs.Update(fn); err != nil {
			return errMsg{what: "prefs", err: err}
		}
		return nil
	}
}

// agentsCmd lists the primary agents through p.Agents.
func agentsCmd(p Ports) tea.Cmd {
	if p.Agents == nil {
		return nil
	}
	return func() tea.Msg { return agentsMsg{agents: p.Agents.Primary()} }
}

// catalogCmd lists the catalog's providers through p.Catalog.
func catalogCmd(p Ports) tea.Cmd {
	if p.Catalog == nil {
		return nil
	}
	return func() tea.Msg { return catalogMsg{providers: p.Catalog.Providers()} }
}

// editorCmd prepares an $EDITOR round trip over text through p.Editor. A
// failure comes back as a prompt.EditedMsg with Err set.
func editorCmd(p Ports, text string) tea.Cmd {
	if p.Editor == nil {
		return nil
	}
	return func() tea.Msg {
		cmd, result, err := p.Editor.Edit(text)
		if err != nil {
			return prompt.EditedMsg{Err: err}
		}
		return editorExecMsg{cmd: cmd, result: result}
	}
}

// editorResultCmd reads the edited text back once the editor exited.
func editorResultCmd(m editorExitedMsg) tea.Cmd {
	return func() tea.Msg {
		if m.err != nil {
			return prompt.EditedMsg{Err: m.err}
		}
		text, err := m.result()
		return prompt.EditedMsg{Text: text, Err: err}
	}
}
