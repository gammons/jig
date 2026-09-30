package mcp

import (
	"errors"
	"testing"

	"github.com/gammons/jig/internal/core"
)

type fakeImager struct {
	media core.Media
	info  core.ImageInfo
	err   error
}

func (f fakeImager) Process(data []byte) (core.Media, core.ImageInfo, error) {
	return f.media, f.info, f.err
}

func call() core.ToolCall {
	return core.ToolCall{ID: "c1", Name: "mcp__gh__get_issue"}
}

func TestMapResult_Text(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "text", Text: "hello"}, {Kind: "text", Text: "world"}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "hello\nworld" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_Image(t *testing.T) {
	img := fakeImager{media: core.Media{MIME: "image/png", Ref: "abc"}}
	r := RemoteResult{Content: []RemoteContent{{Kind: "image", Data: []byte("bytes")}}}
	got := mapResult(call(), "gh", "get_issue", r, img)
	if len(got.Media) != 1 || got.Media[0].Ref != "abc" {
		t.Fatalf("Media = %+v", got.Media)
	}
	if got.Output != "" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_ImageError(t *testing.T) {
	img := fakeImager{err: errors.New("too big")}
	r := RemoteResult{Content: []RemoteContent{{Kind: "image", Data: []byte("bytes")}}}
	got := mapResult(call(), "gh", "get_issue", r, img)
	if got.Output != "[image omitted: too big]" {
		t.Fatalf("Output = %q", got.Output)
	}
	if len(got.Media) != 0 {
		t.Fatalf("Media = %+v", got.Media)
	}
}

func TestMapResult_ImageNilImager(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "image", Data: []byte("bytes")}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "[image omitted: no image support]" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_Audio(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "audio", MIME: "audio/wav", Data: make([]byte, 42)}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "[audio omitted: audio/wav, 42 bytes]" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_ResourceLink(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "resource_link", URI: "file:///a", Name: "a.txt"}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "[resource: file:///a a.txt]" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_ResourceText(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "resource_text", URI: "file:///a", Text: "content"}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "--- file:///a ---\ncontent" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_ResourceBlob(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "resource_blob", URI: "file:///a", MIME: "application/pdf"}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "[resource omitted: file:///a, application/pdf]" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_MixedOrder(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{
		{Kind: "text", Text: "first"},
		{Kind: "resource_link", URI: "file:///a", Name: "a"},
		{Kind: "text", Text: "second"},
	}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	want := "first\n[resource: file:///a a]\nsecond"
	if got.Output != want {
		t.Fatalf("Output = %q, want %q", got.Output, want)
	}
}

func TestMapResult_StructuredOnlyWhenNoText(t *testing.T) {
	r := RemoteResult{Structured: []byte(`{"a":1}`)}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	want := "{\n  \"a\": 1\n}"
	if got.Output != want {
		t.Fatalf("Output = %q, want %q", got.Output, want)
	}
}

func TestMapResult_StructuredInvalidJSON(t *testing.T) {
	r := RemoteResult{Structured: []byte(`not json`)}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "not json" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_StructuredIgnoredWhenTextPresent(t *testing.T) {
	r := RemoteResult{
		Content:    []RemoteContent{{Kind: "text", Text: "hello"}},
		Structured: []byte(`{"a":1}`),
	}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Output != "hello" {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestMapResult_IsError(t *testing.T) {
	r := RemoteResult{IsError: true, Content: []RemoteContent{{Kind: "text", Text: "boom"}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if !got.IsError {
		t.Fatal("expected IsError")
	}
}

func TestMapResult_Metadata(t *testing.T) {
	r := RemoteResult{Content: []RemoteContent{{Kind: "text", Text: "hi"}}}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.Metadata["mcp.server"] != "gh" || got.Metadata["mcp.tool"] != "get_issue" {
		t.Fatalf("Metadata = %+v", got.Metadata)
	}
}

func TestMapResult_KeepsCallIDAndName(t *testing.T) {
	r := RemoteResult{}
	got := mapResult(call(), "gh", "get_issue", r, nil)
	if got.CallID != "c1" || got.Name != "mcp__gh__get_issue" {
		t.Fatalf("CallID/Name = %q/%q", got.CallID, got.Name)
	}
}
