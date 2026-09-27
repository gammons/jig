package app

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/client/search"
	"github.com/gammons/jig/internal/client/shell"
	"github.com/gammons/jig/internal/data/contextfs"
	"github.com/gammons/jig/internal/data/frontmatter"
	"github.com/gammons/jig/internal/data/skillfs"
	"github.com/gammons/jig/internal/service/prompt"
	"github.com/gammons/jig/internal/service/tools"
)

// shellAdapter adapts shell.Runner to tools.Shell.
type shellAdapter struct{ r shell.Runner }

func (a shellAdapter) Run(ctx context.Context, s tools.ShellSpec) (tools.ShellResult, error) {
	res, err := a.r.Run(ctx, shell.Spec{
		Command:   s.Command,
		Dir:       s.Dir,
		Timeout:   s.Timeout,
		SpillPath: s.SpillPath,
		TailBytes: s.TailBytes,
	})
	return tools.ShellResult{
		Output:     res.Output,
		ExitCode:   res.ExitCode,
		TimedOut:   res.TimedOut,
		Truncated:  res.Truncated,
		TotalBytes: res.TotalBytes,
		SpillErr:   res.SpillErr,
	}, err
}

// searchAdapter adapts search.Searcher to tools.Searcher.
type searchAdapter struct{ s search.Searcher }

func (a searchAdapter) Glob(ctx context.Context, dir, pattern string, limit int) ([]string, error) {
	return a.s.Glob(ctx, dir, pattern, limit)
}

func (a searchAdapter) Grep(ctx context.Context, dir, pattern, include string, limit int) ([]tools.Match, error) {
	ms, err := a.s.Grep(ctx, dir, pattern, include, limit)
	out := make([]tools.Match, len(ms))
	for i, m := range ms {
		out[i] = tools.Match{Path: m.Path, Line: m.Line, Text: m.Text}
	}
	return out, err
}

// skillFS implements skills.FS over the OS filesystem.
type skillFS struct{}

// ReadBody returns the SKILL.md at path without its frontmatter.
func (skillFS) ReadBody(path string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var meta map[string]any
	return frontmatter.Parse(src, &meta)
}

func (skillFS) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}

// promptFiles converts contextfs files to prompt files.
func promptFiles(files []contextfs.File) []prompt.File {
	out := make([]prompt.File, len(files))
	for i, f := range files {
		out[i] = prompt.File{Path: f.Path, Content: f.Content}
	}
	return out
}

// printWarnings writes discovery warnings as "warning: <path>: <msg>".
func printWarnings(w io.Writer, ws []skillfs.Warning) {
	for _, wn := range ws {
		fmt.Fprintf(w, "warning: %s: %s\n", wn.Path, wn.Msg)
	}
}
