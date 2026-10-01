// Package importer drives opencode sessions into jig's store: it skips
// sessions already present, turns subagents whose parent failed into
// roots, turns attached images into stored blobs, and tallies a summary.
// It declares small interfaces for the source, store, and media
// capabilities it needs, so it never imports concrete client/data types.
package importer

import (
	"context"
	"fmt"

	"github.com/gammons/jig/internal/core"
)

// Stats tallies what a session's translation skipped, dropped, or could
// not translate (the same fields as opencode.Stats).
type Stats struct {
	SkippedMessages       []string
	DroppedTypes          map[string]int
	UntranslatedTools     map[string]int
	UnimportedAttachments int
	EmptyMessages         int
}

// Item is one session as a Source yields it.
type Item struct {
	Session  core.Session
	Messages []core.Message
	Todos    []core.Todo
	Stats    Stats
}

// Source yields one Item per session, parents before children, stopping
// (and returning fn's error) as soon as fn returns a non-nil error.
type Source interface {
	Each(ctx context.Context, fn func(Item) error) error
}

// Store is the persistence capability Run needs. *store.Store satisfies
// it.
type Store interface {
	SessionExists(ctx context.Context, id core.SessionID) (bool, error)
	ImportSession(ctx context.Context, s core.Session, msgs []core.Message, todos []core.Todo) error
}

// Media turns raw image bytes into a bounded, stored core.Media.
// *media.Pipeline satisfies it.
type Media interface {
	Process(data []byte) (core.Media, core.ImageInfo, error)
}

// Options configures Run.
type Options struct {
	// DryRun reads and translates everything but writes nothing: no
	// Process or ImportSession call is made.
	DryRun bool
	// Progress, if non-nil, is called after every 100th item with the
	// number of items processed so far.
	Progress func(done int)
}

// Failure records a session that failed to import.
type Failure struct {
	ID  core.SessionID
	Err error
}

// Summary tallies the outcome of a Run.
type Summary struct {
	Imported       int
	Skipped        int
	OrphansAsRoots int
	Failed         []Failure
	Stats          Stats
}

// progressEvery is how often Options.Progress is called.
const progressEvery = 100

// Run drives src's items into st, converting media through med. The
// returned error is non-nil only for ctx cancellation or a Source error;
// per-session failures are recorded in the returned Summary instead.
func Run(ctx context.Context, src Source, st Store, med Media, opts Options) (Summary, error) {
	r := &runner{
		ctx:      ctx,
		st:       st,
		med:      med,
		opts:     opts,
		imported: map[core.SessionID]bool{},
		failed:   map[core.SessionID]bool{},
	}
	r.summary.Stats.DroppedTypes = map[string]int{}
	r.summary.Stats.UntranslatedTools = map[string]int{}

	err := src.Each(ctx, r.handle)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return r.summary, ctxErr
		}
		return r.summary, err
	}
	return r.summary, nil
}

// runner holds the state threaded through one Run.
type runner struct {
	ctx  context.Context
	st   Store
	med  Media
	opts Options

	summary  Summary
	imported map[core.SessionID]bool
	failed   map[core.SessionID]bool
	count    int
}

// handle processes one Item, returning a non-nil error only to stop the
// walk for ctx cancellation.
func (r *runner) handle(it Item) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}

	r.count++
	if r.opts.Progress != nil && r.count%progressEvery == 0 {
		r.opts.Progress(r.count)
	}

	r.summary.Stats = sumStats(r.summary.Stats, it.Stats)

	exists, err := r.st.SessionExists(r.ctx, it.Session.ID)
	if err != nil {
		return err
	}
	if exists {
		r.summary.Skipped++
		return nil
	}

	sess := it.Session
	if sess.ParentID != "" {
		orphan, err := r.isOrphan(sess.ParentID)
		if err != nil {
			return err
		}
		if orphan {
			sess.ParentID = ""
			r.summary.OrphansAsRoots++
		}
	}

	if r.opts.DryRun {
		r.imported[it.Session.ID] = true
		r.summary.Imported++
		return nil
	}

	msgs, err := r.convertMedia(it.Messages)
	if err != nil {
		return err
	}

	if err := r.st.ImportSession(r.ctx, sess, msgs, it.Todos); err != nil {
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		r.failed[it.Session.ID] = true
		r.summary.Failed = append(r.summary.Failed, Failure{ID: it.Session.ID, Err: err})
		return nil
	}

	r.imported[it.Session.ID] = true
	r.summary.Imported++
	return nil
}

// convertMedia returns a copy of msgs with every attachment's and tool
// result's media processed into a stored blob ref, leaving the source's
// own Message, Part, Attachment, and ToolResult values untouched. A
// Process failure replaces an attachment part with a PartText
// placeholder, or drops the result media and appends the placeholder
// text to the result's Output.
func (r *runner) convertMedia(msgs []core.Message) ([]core.Message, error) {
	out := make([]core.Message, len(msgs))
	for i, m := range msgs {
		if len(m.Parts) == 0 {
			out[i] = m
			continue
		}
		parts := make([]core.Part, len(m.Parts))
		for j, p := range m.Parts {
			np, err := r.convertPart(p)
			if err != nil {
				return nil, err
			}
			parts[j] = np
		}
		m.Parts = parts
		out[i] = m
	}
	return out, nil
}

// convertPart converts p's media, if any, returning a copy; p itself and
// anything it points to are left unmodified.
func (r *runner) convertPart(p core.Part) (core.Part, error) {
	switch {
	case p.Kind == core.PartAttachment && p.Attachment != nil && p.Attachment.Media != nil && p.Attachment.Media.Data != nil:
		return r.convertAttachment(p)
	case p.Kind == core.PartToolResult && p.Result != nil && len(p.Result.Media) > 0:
		return r.convertToolResult(p)
	default:
		return p, nil
	}
}

// convertAttachment processes att's Media and returns either p with a
// fresh *Attachment holding the stored media, or a PartText placeholder.
func (r *runner) convertAttachment(p core.Part) (core.Part, error) {
	data := p.Attachment.Media.Data
	media, _, err := r.med.Process(data)
	if err != nil {
		if cerr := r.ctx.Err(); cerr != nil {
			return core.Part{}, cerr
		}
		return core.Part{Kind: core.PartText, Text: placeholder(err)}, nil
	}
	att := *p.Attachment
	m := media
	att.Media = &m
	p.Attachment = &att
	return p, nil
}

// convertToolResult processes every media entry of p.Result, returning p
// with a fresh *ToolResult: successful ones are kept with their Data
// cleared, failed ones are dropped and their placeholder text appended to
// Output.
func (r *runner) convertToolResult(p core.Part) (core.Part, error) {
	res := *p.Result
	kept := make([]core.Media, 0, len(p.Result.Media))
	for _, m := range p.Result.Media {
		if m.Data == nil {
			kept = append(kept, m)
			continue
		}
		stored, _, err := r.med.Process(m.Data)
		if err != nil {
			if cerr := r.ctx.Err(); cerr != nil {
				return core.Part{}, cerr
			}
			res.Output += "\n" + placeholder(err)
			continue
		}
		kept = append(kept, stored)
	}
	res.Media = kept
	p.Result = &res
	return p, nil
}

// isOrphan reports whether parentID names a session that is neither
// imported in this run nor already present in the store: a failed
// parent, or one skipped because it already exists, is not an orphan
// (the latter's child keeps its ParentID). A SessionExists error is
// propagated rather than treated as "missing", the same way handle's own
// SessionExists error is handled.
func (r *runner) isOrphan(parentID core.SessionID) (bool, error) {
	if r.imported[parentID] {
		return false, nil
	}
	if r.failed[parentID] {
		return true, nil
	}
	exists, err := r.st.SessionExists(r.ctx, parentID)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// sumStats adds b into a, returning the result; a's maps are non-nil in
// the result even when both inputs start nil.
func sumStats(a, b Stats) Stats {
	if a.DroppedTypes == nil {
		a.DroppedTypes = map[string]int{}
	}
	if a.UntranslatedTools == nil {
		a.UntranslatedTools = map[string]int{}
	}
	a.SkippedMessages = append(a.SkippedMessages, b.SkippedMessages...)
	for k, v := range b.DroppedTypes {
		a.DroppedTypes[k] += v
	}
	for k, v := range b.UntranslatedTools {
		a.UntranslatedTools[k] += v
	}
	a.UnimportedAttachments += b.UnimportedAttachments
	a.EmptyMessages += b.EmptyMessages
	return a
}

// placeholder formats the text jig stores in place of an image that
// failed to import.
func placeholder(err error) string {
	return fmt.Sprintf("[image not imported: %v]", err)
}
