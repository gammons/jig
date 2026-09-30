package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
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

// hint is "<what>: <error>", sanitized, without repeating a prefix the
// error already carries (e.g. "permission: unknown request").
func (m errMsg) hint() string {
	text := ansi.SanitizeLine(m.err.Error())
	if strings.HasPrefix(text, m.what+": ") {
		return text
	}
	return m.what + ": " + text
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

// setEffortCmd sets id's reasoning effort ("" clears it) through p.Sessions.
func setEffortCmd(ctx context.Context, p Ports, id core.SessionID, e core.Effort) tea.Cmd {
	return func() tea.Msg {
		if err := p.Sessions.SetEffort(ctx, id, e); err != nil {
			return errMsg{what: "effort", err: err}
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

// branchMsg is the workdir's git branch ("" outside a repo or on error).
type branchMsg struct{ branch string }

// branchCmd reads the workdir's git branch through p.Project. A failure
// just hides the branch.
func branchCmd(ctx context.Context, p Ports) tea.Cmd {
	if p.Project == nil {
		return nil
	}
	return func() tea.Msg {
		b, err := p.Project.Branch(ctx)
		if err != nil {
			return branchMsg{}
		}
		return branchMsg{branch: b}
	}
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

// itemsCmd yields a picker level's items, built without a port call.
func itemsCmd(level string, items []picker.Item) tea.Cmd {
	return func() tea.Msg { return picker.ItemsMsg{Level: level, Items: items} }
}

// mcpActionMsg carries the outcome of an MCP server action (signin,
// cancel, reconnect, signout) run through mcpActionCmd.
type mcpActionMsg struct {
	verb, name string
	err        error
}

// mcpActionCmd runs verb (signin|cancel|reconnect|signout) on server name
// through p.MCP. signin can block for up to 5 minutes; that's fine, it
// runs inside this Cmd.
func mcpActionCmd(ctx context.Context, p Ports, verb, name string) tea.Cmd {
	return func() tea.Msg {
		if p.MCP == nil {
			return mcpActionMsg{verb: verb, name: name}
		}
		var err error
		switch verb {
		case "signin":
			err = p.MCP.Authenticate(ctx, name)
		case "cancel":
			err = p.MCP.CancelAuth(ctx, name)
		case "reconnect":
			err = p.MCP.Reconnect(ctx, name)
		case "signout":
			err = p.MCP.Logout(ctx, name)
		}
		return mcpActionMsg{verb: verb, name: name, err: err}
	}
}

// sessionsCmd lists the workdir's sessions (at most maxSessions) through
// p.Sessions, each with its summed message cost, as the sessions level.
func sessionsCmd(ctx context.Context, p Ports, workDir string, current core.SessionID, now time.Time) tea.Cmd {
	return func() tea.Msg {
		list, err := p.Sessions.ListForCwd(ctx, workDir, maxSessions)
		if err != nil {
			return picker.ItemsMsg{Level: levelSessions, Err: err}
		}
		costs := make([]float64, len(list))
		for i, s := range list {
			msgs, err := p.Sessions.Messages(ctx, s.ID)
			if err != nil {
				return picker.ItemsMsg{Level: levelSessions, Err: err}
			}
			for _, m := range msgs {
				costs[i] += m.CostUSD
			}
		}
		return picker.ItemsMsg{Level: levelSessions, Items: sessionItems(list, costs, current, now)}
	}
}

// modelsCmd lists the catalog's models through p.Catalog as the models
// level; current is the "provider/model" in use.
func modelsCmd(p Ports, current string) tea.Cmd {
	if p.Catalog == nil {
		return itemsCmd(levelModels, nil)
	}
	return func() tea.Msg {
		return picker.ItemsMsg{Level: levelModels, Items: modelItems(p.Catalog.Providers(), current)}
	}
}

// agentItemsCmd lists the primary agents through p.Agents as the agents
// level; current is the agent in use.
func agentItemsCmd(p Ports, current string) tea.Cmd {
	if p.Agents == nil {
		return itemsCmd(levelAgents, nil)
	}
	return func() tea.Msg {
		return picker.ItemsMsg{Level: levelAgents, Items: agentItems(p.Agents.Primary(), current)}
	}
}

// filesCmd lists the project's files through p.Project as the files
// level, the touched ones (most recent first) leading.
func filesCmd(ctx context.Context, p Ports, touched []string) tea.Cmd {
	if p.Project == nil {
		return itemsCmd(levelFiles, nil)
	}
	return func() tea.Msg {
		files, err := p.Project.Files(ctx)
		if err != nil {
			return picker.ItemsMsg{Level: levelFiles, Err: err}
		}
		return picker.ItemsMsg{Level: levelFiles, Items: fileItems(files, touched)}
	}
}

// renameCmd renames id through p.Sessions; its SessionUpdated event
// carries the new title back.
func renameCmd(ctx context.Context, p Ports, id core.SessionID, title string) tea.Cmd {
	return func() tea.Msg {
		if err := p.Sessions.Rename(ctx, id, title); err != nil {
			return errMsg{what: "rename", err: err}
		}
		return nil
	}
}

// compactedMsg is the outcome of a Chat.Compact of id.
type compactedMsg struct {
	id  core.SessionID
	err error
}

// compactCmd compacts id's history through p.Chat.
func compactCmd(ctx context.Context, p Ports, id core.SessionID) tea.Cmd {
	return func() tea.Msg { return compactedMsg{id: id, err: p.Chat.Compact(ctx, id)} }
}

// extCmd runs an extension command with no arguments; what names it in
// the hint an error becomes.
func extCmd(ctx context.Context, c ext.Command, what string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Run(ctx, nil); err != nil {
			return errMsg{what: what, err: err}
		}
		return nil
	}
}

// permReplyCmd answers permission request id through p.Perms. A reply to
// a request that is no longer pending (its run was cancelled, or another
// reply won) fails harmlessly: the error only becomes a hint.
func permReplyCmd(p Ports, id string, r core.PermissionReply) tea.Cmd {
	if p.Perms == nil {
		return nil
	}
	return func() tea.Msg {
		if err := p.Perms.Reply(id, r); err != nil {
			return errMsg{what: "permission", err: err}
		}
		return nil
	}
}
