// Package session manages sessions and their messages: creation, lookup,
// the history the model sees, compaction, and background titles.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/pathid"
	"github.com/gammons/jig/internal/service/agent"
)

// Store persists sessions, messages, and todos.
type Store interface {
	CreateSession(ctx context.Context, sess core.Session) error
	UpdateSession(ctx context.Context, sess core.Session) error
	GetSession(ctx context.Context, id core.SessionID) (core.Session, error)
	ListSessions(ctx context.Context, parent core.SessionID, limit int) ([]core.Session, error)
	ListRootsByCwd(ctx context.Context, cwd string, limit int) ([]core.Session, error)
	SaveMessage(ctx context.Context, m core.Message) error
	ListMessages(ctx context.Context, id core.SessionID) ([]core.Message, error)
	ListTodos(ctx context.Context, id core.SessionID) ([]core.Todo, error)
	// IsNotFound reports whether err means the requested session does
	// not exist.
	IsNotFound(err error) bool
}

// ErrNotFound is wrapped by Get's error when the session does not exist.
var ErrNotFound = errors.New("session not found")

// Agents looks up agents, resolves the models they run on, and resolves
// model strings ("provider/model" or a [model_aliases] name).
type Agents interface {
	Get(name string) (core.Agent, bool)
	ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
	SmallModel(fallback core.ModelRef) core.ModelRef
	// ResolveRef resolves a model string against the agents service's
	// config, backing Configure's model validation.
	ResolveRef(s string) (core.ModelRef, error)
}

// Deps are the Service's collaborators.
type Deps struct {
	Store  Store
	LLMs   agent.LLMSource
	Agents Agents
	Bus    event.Publisher
	Clock  clock.Clock
	IDs    *ids.Gen
}

// Service implements core.SessionService, task.Sessions, and
// agent.History over a Store.
type Service struct {
	d Deps
	// mu serializes read-modify-write updates (Touch, GenerateTitle) so a
	// background title and a post-run touch cannot overwrite each other.
	mu sync.Mutex
}

// New returns a Service over d.
func New(d Deps) *Service {
	return &Service{d: d}
}

// Create starts a new root session for agentName in cwd and publishes
// event.SessionCreated.
func (s *Service) Create(ctx context.Context, agentName, cwd string) (core.Session, error) {
	now := s.d.Clock.Now()
	sess := core.Session{
		ID:        core.SessionID(s.d.IDs.Next("ses")),
		Agent:     agentName,
		Cwd:       cwd,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.d.Store.CreateSession(ctx, sess); err != nil {
		return core.Session{}, err
	}
	s.d.Bus.Publish(event.SessionCreated{Base: event.Base{SessionID: sess.ID, RootID: sess.ID}, Info: sess})
	return sess, nil
}

// CreateChild starts a subagent session under parent, titled title and
// sharing parent's working directory.
func (s *Service) CreateChild(ctx context.Context, parent core.SessionID, agentName string, model core.ModelRef, title string) (core.Session, error) {
	p, err := s.d.Store.GetSession(ctx, parent)
	if err != nil {
		return core.Session{}, fmt.Errorf("session: parent %s: %w", parent, err)
	}
	now := s.d.Clock.Now()
	sess := core.Session{
		ID:        core.SessionID(s.d.IDs.Next("ses")),
		ParentID:  parent,
		Title:     title,
		Agent:     agentName,
		Cwd:       p.Cwd,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if !model.IsZero() {
		sess.Model = model.String()
	}
	if err := s.d.Store.CreateSession(ctx, sess); err != nil {
		return core.Session{}, err
	}
	return sess, nil
}

// Get returns the session with id. When it does not exist, the error
// wraps ErrNotFound.
func (s *Service) Get(ctx context.Context, id core.SessionID) (core.Session, error) {
	sess, err := s.d.Store.GetSession(ctx, id)
	if err != nil && s.d.Store.IsNotFound(err) {
		return core.Session{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return sess, err
}

// Update stores sess's title, agent, model, effort, and UpdatedAt as given.
func (s *Service) Update(ctx context.Context, sess core.Session) error {
	return s.d.Store.UpdateSession(ctx, sess)
}

// Touch sets id's UpdatedAt to now.
func (s *Service) Touch(ctx context.Context, id core.SessionID) error {
	return s.modify(ctx, id, func(sess *core.Session) {
		sess.UpdatedAt = s.d.Clock.Now()
	})
}

// modify applies fn to a fresh copy of id and saves it, holding mu so
// concurrent modifications do not lose each other's fields.
func (s *Service) modify(ctx context.Context, id core.SessionID, fn func(*core.Session)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.d.Store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	fn(&sess)
	return s.d.Store.UpdateSession(ctx, sess)
}

// List returns root sessions, newest first, at most limit (all if <= 0).
func (s *Service) List(ctx context.Context, limit int) ([]core.Session, error) {
	return s.d.Store.ListSessions(ctx, "", limit)
}

// ListForCwd returns root sessions whose Cwd matches pathid.Key(cwd),
// newest first, at most limit (all if <= 0).
func (s *Service) ListForCwd(ctx context.Context, cwd string, limit int) ([]core.Session, error) {
	return s.d.Store.ListRootsByCwd(ctx, pathid.Key(cwd), limit)
}

// Messages returns every message in id, oldest first.
func (s *Service) Messages(ctx context.Context, id core.SessionID) ([]core.Message, error) {
	return s.d.Store.ListMessages(ctx, id)
}

// Todos returns id's current todo list.
func (s *Service) Todos(ctx context.Context, id core.SessionID) ([]core.Todo, error) {
	return s.d.Store.ListTodos(ctx, id)
}

// Rename sets id's title to title, trimmed and capped at 50 runes. An
// empty (after trimming) title is an error. It publishes
// event.SessionUpdated after a successful save.
func (s *Service) Rename(ctx context.Context, id core.SessionID, title string) error {
	title = capTitle(strings.TrimSpace(title))
	if title == "" {
		return errors.New("session: title must not be empty")
	}
	var saved core.Session
	if err := s.modify(ctx, id, func(sess *core.Session) {
		sess.Title = title
		saved = *sess
	}); err != nil {
		return err
	}
	s.d.Bus.Publish(event.SessionUpdated{Base: event.Base{SessionID: id, RootID: id}, Info: saved})
	return nil
}

// Configure sets id's agent and/or model, leaving a field unchanged when
// the corresponding argument is "". agent must name a known, primary
// (non-hidden, mode primary or all) agent; model must resolve via
// Agents.ResolveRef (a "provider/model" ref or a [model_aliases] name),
// and the resolved canonical "provider/model" string is what gets
// stored. With both "" it is a no-op: no store write, no event.
// Otherwise it publishes event.SessionUpdated after a successful save.
func (s *Service) Configure(ctx context.Context, id core.SessionID, agentName, model string) error {
	if agentName == "" && model == "" {
		return nil
	}
	if agentName != "" {
		if err := s.checkPrimaryAgent(agentName); err != nil {
			return err
		}
	}
	resolvedModel := ""
	if model != "" {
		ref, err := s.d.Agents.ResolveRef(model)
		if err != nil {
			return err
		}
		resolvedModel = ref.String()
	}
	var saved core.Session
	if err := s.modify(ctx, id, func(sess *core.Session) {
		if agentName != "" {
			sess.Agent = agentName
		}
		if resolvedModel != "" {
			sess.Model = resolvedModel
		}
		saved = *sess
	}); err != nil {
		return err
	}
	s.d.Bus.Publish(event.SessionUpdated{Base: event.Base{SessionID: id, RootID: id}, Info: saved})
	return nil
}

// SetEffort stores effort (a level on the scale, or "" to clear) as id's
// reasoning effort and publishes event.SessionUpdated after the save.
func (s *Service) SetEffort(ctx context.Context, id core.SessionID, effort core.Effort) error {
	if effort != "" && !effort.Known() {
		return fmt.Errorf("session: unknown effort %q", effort)
	}
	var saved core.Session
	if err := s.modify(ctx, id, func(sess *core.Session) {
		sess.Effort = effort
		saved = *sess
	}); err != nil {
		return err
	}
	s.d.Bus.Publish(event.SessionUpdated{Base: event.Base{SessionID: id, RootID: id}, Info: saved})
	return nil
}

// checkPrimaryAgent returns an error unless agentName names a known
// agent whose mode is primary or all, and is not hidden.
func (s *Service) checkPrimaryAgent(agentName string) error {
	a, ok := s.d.Agents.Get(agentName)
	if !ok {
		return fmt.Errorf("session: agent %q not found", agentName)
	}
	if a.Hidden || (a.Mode != core.ModePrimary && a.Mode != core.ModeAll) {
		return fmt.Errorf("session: agent %q is not a primary agent", agentName)
	}
	return nil
}

// parseOrZero parses a stored "provider/model" string, returning the zero
// ModelRef when it is empty or invalid.
func parseOrZero(s string) core.ModelRef {
	ref, err := core.ParseModelRef(s)
	if err != nil {
		return core.ModelRef{}
	}
	return ref
}

// smallModelFor resolves the model a hidden helper agent (title,
// compaction) runs on for sess: SmallModel of the agent's resolved model.
func (s *Service) smallModelFor(agentName string, sess core.Session) (core.Agent, core.ModelRef, error) {
	a, ok := s.d.Agents.Get(agentName)
	if !ok {
		return core.Agent{}, core.ModelRef{}, fmt.Errorf("session: agent %q not found", agentName)
	}
	resolved, err := s.d.Agents.ResolveModel(a, core.ModelRef{}, parseOrZero(sess.Model))
	if err != nil {
		return core.Agent{}, core.ModelRef{}, err
	}
	return a, s.d.Agents.SmallModel(resolved), nil
}

// complete runs agentName's prompt over user on sess's small model at its
// lowest effort level, returning the reply and the model used.
func (s *Service) complete(ctx context.Context, agentName string, sess core.Session, user string) (string, core.ModelRef, error) {
	a, model, err := s.smallModelFor(agentName, sess)
	if err != nil {
		return "", core.ModelRef{}, err
	}
	llm, info, err := s.d.LLMs.For(model)
	if err != nil {
		return "", core.ModelRef{}, err
	}
	out, err := agent.Complete(ctx, llm, a.Prompt, user, core.LowestEffort(info))
	if err != nil {
		return "", core.ModelRef{}, err
	}
	return out, model, nil
}
