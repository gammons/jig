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
	agent        string
	model        string
	yes          bool
	trustProject bool
	session      string
	cwd          string
	attach       []string
	prompt       string
}

const runUsage = `usage: jig run [--agent A] [--model M] [--yes] [--trust-project] [--session ID] [--cwd DIR] [--attach PATH]... <prompt...>`

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
	fs.BoolVar(&o.trustProject, "trust-project", false, "trust this project's config (and remember it until the config changes)")
	fs.StringVar(&o.session, "session", "", "session ID to continue")
	fs.StringVar(&o.cwd, "cwd", "", "working directory (default: current directory)")
	fs.Func("attach", "attach a file to the prompt (repeatable)", func(v string) error {
		o.attach = append(o.attach, v)
		return nil
	})
	words, err := parseInterspersed(fs, args)
	if err != nil {
		return runOpts{}, ErrUsage
	}
	o.prompt = strings.TrimSpace(strings.Join(words, " "))
	if o.prompt == "" {
		fs.Usage()
		return runOpts{}, ErrUsage
	}
	return o, nil
}

// parseInterspersed parses fs's flags anywhere in args and returns the
// positional args in order. A "--" ends flag parsing: everything after it
// is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(words, rest...), nil
		}
		if len(rest) == 0 {
			return words, nil
		}
		words = append(words, rest[0])
		args = rest[1:]
	}
}

// tuiOpts are the parsed flags of `jig` (the TUI).
type tuiOpts struct {
	cwd, session string
	trustProject bool
}

const tuiUsage = `usage: jig [--cwd DIR] [--session ID] [--trust-project]`

// parseTUI parses `jig`'s flags. A flag error or any positional arg
// writes the usage to errw and returns ErrUsage.
func parseTUI(args []string, errw io.Writer) (tuiOpts, error) {
	var o tuiOpts
	fs := flag.NewFlagSet("jig", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { fmt.Fprintln(errw, tuiUsage) }
	fs.StringVar(&o.cwd, "cwd", "", "working directory (default: current directory)")
	fs.StringVar(&o.session, "session", "", "session ID to resume")
	fs.BoolVar(&o.trustProject, "trust-project", false, "trust this project's config (and remember it until the config changes)")
	if err := fs.Parse(args); err != nil {
		return tuiOpts{}, ErrUsage
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return tuiOpts{}, ErrUsage
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
