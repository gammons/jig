package permission

import (
	"context"
	"fmt"
	"sync"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ids"
)

// Request describes a single permission decision an Asker must make.
type Request struct {
	SessionID core.SessionID
	Tool      string
	Subject   string
	Call      core.ToolCall
}

// Asker asks something (a UI, or a fixed policy) for a decision on a
// pending permission Request.
type Asker interface {
	Ask(ctx context.Context, req Request) (core.PermissionReply, error)
}

// pendingRequest tracks the state Reply needs to resolve an outstanding
// Ask call: the channel its goroutine is blocked reading, and the session
// the PermissionResolved event belongs to.
type pendingRequest struct {
	ch        chan core.PermissionReply
	sessionID core.SessionID
}

// BusAsker is an Asker that publishes PermissionRequested on an
// event.Publisher and blocks until a matching Reply call (or context
// cancellation) resolves it. It also implements core.PermissionService,
// so a UI can drive it directly. BusAsker is safe for concurrent use.
type BusAsker struct {
	pub event.Publisher
	ids *ids.Gen

	mu      sync.Mutex
	pending map[string]pendingRequest
}

// NewBusAsker returns a BusAsker that publishes on pub and mints request
// IDs from idGen.
func NewBusAsker(pub event.Publisher, idGen *ids.Gen) *BusAsker {
	return &BusAsker{
		pub:     pub,
		ids:     idGen,
		pending: make(map[string]pendingRequest),
	}
}

// Ask publishes a PermissionRequested event and blocks until Reply is
// called for the request's ID, or ctx is done, whichever comes first. On
// ctx cancellation the pending entry is removed before Ask returns, so a
// later Reply for the same ID errors instead of blocking.
func (b *BusAsker) Ask(ctx context.Context, req Request) (core.PermissionReply, error) {
	id := b.ids.Next("perm")
	ch := make(chan core.PermissionReply, 1)

	b.mu.Lock()
	b.pending[id] = pendingRequest{ch: ch, sessionID: req.SessionID}
	b.mu.Unlock()

	b.pub.Publish(event.PermissionRequested{
		Base:      event.Base{SessionID: req.SessionID},
		RequestID: id,
		Tool:      req.Tool,
		Subject:   req.Subject,
		Call:      req.Call,
	})

	select {
	case reply := <-ch:
		return reply, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return core.PermissionReply{}, ctx.Err()
	}
}

// Reply resolves the pending request named requestID, publishing
// PermissionResolved and unblocking the matching Ask call. It returns an
// error for an unknown or already-resolved id.
func (b *BusAsker) Reply(requestID string, r core.PermissionReply) error {
	b.mu.Lock()
	p, ok := b.pending[requestID]
	if ok {
		delete(b.pending, requestID)
	}
	b.mu.Unlock()

	if !ok {
		return fmt.Errorf("permission: unknown request %q", requestID)
	}

	p.ch <- r
	b.pub.Publish(event.PermissionResolved{
		Base:      event.Base{SessionID: p.sessionID},
		RequestID: requestID,
		Reply:     r,
	})
	return nil
}

// Pending returns the number of outstanding requests: those published but
// not yet resolved by Reply or dropped by ctx cancellation.
func (b *BusAsker) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// StaticAsker is a fixed-policy Asker for headless runs: it never actually
// asks anyone.
type StaticAsker struct {
	// Allow, when true, replies ReplyOnce to every request. When false, it
	// replies ReplyDeny, explaining how to allow the run to proceed
	// unattended.
	Allow bool
}

// Ask implements Asker without consulting req or ctx.
func (a StaticAsker) Ask(_ context.Context, _ Request) (core.PermissionReply, error) {
	if a.Allow {
		return core.PermissionReply{Kind: core.ReplyOnce}, nil
	}
	return core.PermissionReply{
		Kind:    core.ReplyDeny,
		Message: "permission required (re-run with --yes)",
	}, nil
}
