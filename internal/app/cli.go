package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runOpts are the parsed flags and prompt of `jig run`.
type runOpts struct {
	agent   string
	model   string
	yes     bool
	session string
	cwd     string
	prompt  string
}

const runUsage = `usage: jig run [--agent A] [--model M] [--yes] [--session ID] [--cwd DIR] <prompt...>`

// ErrUsage reports a malformed command line; the usage has already been
// written.
var ErrUsage = errors.New("usage error")

// parseRun parses `jig run` args. Flag errors and an empty prompt write
// the usage to errw and return an error.
func parseRun(args []string, errw io.Writer) (runOpts, error) {
	var o runOpts
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { fmt.Fprintln(errw, runUsage) }
	fs.StringVar(&o.agent, "agent", "", "primary agent to run")
	fs.StringVar(&o.model, "model", "", "model as provider/model")
	fs.BoolVar(&o.yes, "yes", false, "allow every tool call that would ask for permission")
	fs.StringVar(&o.session, "session", "", "session ID to continue")
	fs.StringVar(&o.cwd, "cwd", "", "working directory (default: current directory)")
	if err := fs.Parse(args); err != nil {
		return runOpts{}, ErrUsage
	}
	o.prompt = strings.TrimSpace(strings.Join(fs.Args(), " "))
	if o.prompt == "" {
		fs.Usage()
		return runOpts{}, ErrUsage
	}
	return o, nil
}

// parseModels parses `jig models [provider]`.
func parseModels(args []string, errw io.Writer) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		return args[0], nil
	default:
		fmt.Fprintln(errw, "usage: jig models [provider]")
		return "", ErrUsage
	}
}
