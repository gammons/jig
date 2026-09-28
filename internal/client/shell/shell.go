// Package shell runs shell commands through bash (falling back to sh),
// capturing combined stdout+stderr and enforcing an optional timeout on
// top of whatever context the caller supplies.
package shell

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Spec describes a command to run. If SpillPath is set, the full combined
// output is streamed to that file as it is produced (created 0o600),
// independent of TailBytes. If TailBytes is positive, memory retains only
// the last TailBytes bytes of output instead of the whole thing. A
// SpillPath that cannot be opened or written does not fail the command;
// see Result.SpillErr.
type Spec struct {
	Command   string
	Dir       string
	Timeout   time.Duration
	SpillPath string
	TailBytes int
}

// Result is the outcome of running a Spec. Output is stdout and stderr
// combined into a single buffer, in the order the process wrote to them,
// bounded to the last TailBytes bytes when Spec.TailBytes was positive.
// TotalBytes is the full combined size actually produced, and Truncated
// reports whether Output is missing some of it. SpillErr is set if
// Spec.SpillPath was set but could not be opened or fully written to; the
// command still runs either way.
type Result struct {
	Output     []byte
	ExitCode   int
	TimedOut   bool
	Truncated  bool
	TotalBytes int64
	SpillErr   error
}

// Runner runs shell commands via bash -c (sh -c if bash is not on PATH).
type Runner struct{}

// Run runs s.Command in a new session (on unix: no controlling terminal,
// its own process group). If s.Timeout is positive
// and the command has not finished by then, the process group is killed
// and Run returns a partial Result with TimedOut set and a nil error. If
// ctx is done for any other reason, Run returns a partial Result and
// ctx.Err(). Otherwise Run returns the command's exit code with a nil
// error, even for a non-zero exit.
func (Runner) Run(ctx context.Context, s Spec) (Result, error) {
	runCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, shellName(), "-c", s.Command)
	cmd.Dir = s.Dir

	cp := newCapture(s.TailBytes, s.SpillPath)
	cmd.Stdout = cp
	cmd.Stderr = cp
	configureProcessGroup(cmd)

	err := cmd.Run()
	_ = cp.Close()

	build := func(exitCode int, timedOut bool) Result {
		return Result{
			Output:     cp.tail(),
			ExitCode:   exitCode,
			TimedOut:   timedOut,
			Truncated:  s.TailBytes > 0 && cp.total > int64(s.TailBytes),
			TotalBytes: cp.total,
			SpillErr:   cp.SpillErr(),
		}
	}

	if s.Timeout > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return build(-1, true), nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return build(-1, false), ctxErr
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return build(exitErr.ExitCode(), false), nil
	}
	if err != nil {
		return build(0, false), fmt.Errorf("shell: %w", err)
	}
	return build(0, false), nil
}

// shellName returns "bash" if it is on PATH, else "sh"; exec.Command
// resolves either via the child process's own PATH lookup.
func shellName() string {
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}
