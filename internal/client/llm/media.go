package llm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// maxRequestImages is how many media, counted from the end of a request,
// are loaded and sent; older ones get droppedNote instead.
const maxRequestImages = 20

const droppedNote = "[image omitted: earlier image; re-read or re-take it if needed]"

// BlobReader reads a stored blob by ref. *blobfs.Store satisfies it.
type BlobReader interface {
	Open(ref string) ([]byte, error)
}

// mediaLLM resolves each request's media before handing it to inner: an
// image-capable model gets Media.Data loaded from blobs for the newest
// maxRequestImages media (older ones get droppedNote); any other model
// gets a text placeholder in place of its media. The caller's messages
// are never mutated (they may alias the store's cached values): every
// message, part, result, and attachment it changes is a copy.
type mediaLLM struct {
	inner  core.LLM
	blobs  BlobReader
	images bool
	model  string // provider/model, for the omitted placeholder
}

// withMedia wraps inner so its requests' media are resolved for a model
// named model that does (images) or does not accept images.
func withMedia(inner core.LLM, blobs BlobReader, images bool, model string) core.LLM {
	return &mediaLLM{inner: inner, blobs: blobs, images: images, model: model}
}

func (m *mediaLLM) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	req.Messages = m.resolveMessages(req.Messages)
	return m.inner.Stream(ctx, req)
}

// resolver resolves one request's media; skip counts down the oldest
// media still to be dropped rather than loaded.
type resolver struct {
	m    *mediaLLM
	skip int
}

// resolveMessages returns msgs with every medium resolved, copying only
// the messages that hold media.
func (m *mediaLLM) resolveMessages(msgs []core.Message) []core.Message {
	r := &resolver{m: m, skip: max(0, countMedia(msgs)-maxRequestImages)}
	var out []core.Message
	for i, msg := range msgs {
		parts, changed := r.resolveParts(msg.Parts)
		if !changed {
			continue
		}
		if out == nil {
			out = append([]core.Message(nil), msgs...)
		}
		out[i].Parts = parts
	}
	if out == nil {
		return msgs
	}
	return out
}

// hasResultMedia and hasAttachmentMedia pick the parts whose media
// resolveParts resolves.
func hasResultMedia(p core.Part) bool {
	return p.Kind == core.PartToolResult && p.Result != nil && !p.Result.IsError && len(p.Result.Media) > 0
}

func hasAttachmentMedia(p core.Part) bool {
	return p.Kind == core.PartAttachment && p.Attachment != nil && p.Attachment.Media != nil
}

// countMedia counts the media resolveParts would resolve across msgs.
func countMedia(msgs []core.Message) int {
	n := 0
	for _, msg := range msgs {
		for _, p := range msg.Parts {
			switch {
			case hasResultMedia(p):
				n += len(p.Result.Media)
			case hasAttachmentMedia(p):
				n++
			}
		}
	}
	return n
}

// dropNext reports whether the next medium (in request order) is one of
// the oldest that exceed maxRequestImages, consuming it if so.
func (r *resolver) dropNext() bool {
	if r.skip == 0 {
		return false
	}
	r.skip--
	return true
}

// resolveParts returns a copy of parts with media resolved, and whether
// any part held media; parts itself is returned unchanged when none did.
func (r *resolver) resolveParts(parts []core.Part) ([]core.Part, bool) {
	var out []core.Part
	for i, p := range parts {
		switch {
		case hasResultMedia(p):
			res := r.resolveResult(*p.Result)
			p.Result = &res
		case hasAttachmentMedia(p):
			a := r.resolveAttachment(*p.Attachment)
			p.Attachment = &a
		default:
			continue
		}
		if out == nil {
			out = append([]core.Part(nil), parts...)
		}
		out[i] = p
	}
	if out == nil {
		return parts, false
	}
	return out, true
}

// resolveResult loads res's media (a failed load drops that medium and
// notes why in Output; an old medium past the cap is dropped with
// droppedNote), or replaces them with the omitted placeholder. res is a
// copy; its Media slice is rebuilt, never written in place.
func (r *resolver) resolveResult(res core.ToolResult) core.ToolResult {
	if !r.m.images {
		res.Media = nil
		res.Output += "\n" + r.m.omitted()
		return res
	}
	loaded := make([]core.Media, 0, len(res.Media))
	for _, med := range res.Media {
		if r.dropNext() {
			res.Output += "\n" + droppedNote
			continue
		}
		data, err := r.m.load(med)
		if err != nil {
			res.Output += "\n" + unavailable(err)
			continue
		}
		med.Data = data
		loaded = append(loaded, med)
	}
	res.Media = loaded
	return res
}

// resolveAttachment loads a's image, or replaces its Content with the
// omitted, dropped, or unavailable placeholder and drops the Media. a is
// a copy.
func (r *resolver) resolveAttachment(a core.Attachment) core.Attachment {
	if !r.m.images {
		a.Media, a.Content = nil, r.m.omitted()
		return a
	}
	if r.dropNext() {
		a.Media, a.Content = nil, droppedNote
		return a
	}
	data, err := r.m.load(*a.Media)
	if err != nil {
		a.Media, a.Content = nil, unavailable(err)
		return a
	}
	med := *a.Media
	med.Data = data
	a.Media = &med
	return a
}

func (m *mediaLLM) load(med core.Media) ([]byte, error) {
	if med.Data != nil {
		return med.Data, nil
	}
	if m.blobs == nil {
		return nil, errors.New("no blob store")
	}
	return m.blobs.Open(med.Ref)
}

func (m *mediaLLM) omitted() string {
	return fmt.Sprintf("[image omitted: %s does not accept images]", m.model)
}

func unavailable(err error) string {
	return "[image unavailable: " + strings.TrimSpace(err.Error()) + "]"
}
