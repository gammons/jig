package agent

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// ErrProxyUnset is returned by Proxy.Run before Set has been called.
var ErrProxyUnset = errors.New("agent: proxy runner not set")

// Proxy late-binds a Runner for the task tool, which must be registered
// before the registry is frozen and the Runner built. It is the only
// sanctioned setter-style wiring: Set panics if called twice.
type Proxy struct {
	r atomic.Pointer[Runner]
}

// Set binds r. It panics if r is nil or a Runner is already bound.
func (p *Proxy) Set(r *Runner) {
	if r == nil {
		panic("agent: Proxy.Set with nil Runner")
	}
	if !p.r.CompareAndSwap(nil, r) {
		panic("agent: Proxy.Set called twice")
	}
}

// Run delegates to the bound Runner.
func (p *Proxy) Run(ctx context.Context, rc ext.RunContext, userText string) (core.Message, error) {
	r := p.r.Load()
	if r == nil {
		return core.Message{}, ErrProxyUnset
	}
	return r.Run(ctx, rc, userText)
}
