// Package permcard is jig's inline permission card: it renders under the
// block that requested a tool call and turns a/A/d/D (plus, for D, a
// one-line deny message and enter/esc) into a Reply through the caller's
// ReplyFunc.
package permcard

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// ReplyKind is the kind of reply a card can send.
type ReplyKind string

const (
	ReplyOnce   ReplyKind = "once"
	ReplyAlways ReplyKind = "always"
	ReplyDeny   ReplyKind = "deny"
)

// Reply is what a card key press (or the deny-with-message input) sends.
type Reply struct {
	Kind    ReplyKind
	Message string
}

// ReplyFunc sends a Reply for requestID. It must yield its effect (e.g. a
// PermissionService.Reply call) via its returned Cmd; the widget never
// does I/O itself.
type ReplyFunc func(requestID string, r Reply) tea.Cmd

// Request is one pending permission request. Subagent is "" for the root
// agent. Tool, Subject, and Subagent are pre-sanitized by ui, but line 1
// still truncates the subject to fit the card's width.
type Request struct {
	ID, Tool, Subject, Subagent string
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// WithKeyMap sets the key bindings.
func WithKeyMap(km KeyMap) Option { return func(m *Model) { m.keys = km } }

// Model is the permission card. Use the Model most recently returned by
// Update; it is not safe for concurrent use.
type Model struct {
	reply  ReplyFunc
	styles Styles
	keys   KeyMap
	width  int
	req    *Request
	typing bool
	input  textinput.Model
	ver    int
}

// New builds an empty card that sends replies through reply.
func New(reply ReplyFunc, opts ...Option) Model {
	m := Model{
		reply:  reply,
		styles: DefaultStyles(),
		keys:   DefaultKeyMap(),
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// SetWidth sets the width the card's lines are fit to.
func (m *Model) SetWidth(w int) {
	m.width = w
	m.input.SetWidth(inputWidth(w))
}

// Set replaces the shown request; nil clears the card and closes any open
// deny-message input. Version bumps only when the request actually
// changes (a Set with equal field values is a no-op).
func (m *Model) Set(req *Request) {
	if reqEqual(m.req, req) {
		return
	}
	m.req = cloneRequest(req)
	m.typing = false
	m.input = textinput.Model{}
	m.ver++
}

// Request returns the current request, or nil when the card is empty.
func (m Model) Request() *Request { return m.req }

// Typing reports whether the deny-with-message input is open.
func (m Model) Typing() bool { return m.typing }

// Version increases by one on every change that affects View's output.
func (m Model) Version() int { return m.ver }

// Update handles a/A/d/D and, while typing, every key routed to the
// deny-message input plus enter (send) and esc (close). It is a no-op
// without a Request.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || m.req == nil {
		return m, nil
	}
	if m.typing {
		return m.updateTyping(k)
	}
	return m.updateIdle(k)
}

func (m Model) updateIdle(k tea.KeyPressMsg) (Model, tea.Cmd) {
	id := m.req.ID
	switch {
	case key.Matches(k, m.keys.DenyMsg):
		m.input = newDenyInput(m.width)
		m.typing = true
		m.ver++
		return m, m.input.Focus()
	case key.Matches(k, m.keys.Allow):
		return m, m.reply(id, Reply{Kind: ReplyOnce})
	case key.Matches(k, m.keys.Always):
		return m, m.reply(id, Reply{Kind: ReplyAlways})
	case key.Matches(k, m.keys.Deny):
		return m, m.reply(id, Reply{Kind: ReplyDeny})
	}
	return m, nil
}

func (m Model) updateTyping(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Cancel):
		m.typing = false
		m.input = textinput.Model{}
		m.ver++
		return m, nil
	case key.Matches(k, m.keys.Send):
		id, text := m.req.ID, m.input.Value()
		m.typing = false
		m.input = textinput.Model{}
		m.ver++
		return m, m.reply(id, Reply{Kind: ReplyDeny, Message: text})
	}
	ti, cmd := m.input.Update(k)
	m.input = ti
	m.ver++
	return m, cmd
}

func newDenyInput(width int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "  deny: "
	ti.Placeholder = "reason (optional)"
	ti.SetWidth(inputWidth(width))
	return ti
}

func inputWidth(w int) int { return max(1, w-len("  deny: ")) }

func reqEqual(a, b *Request) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func cloneRequest(req *Request) *Request {
	if req == nil {
		return nil
	}
	c := *req
	return &c
}
