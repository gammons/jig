// Package shell runs shell commands through bash (falling back to sh),
// capturing combined stdout+stderr and enforcing an optional timeout on
// top of whatever context the caller supplies.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Spec describes a command to run.
type Spec struct {
	Command string
	Dir     string
	Timeout time.Duration
}

// Result is the outcome of running a Spec. Output is stdout and stderr
// combined into a single buffer, in the order the process wrote to them.
type Result struct {
	Output   []byte
	ExitCode int
	TimedOut bool
}

// Runner runs shell commands via bash -c (sh -c if bash is not on PATH).
type Runner struct{}

// Run runs s.Command in a fresh process group. If s.Timeout is positive
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

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	configureProcessGroup(cmd)

	err := cmd.Run()

	if s.Timeout > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return Result{Output: out.Bytes(), ExitCode: -1, TimedOut: true}, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Result{Output: out.Bytes(), ExitCode: -1}, ctxErr
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return Result{Output: out.Bytes(), ExitCode: exitErr.ExitCode()}, nil
	}
	if err != nil {
		return Result{Output: out.Bytes()}, fmt.Errorf("shell: %w", err)
	}
	return Result{Output: out.Bytes(), ExitCode: 0}, nil
}

// shellName returns "bash" if it is on PATH, else "sh"; exec.Command
// resolves either via the child process's own PATH lookup.
func shellName() string {
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}
