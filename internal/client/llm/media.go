package llm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// BlobReader reads a stored blob by ref. *blobfs.Store satisfies it.
type BlobReader interface {
	Open(ref string) ([]byte, error)
}

// mediaLLM resolves each request's media before handing it to inner: an
// image-capable model gets Media.Data loaded from blobs; any other model
// gets a text placeholder in place of its media. The caller's messages are
// never mutated (they may alias the store's cached values): every message,
// part, result, and attachment it changes is a copy.
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

// resolveMessages returns msgs with every medium resolved, copying only
// the messages that hold media.
func (m *mediaLLM) resolveMessages(msgs []core.Message) []core.Message {
	var out []core.Message
	for i, msg := range msgs {
		parts, changed := m.resolveParts(msg.Parts)
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

// resolveParts returns a copy of parts with media resolved, and whether
// any part held media; parts itself is returned unchanged when none did.
func (m *mediaLLM) resolveParts(parts []core.Part) ([]core.Part, bool) {
	var out []core.Part
	for i, p := range parts {
		switch {
		case p.Kind == core.PartToolResult && p.Result != nil && !p.Result.IsError && len(p.Result.Media) > 0:
			r := m.resolveResult(*p.Result)
			p.Result = &r
		case p.Kind == core.PartAttachment && p.Attachment != nil && p.Attachment.Media != nil:
			a := m.resolveAttachment(*p.Attachment)
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

// resolveResult loads r's media (a failed load drops that medium and
// notes why in Output), or replaces them with the omitted placeholder.
// r is a copy; its Media slice is rebuilt, never written in place.
func (m *mediaLLM) resolveResult(r core.ToolResult) core.ToolResult {
	if !m.images {
		r.Media = nil
		r.Output += "\n" + m.omitted()
		return r
	}
	loaded := make([]core.Media, 0, len(r.Media))
	for _, med := range r.Media {
		data, err := m.load(med)
		if err != nil {
			r.Output += "\n" + unavailable(err)
			continue
		}
		med.Data = data
		loaded = append(loaded, med)
	}
	r.Media = loaded
	return r
}

// resolveAttachment loads a's image, or replaces its Content with the
// omitted or unavailable placeholder and drops the Media. a is a copy.
func (m *mediaLLM) resolveAttachment(a core.Attachment) core.Attachment {
	if !m.images {
		a.Media, a.Content = nil, m.omitted()
		return a
	}
	data, err := m.load(*a.Media)
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
