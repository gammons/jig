package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/service/permission"
	"github.com/gammons/jig/internal/ui/plain"
)

// closeTimeout bounds how long a finished run waits for background work
// (session titles) before exiting.
const closeTimeout = 3 * time.Second

// runCmd implements `jig run`.
func runCmd(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	opts, err := parseRun(args, std.Err)
	if err != nil {
		return exitConfig
	}
	e, err := loadEnv(opts.cwd, getenv, staticTrust(opts.trustProject))
	if err != nil {
		fmt.Fprintln(std.Err, err)
		return exitConfig
	}
	if e.trust.warn() {
		fmt.Fprintln(std.Err, untrustedWarning)
	}
	warnProviderOptions(std.Err, e.cfg().Providers)
	rt, err := newRuntime(ctx, e, permission.StaticAsker{Allow: opts.yes}, std.Err)
	if err != nil {
		fmt.Fprintln(std.Err, "error:", err)
		return exitCode(err)
	}
	defer rt.close()
	return rt.headless(ctx, opts, std)
}

// drained marks the end of a run's events: once the renderer's feed
// reaches it, every earlier event has been rendered.
type drained struct{ event.Base }

// headless sends opts.prompt, rendering bus events as they arrive, and
// returns the exit code. Send's error is printed only when the renderer
// has not already reported it as the root session's RunFailed.
func (rt *runtime) headless(ctx context.Context, opts runOpts, std Stdio) int {
	r := startRendering(rt.bus, std.Out, std.Err)
	res, err := rt.chat.Send(ctx, core.SendRequest{
		SessionID:   core.SessionID(opts.session),
		Agent:       opts.agent,
		Model:       opts.model,
		Text:        opts.prompt,
		Attachments: opts.attach,
	})

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	_ = rt.chat.Close(closeCtx)
	cancel()
	reported := r.finish()

	if err == nil {
		return exitOK
	}
	if !reported[res.SessionID] {
		fmt.Fprintln(std.Err, "error:", err)
	}
	return exitCode(err)
}

// rendering is a plain renderer fed from a bus subscription.
type rendering struct {
	bus    *event.Bus
	sub    *event.Subscription
	failed chan map[core.SessionID]bool
	done   chan struct{}
}

// startRendering subscribes to bus and renders its events to out and
// errw until finish is called.
func startRendering(bus *event.Bus, out, errw io.Writer) *rendering {
	r := &rendering{
		bus:    bus,
		sub:    bus.Subscribe(),
		failed: make(chan map[core.SessionID]bool, 1),
		done:   make(chan struct{}),
	}
	feed := make(chan event.Event)
	go forward(r.sub.C(), feed, r.failed)
	go func() {
		plain.New(out, errw).Run(feed)
		close(r.done)
	}()
	return r
}

// finish renders every event published before it was called, then stops
// rendering and returns the sessions that published RunFailed. Closing
// the subscription discards undelivered events, so finish first publishes
// a drained marker and waits for the renderer's feed to reach it.
func (r *rendering) finish() map[core.SessionID]bool {
	r.bus.Publish(drained{})
	reported := <-r.failed
	r.sub.Close()
	<-r.done
	return reported
}

// forward copies events from in to out until it sees drained, then
// closes out and sends the set of sessions that published RunFailed.
func forward(in <-chan event.Event, out chan<- event.Event, failed chan<- map[core.SessionID]bool) {
	seen := make(map[core.SessionID]bool)
	defer func() {
		close(out)
		failed <- seen
	}()
	for e := range in {
		if _, ok := e.(drained); ok {
			return
		}
		if f, ok := e.(event.RunFailed); ok {
			seen[f.SessionID] = true
		}
		out <- e
	}
}
