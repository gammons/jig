// Package agentbrowser detects the agent-browser binary on PATH and runs
// its "skills path" subcommand to locate its bundled skills.
package agentbrowser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SkillsTimeout bounds how long SkillsPath waits for `<bin> skills path`.
const SkillsTimeout = 3 * time.Second

// maxSkillsOutput caps how much stdout SkillsPath will buffer, so a
// misbehaving binary cannot exhaust memory by writing unbounded output
// within SkillsTimeout.
const maxSkillsOutput = 4 * 1024

// ErrOutputTooLarge is boundedWriter's Write error once the cap is hit.
var ErrOutputTooLarge = errors.New("output too large")

// boundedWriter accumulates at most limit bytes, failing every Write once
// that cap would be exceeded instead of silently truncating. exceeded is
// checked directly (rather than relying on cmd.Run's returned error):
// once Write fails, the pipe closes and the writing process typically
// dies of a broken pipe or SIGPIPE, which os/exec reports as that
// process's own exit error, masking the copy error that caused it.
type boundedWriter struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		w.exceeded = true
		return 0, ErrOutputTooLarge
	}
	return w.buf.Write(p)
}

// Detect looks up "agent-browser" on PATH using lookPath (normally
// exec.LookPath), reporting its resolved path and whether it was found.
func Detect(lookPath func(string) (string, error)) (bin string, ok bool) {
	bin, err := lookPath("agent-browser")
	if err != nil {
		return "", false
	}
	return bin, true
}

// SkillsPath runs "<bin> skills path" and returns its trimmed stdout. The
// command is capped at SkillsTimeout regardless of ctx's own deadline,
// though ctx's deadline still applies if it is sooner.
func SkillsPath(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, SkillsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "skills", "path")
	out := &boundedWriter{limit: maxSkillsOutput}
	cmd.Stdout = out
	configureProcessGroup(cmd)
	err := cmd.Run()
	if out.exceeded {
		return "", fmt.Errorf("agentbrowser: skills path: %w", ErrOutputTooLarge)
	}
	if err != nil {
		return "", fmt.Errorf("agentbrowser: skills path: %w", err)
	}
	return strings.TrimSpace(out.buf.String()), nil
}
