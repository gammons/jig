// Package chat is the ChatService facade the UIs call: it validates a
// send request, creates or resumes the session, starts a background title,
// and drives the agent Runner.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/pathid"
	"github.com/gammons/jig/internal/service/session"
)

// defaultAgent runs when neither the request nor the session names one.
const defaultAgent = "build"

// ErrClosed is returned by Send after Close.
var ErrClosed = errors.New("chat: service closed")

// ConfigError marks a Send failure caused by configuration or user input
// (unknown agent, bad model, missing credentials) rather than by the run
// itself. Headless maps it to exit code 2.
type ConfigError struct{ Err error }

// Error returns the wrapped error's message.
func (e *ConfigError) Error() string { return e.Err.Error() }

// Unwrap returns e.Err.
func (e *ConfigError) Unwrap() error { return e.Err }

func configErr(format string, args ...any) error {
	return &ConfigError{Err: fmt.Errorf(format, args...)}
}

// Sessions creates, loads, and updates sessions.
type Sessions interface {
	Create(ctx context.Context, agent, cwd string) (core.Session, error)
	Get(ctx context.Context, id core.SessionID) (core.Session, error)
	Update(ctx context.Context, sess core.Session) error
	Touch(ctx context.Context, id core.SessionID) error
	GenerateTitle(ctx context.Context, id core.SessionID, firstPrompt string) error
	Compact(ctx context.Context, id core.SessionID) error
}

// Agents looks up agents and resolves their models.
type Agents interface {
	Get(name string) (core.Agent, bool)
	Primary() []core.Agent
	ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
	ResolveRef(s string) (core.ModelRef, error)
}

// LLMSource resolves a model to its client; Send uses it to preflight
// credentials before starting a run.
type LLMSource interface {
	For(core.ModelRef) (core.LLM, core.ModelInfo, error)
}

// Runner drives agent turns.
type Runner interface {
	Run(ctx context.Context, rc ext.RunContext, text string, atts ...core.Attachment) (core.Message, error)
	// Exclusive runs fn while holding id's slot in the running map, so it
	// excludes (and is excluded by) a Run on id.
	Exclusive(ctx context.Context, id core.SessionID, fn func(context.Context) error) error
	Cancel(id core.SessionID)
}

// Deps are the Service's collaborators.
type Deps struct {
	Sessions Sessions
	Agents   Agents
	LLMs     LLMSource
	Runner   Runner
	WorkDir  string
	Files    FileReader
	Reads    ReadMarker
	Images   Imager
}

// Service implements core.ChatService.
type Service struct {
	d      Deps
	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

// New returns a Service over d. Background work (titles) runs on a context
// that lives until Close.
func New(d Deps) *Service {
	base, cancel := context.WithCancel(context.Background())
	return &Service{d: d, base: base, cancel: cancel}
}

// plan is a validated Send: the session to run in, the agent and model,
// and whether the session must be created first.
type plan struct {
	sess     core.Session
	isNew    bool
	agent    core.Agent
	model    core.ModelRef
	flag     core.ModelRef
	newTitle bool
	atts     []core.Attachment
	reads    []textRead
}

// Send runs req.Text in req.SessionID (or a new session). Validation
// failures are *ConfigError and create nothing. A failed run still returns
// the SendResult for the session alongside the error.
func (s *Service) Send(ctx context.Context, req core.SendRequest) (core.SendResult, error) {
	p, err := s.prepare(ctx, req)
	if err != nil {
		return core.SendResult{}, err
	}
	if err := s.commit(ctx, &p, req.Text); err != nil {
		return core.SendResult{SessionID: p.sess.ID}, err
	}
	s.markAttachmentsRead(p)

	msg, runErr := s.d.Runner.Run(ctx, ext.RunContext{
		SessionID: p.sess.ID,
		RootID:    p.sess.ID,
		Agent:     p.agent,
		Model:     p.model,
		WorkDir:   s.d.WorkDir,
	}, req.Text, p.atts...)
	touchErr := s.d.Sessions.Touch(context.WithoutCancel(ctx), p.sess.ID)
	if runErr == nil {
		runErr = touchErr
	}
	return core.SendResult{SessionID: p.sess.ID, Message: msg}, runErr
}

// prepare validates req without writing anything: it loads a resumed
// session, picks and checks the agent, parses the model flag, resolves the
// model, and preflights its credentials.
func (s *Service) prepare(ctx context.Context, req core.SendRequest) (plan, error) {
	var p plan
	if req.SessionID == "" {
		p.isNew = true
	} else {
		sess, err := s.resume(ctx, req.SessionID)
		if err != nil {
			return plan{}, err
		}
		p.sess = sess
	}

	a, err := s.agentFor(firstNonEmpty(req.Agent, p.sess.Agent, defaultAgent))
	if err != nil {
		return plan{}, err
	}
	p.agent = a

	sessModel := parseOrZero(p.sess.Model)
	if req.Model != "" {
		if p.flag, err = s.d.Agents.ResolveRef(req.Model); err != nil {
			return plan{}, &ConfigError{Err: err}
		}
		sessModel = p.flag
	}
	if p.model, err = s.d.Agents.ResolveModel(a, core.ModelRef{}, sessModel); err != nil {
		return plan{}, &ConfigError{Err: err}
	}
	if _, _, err := s.d.LLMs.For(p.model); err != nil {
		return plan{}, &ConfigError{Err: err}
	}

	atts, reads, err := s.resolveAttachments(req.Attachments)
	if err != nil {
		return plan{}, err
	}
	p.atts, p.reads = atts, reads
	return p, nil
}

// markAttachmentsRead records p's text attachments as read by p.sess.ID,
// so a later edit or write to one of them needs no prior read tool call.
func (s *Service) markAttachmentsRead(p plan) {
	for _, r := range p.reads {
		s.d.Reads.MarkRead(p.sess.ID, r.path, r.info)
	}
}

// resume loads session id for a follow-up Send. A missing session, or one
// started in a different working directory, is a ConfigError; any other
// load failure is a run error. Directories are compared by pathid.Key, so a
// symlinked path to the session's Cwd resumes.
func (s *Service) resume(ctx context.Context, id core.SessionID) (core.Session, error) {
	sess, err := s.d.Sessions.Get(ctx, id)
	switch {
	case errors.Is(err, session.ErrNotFound):
		return core.Session{}, configErr("session %q: %w", id, err)
	case err != nil:
		return core.Session{}, fmt.Errorf("session %q: %w", id, err)
	case pathid.Key(sess.Cwd) != pathid.Key(s.d.WorkDir):
		return core.Session{}, configErr("session %s belongs to %s; re-run with --cwd %s", id, sess.Cwd, sess.Cwd)
	}
	return sess, nil
}

// agentFor returns the primary agent name, or a ConfigError naming the
// primary agents when it is unknown or not primary.
func (s *Service) agentFor(name string) (core.Agent, error) {
	primary := s.d.Agents.Primary()
	names := make([]string, 0, len(primary))
	for _, a := range primary {
		if a.Name == name {
			return a, nil
		}
		names = append(names, a.Name)
	}
	list := strings.Join(names, ", ")
	if _, ok := s.d.Agents.Get(name); ok {
		return core.Agent{}, configErr("agent %q is a subagent; primary agents: %s", name, list)
	}
	return core.Agent{}, configErr("unknown agent %q; primary agents: %s", name, list)
}

// commit creates the session if needed, stores the agent, model flag, and
// placeholder title on it, and starts the background title.
func (s *Service) commit(ctx context.Context, p *plan, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if p.isNew {
		sess, err := s.d.Sessions.Create(ctx, p.agent.Name, s.d.WorkDir)
		if err != nil {
			return err
		}
		p.sess = sess
	}

	dirty := false
	if p.sess.Agent != p.agent.Name {
		p.sess.Agent, dirty = p.agent.Name, true
	}
	if !p.flag.IsZero() && p.sess.Model != p.flag.String() {
		p.sess.Model, dirty = p.flag.String(), true
	}
	if p.sess.Title == "" {
		p.sess.Title, dirty, p.newTitle = session.PlaceholderTitle(text), true, true
	}
	if dirty {
		if err := s.d.Sessions.Update(ctx, p.sess); err != nil {
			return err
		}
	}
	if p.newTitle {
		s.wg.Add(1)
		go func(id core.SessionID) {
			defer s.wg.Done()
			_ = s.d.Sessions.GenerateTitle(s.base, id, text)
		}(p.sess.ID)
	}
	return nil
}

// Cancel cancels id's running turn, if any.
func (s *Service) Cancel(id core.SessionID) {
	s.d.Runner.Cancel(id)
}

// Close stops accepting sends, waits for background titles or ctx, then
// cancels the background context. It is idempotent.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	s.cancel()
	return err
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseOrZero(s string) core.ModelRef {
	ref, err := core.ParseModelRef(s)
	if err != nil {
		return core.ModelRef{}
	}
	return ref
}
