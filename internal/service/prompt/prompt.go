// Package prompt provides the ContextTransforms that assemble a run's
// system prompt: the active agent's own prompt, an environment summary,
// and any AGENTS.md / instructions files discovered for the working
// directory.
package prompt

import (
	"context"
	"fmt"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// File is a context file (an AGENTS.md or an instructions file) to inject
// into the system prompt.
type File struct {
	Path    string
	Content string
}

// agentPromptTransform implements ext.ContextTransform, appending the
// active agent's own Prompt.
type agentPromptTransform struct{}

// AgentPrompt returns the priority-0 ContextTransform that appends
// rc.Agent.Prompt, if it is non-empty.
func AgentPrompt() ext.ContextTransform { return agentPromptTransform{} }

func (agentPromptTransform) Priority() int { return 0 }

func (agentPromptTransform) Transform(_ context.Context, rc ext.RunContext, req *core.LLMRequest) error {
	if rc.Agent.Prompt == "" {
		return nil
	}
	req.System = append(req.System, rc.Agent.Prompt)
	return nil
}

// envTransform implements ext.ContextTransform, appending a summary of the
// run's environment.
type envTransform struct {
	clk      clock.Clock
	platform string
	isGit    func(dir string) bool
}

// dateLayout is Go's reference-time layout for Env's "Today's date" line.
const dateLayout = "Mon Jan 2 2006"

// Env returns the priority-10 ContextTransform that appends an <env> block
// describing rc.WorkDir, platform, whether rc.WorkDir is a git repo (per
// isGit), and the current date (per clk).
func Env(clk clock.Clock, platform string, isGit func(dir string) bool) ext.ContextTransform {
	return envTransform{clk: clk, platform: platform, isGit: isGit}
}

func (envTransform) Priority() int { return 10 }

func (e envTransform) Transform(_ context.Context, rc ext.RunContext, req *core.LLMRequest) error {
	gitStatus := "no"
	if e.isGit(rc.WorkDir) {
		gitStatus = "yes"
	}
	req.System = append(req.System, fmt.Sprintf(
		"<env>\n  Working directory: %s\n  Platform: %s\n  Is git repo: %s\n  Today's date: %s\n</env>",
		rc.WorkDir, e.platform, gitStatus, e.clk.Now().Format(dateLayout),
	))
	return nil
}

// filesTransform implements ext.ContextTransform, appending one system
// string per File. Instructions and AgentsMD share this implementation:
// they differ only in Priority.
type filesTransform struct {
	files    []File
	priority int
}

// Instructions returns the priority-20 ContextTransform that appends one
// system string per file in files. It appends nothing if files is empty.
func Instructions(files []File) ext.ContextTransform {
	return filesTransform{files: files, priority: 20}
}

// AgentsMD returns the priority-30 ContextTransform that appends one
// system string per file in files. It appends nothing if files is empty.
func AgentsMD(files []File) ext.ContextTransform {
	return filesTransform{files: files, priority: 30}
}

func (f filesTransform) Priority() int { return f.priority }

func (f filesTransform) Transform(_ context.Context, _ ext.RunContext, req *core.LLMRequest) error {
	for _, file := range f.files {
		req.System = append(req.System, fmt.Sprintf("Instructions from: %s\n%s", file.Path, file.Content))
	}
	return nil
}
