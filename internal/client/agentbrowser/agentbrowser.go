// Package agentbrowser detects the agent-browser binary on PATH and runs
// its "skills path" subcommand to locate its bundled skills.
package agentbrowser

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SkillsTimeout bounds how long SkillsPath waits for `<bin> skills path`.
const SkillsTimeout = 3 * time.Second

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
	var out bytes.Buffer
	cmd.Stdout = &out
	configureProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("agentbrowser: skills path: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}
