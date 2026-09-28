package llm

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

// fakeBlobs is a BlobReader over an in-memory map; a missing ref errors.
type fakeBlobs map[string][]byte

func (b fakeBlobs) Open(ref string) ([]byte, error) {
	data, ok := b[ref]
	if !ok {
		return nil, errors.New("blob " + ref + " not found")
	}
	return data, nil
}

// recordingLLM records the request it is streamed and yields nothing.
type recordingLLM struct{ got core.LLMRequest }

func (r *recordingLLM) Stream(_ context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	r.got = req
	return func(func(core.StreamEvent, error) bool) {}
}

func drain(l core.LLM, req core.LLMRequest) {
	for range l.Stream(context.Background(), req) {
	}
}

// imageRequest is one assistant message whose read result carries one
// image medium with the given ref, plus a user image attachment.
func imageRequest(ref string) core.LLMRequest {
	return core.LLMRequest{Messages: []core.Message{
		{Role: core.RoleUser, Parts: []core.Part{
			{Kind: core.PartText, Text: "look"},
			{Kind: core.PartAttachment, Attachment: &core.Attachment{Path: "/w/a.png", Media: &core.Media{MIME: "image/png", Ref: ref}}},
		}},
		{Role: core.RoleAssistant, Parts: []core.Part{
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "read"}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{
				CallID: "c1", Name: "read", Output: "image 20x10 (1 KB)",
				Media: []core.Media{{MIME: "image/png", Ref: ref}},
			}},
		}},
	}}
}

func resultOf(t *testing.T, req core.LLMRequest) *core.ToolResult {
	t.Helper()
	r := req.Messages[1].Parts[1].Result
	if r == nil {
		t.Fatal("no tool result in request")
	}
	return r
}

func attachmentOf(t *testing.T, req core.LLMRequest) *core.Attachment {
	t.Helper()
	a := req.Messages[0].Parts[1].Attachment
	if a == nil {
		t.Fatal("no attachment in request")
	}
	return a
}

func TestMediaLLM_LoadsDataWhenSupported(t *testing.T) {
	blob := []byte("png-bytes")
	inner := &recordingLLM{}
	l := withMedia(inner, fakeBlobs{"r1": blob}, true, "p/m")
	req := imageRequest("r1")

	drain(l, req)

	got := resultOf(t, inner.got)
	if len(got.Media) != 1 || !bytes.Equal(got.Media[0].Data, blob) {
		t.Fatalf("sent Media = %+v, want one medium with the blob bytes", got.Media)
	}
	if got.Output != "image 20x10 (1 KB)" {
		t.Errorf("sent Output = %q, want it unchanged", got.Output)
	}
	if a := attachmentOf(t, inner.got); a.Media == nil || !bytes.Equal(a.Media.Data, blob) {
		t.Errorf("sent attachment = %+v, want its Media loaded", a)
	}
	if orig := resultOf(t, req); orig.Media[0].Data != nil {
		t.Errorf("caller's result Media.Data = %q, want nil (no aliasing)", orig.Media[0].Data)
	}
	if orig := attachmentOf(t, req); orig.Media.Data != nil {
		t.Errorf("caller's attachment Media.Data = %q, want nil (no aliasing)", orig.Media.Data)
	}
}

func TestMediaLLM_PlaceholderWhenUnsupported(t *testing.T) {
	inner := &recordingLLM{}
	l := withMedia(inner, fakeBlobs{"r1": []byte("x")}, false, "p/m")
	req := imageRequest("r1")

	drain(l, req)

	const note = "[image omitted: p/m does not accept images]"
	got := resultOf(t, inner.got)
	if !strings.HasSuffix(got.Output, "\n"+note) {
		t.Errorf("sent Output = %q, want it to end with %q", got.Output, "\n"+note)
	}
	if len(got.Media) != 0 {
		t.Errorf("sent Media = %+v, want none", got.Media)
	}
	if a := attachmentOf(t, inner.got); a.Media != nil || a.Content != note {
		t.Errorf("sent attachment = %+v, want no Media and Content %q", a, note)
	}
	orig := resultOf(t, req)
	if orig.Output != "image 20x10 (1 KB)" || len(orig.Media) != 1 {
		t.Errorf("caller's result = %+v, want it unchanged", orig)
	}
	if attachmentOf(t, req).Media == nil {
		t.Error("caller's attachment lost its Media, want it unchanged")
	}
}

func TestMediaLLM_BlobErrorPlaceholder(t *testing.T) {
	inner := &recordingLLM{}
	l := withMedia(inner, fakeBlobs{}, true, "p/m")

	drain(l, imageRequest("gone"))

	const note = "[image unavailable: blob gone not found]"
	got := resultOf(t, inner.got)
	if !strings.HasSuffix(got.Output, "\n"+note) {
		t.Errorf("sent Output = %q, want it to end with %q", got.Output, "\n"+note)
	}
	if len(got.Media) != 0 {
		t.Errorf("sent Media = %+v, want the failed medium dropped", got.Media)
	}
	if a := attachmentOf(t, inner.got); a.Media != nil || a.Content != note {
		t.Errorf("sent attachment = %+v, want no Media and Content %q", a, note)
	}
}

func TestMediaLLM_LeavesPlainMessagesAlone(t *testing.T) {
	inner := &recordingLLM{}
	l := withMedia(inner, fakeBlobs{}, false, "p/m")
	req := core.LLMRequest{Messages: []core.Message{
		{Role: core.RoleAssistant, Parts: []core.Part{
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c1", Output: "ok"}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c2", Output: "bad", IsError: true, Media: []core.Media{{Ref: "x"}}}},
		}},
	}}

	drain(l, req)

	parts := inner.got.Messages[0].Parts
	if parts[0].Result.Output != "ok" {
		t.Errorf("plain result Output = %q, want %q", parts[0].Result.Output, "ok")
	}
	if parts[1].Result.Output != "bad" {
		t.Errorf("error result Output = %q, want %q (errors ignore media)", parts[1].Result.Output, "bad")
	}
}

// llmFactory is an ext.ProviderFactory whose New returns l.
type llmFactory struct{ l core.LLM }

func (llmFactory) Type() string { return "fake" }

func (f llmFactory) New(core.ProviderInfo, core.ProviderConfig, string) (core.LLM, error) {
	return f.l, nil
}

func TestSource_ForResolvesMediaByModel(t *testing.T) {
	see := core.ModelRef{Provider: "p", Model: "see"}
	blind := core.ModelRef{Provider: "p", Model: "blind"}
	cat := fakeCatalog{
		providers: map[string]core.ProviderInfo{"p": {ID: "p", Type: "fake"}},
		models: map[core.ModelRef]core.ModelInfo{
			see:   {Ref: see, SupportsImages: true},
			blind: {Ref: blind},
		},
	}
	inner := &recordingLLM{}
	s := NewSource(cat, viewWith(llmFactory{inner}), nil, noEnv, fakeBlobs{"r1": []byte("img")})

	l, _, err := s.For(see)
	if err != nil {
		t.Fatal(err)
	}
	drain(l, imageRequest("r1"))
	if got := resultOf(t, inner.got); len(got.Media) != 1 || string(got.Media[0].Data) != "img" {
		t.Errorf("p/see: sent Media = %+v, want the blob loaded", got.Media)
	}

	l, _, err = s.For(blind)
	if err != nil {
		t.Fatal(err)
	}
	drain(l, imageRequest("r1"))
	if got := resultOf(t, inner.got); !strings.HasSuffix(got.Output, "[image omitted: p/blind does not accept images]") {
		t.Errorf("p/blind: sent Output = %q, want the omitted placeholder", got.Output)
	}
}
