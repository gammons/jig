// Package session manages sessions and their messages: creation, lookup,
// the history the model sees, compaction, and background titles.
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agent"
)

// Store persists sessions and messages.
type Store interface {
	CreateSession(ctx context.Context, sess core.Session) error
	UpdateSession(ctx context.Context, sess core.Session) error
	GetSession(ctx context.Context, id core.SessionID) (core.Session, error)
	ListSessions(ctx context.Context, parent core.SessionID, limit int) ([]core.Session, error)
	SaveMessage(ctx context.Context, m core.Message) error
	ListMessages(ctx context.Context, id core.SessionID) ([]core.Message, error)
	// IsNotFound reports whether err means the requested session does
	// not exist.
	IsNotFound(err error) bool
}

// ErrNotFound is wrapped by Get's error when the session does not exist.
var ErrNotFound = errors.New("session not found")

// Agents looks up agents and resolves the models they run on.
type Agents interface {
	Get(name string) (core.Agent, bool)
	ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
	SmallModel(fallback core.ModelRef) core.ModelRef
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
	s.d.Bus.Publish(event.SessionCreated{Base: event.Base{SessionID: sess.ID}, Info: sess})
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

// Update stores sess's title, agent, model, and UpdatedAt as given.
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

// Messages returns every message in id, oldest first.
func (s *Service) Messages(ctx context.Context, id core.SessionID) ([]core.Message, error) {
	return s.d.Store.ListMessages(ctx, id)
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

// complete runs agentName's prompt over user on sess's small model,
// returning the reply and the model used.
func (s *Service) complete(ctx context.Context, agentName string, sess core.Session, user string) (string, core.ModelRef, error) {
	a, model, err := s.smallModelFor(agentName, sess)
	if err != nil {
		return "", core.ModelRef{}, err
	}
	llm, _, err := s.d.LLMs.For(model)
	if err != nil {
		return "", core.ModelRef{}, err
	}
	out, err := agent.Complete(ctx, llm, a.Prompt, user)
	if err != nil {
		return "", core.ModelRef{}, err
	}
	return out, model, nil
}
