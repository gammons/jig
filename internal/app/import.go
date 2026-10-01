package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/blobfs"
	"github.com/gammons/jig/internal/data/opencode"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/importer"
	"github.com/gammons/jig/internal/service/media"
)

// importUsage is `jig import`'s usage line, printed for a missing or
// unknown source.
const importUsage = `usage: jig import opencode [--db PATH] [--dry-run]`

// importOpts are the parsed flags of `jig import opencode`.
type importOpts struct {
	db     string
	dryRun bool
}

// importCmd implements `jig import opencode`.
func importCmd(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	if len(args) == 0 || args[0] != "opencode" {
		fmt.Fprintln(std.Err, importUsage)
		return exitConfig
	}
	opts, err := parseImportOpencode(args[1:], std.Err, getenv)
	if err != nil {
		return exitConfig
	}
	return runImportOpencode(ctx, opts, std, getenv)
}

// parseImportOpencode parses `jig import opencode`'s flags.
func parseImportOpencode(args []string, errw io.Writer, getenv func(string) string) (importOpts, error) {
	var o importOpts
	fs := flag.NewFlagSet("import opencode", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { fmt.Fprintln(errw, importUsage) }
	fs.StringVar(&o.db, "db", defaultOpencodeDB(getenv), "path to opencode's SQLite database")
	fs.BoolVar(&o.dryRun, "dry-run", false, "read and translate everything but write nothing")
	if err := fs.Parse(args); err != nil {
		return importOpts{}, ErrUsage
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return importOpts{}, ErrUsage
	}
	return o, nil
}

// defaultOpencodeDB returns "<opencode data dir>/opencode.db", where the
// data dir is $XDG_DATA_HOME/opencode, else ~/.local/share/opencode.
func defaultOpencodeDB(getenv func(string) string) string {
	base := getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(getenv("HOME"), ".local", "share")
	}
	return filepath.Join(base, "opencode", "opencode.db")
}

// runImportOpencode builds the store, the opencode source, and the media
// pipeline, drives importer.Run, and prints the summary.
func runImportOpencode(ctx context.Context, opts importOpts, std Stdio, getenv func(string) string) int {
	e, err := loadEnv("", getenv, staticTrust(false))
	if err != nil {
		printLine(std.Err, err.Error())
		return exitConfig
	}
	st, closeStore, err := importStoreFor(ctx, e, opts.dryRun)
	if err != nil {
		printLine(std.Err, "error: "+err.Error())
		return exitRunFailed
	}
	defer closeStore()

	src, err := openOpencodeSource(ctx, e, opts.db, std.Err)
	if err != nil {
		printLine(std.Err, err.Error())
		return exitConfig
	}
	defer src.Close()

	pipeline := media.New(blobfs.New(e.blobsDir()))
	summary, err := importer.Run(ctx, sourceAdapter{src}, st, pipeline, importer.Options{
		DryRun:   opts.dryRun,
		Progress: func(done int) { printLine(std.Err, fmt.Sprintf("imported %d sessions…", done)) },
	})
	if err != nil {
		printLine(std.Err, "error: "+err.Error())
		return exitRunFailed
	}

	printSummary(std.Out, summary, opts.dryRun)
	if len(summary.Failed) > 0 {
		return exitRunFailed
	}
	return exitOK
}

// importStoreFor returns the importer.Store to use for this run, and a
// func to release it. A non-dry run (or a dry run whose jig.db already
// exists) opens the real store, so "already present" counts are accurate.
// A dry run with no jig.db yet opens none: openStore would create the
// file and run migrations, which a dry run must never do (spec §3).
func importStoreFor(ctx context.Context, e env, dryRun bool) (importer.Store, func(), error) {
	if dryRun {
		if _, err := os.Stat(jigDBPath(e)); errors.Is(err, os.ErrNotExist) {
			return noStoreYet{}, func() {}, nil
		}
	}
	st, err := openStore(ctx, e)
	if err != nil {
		return nil, nil, err
	}
	return st, func() { st.Close() }, nil
}

// noStoreYet is the importer.Store used for a dry run when jig.db does
// not exist yet: every session counts as new, and nothing is ever
// written (a dry run never calls ImportSession; the error is a safety
// net if that ever changes).
type noStoreYet struct{}

func (noStoreYet) SessionExists(context.Context, core.SessionID) (bool, error) {
	return false, nil
}

func (noStoreYet) ImportSession(context.Context, core.Session, []core.Message, []core.Todo) error {
	return fmt.Errorf("import: no jig database open (dry run)")
}

// openOpencodeSource builds jig's known agent names (primary and
// subagent) and opens the opencode database at dbPath.
func openOpencodeSource(ctx context.Context, e env, dbPath string, errw io.Writer) (*opencode.Source, error) {
	disc := discover(e, errw)
	ag, err := agents.New(e.cfg(), disc.sources)
	if err != nil {
		return nil, err
	}
	return opencode.Open(ctx, dbPath, opencode.Options{Agents: agentNames(ag)})
}

// agentNames returns every primary and subagent name ag knows.
func agentNames(ag *agents.Service) []string {
	var names []string
	for _, a := range ag.Primary() {
		names = append(names, a.Name)
	}
	for _, a := range ag.Subagents() {
		names = append(names, a.Name)
	}
	return names
}

// sourceAdapter adapts *opencode.Source to importer.Source, so the
// importer package never imports data/opencode's concrete types.
type sourceAdapter struct{ src *opencode.Source }

// Each copies each opencode.Item into an importer.Item and forwards it.
func (a sourceAdapter) Each(ctx context.Context, fn func(importer.Item) error) error {
	return a.src.Each(ctx, func(it opencode.Item) error {
		return fn(importer.Item{
			Session:  it.Session,
			Messages: it.Messages,
			Todos:    it.Todos,
			Stats: importer.Stats{
				SkippedMessages:       it.Stats.SkippedMessages,
				DroppedTypes:          it.Stats.DroppedTypes,
				UntranslatedTools:     it.Stats.UntranslatedTools,
				UnimportedAttachments: it.Stats.UnimportedAttachments,
				EmptyMessages:         it.Stats.EmptyMessages,
			},
		})
	})
}

// printSummary writes s as the end-of-run summary (spec §3/§6), sanitizing
// every line (session IDs, error text, and tool/message names may hold
// opencode-derived text).
func printSummary(w io.Writer, s importer.Summary, dryRun bool) {
	if dryRun {
		printLine(w, "dry run: nothing was written")
	}
	printLine(w, fmt.Sprintf("imported: %d", s.Imported))
	printLine(w, fmt.Sprintf("already present: %d", s.Skipped))
	printLine(w, fmt.Sprintf("failed: %d", len(s.Failed)))
	for _, f := range s.Failed {
		printLine(w, fmt.Sprintf("  %s: %s", f.ID, f.Err))
	}
	printLine(w, fmt.Sprintf("subagents imported as roots: %d", s.OrphansAsRoots))
	printSkippedMessages(w, s.Stats.SkippedMessages)
	printCountMap(w, "dropped message types:", s.Stats.DroppedTypes)
	printCountMap(w, "untranslated tools:", s.Stats.UntranslatedTools)
	printLine(w, fmt.Sprintf("attachments not imported: %d", s.Stats.UnimportedAttachments))
	printLine(w, fmt.Sprintf("empty messages dropped: %d", s.Stats.EmptyMessages))
}

// printSkippedMessages writes the "messages skipped" count and one
// indented line per entry.
func printSkippedMessages(w io.Writer, msgs []string) {
	printLine(w, fmt.Sprintf("messages skipped: %d", len(msgs)))
	for _, m := range msgs {
		printLine(w, "  "+m)
	}
}

// printCountMap writes header followed by one indented "  <key>: <n>" line
// per entry, sorted by key. It writes nothing when m is empty.
func printCountMap(w io.Writer, header string, m map[string]int) {
	if len(m) == 0 {
		return
	}
	printLine(w, header)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		printLine(w, fmt.Sprintf("  %s: %d", k, m[k]))
	}
}
