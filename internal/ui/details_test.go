package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// fakeProject implements core.ProjectService over an in-memory file map.
type fakeProject struct {
	files map[string][]byte
	err   error
}

func (f fakeProject) Files(context.Context) ([]core.ProjectFile, error) { return nil, nil }

func (f fakeProject) ReadFile(_ context.Context, path string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found: " + path)
	}
	return data, nil
}

// fakeBlobs implements core.BlobService over an in-memory blob map.
type fakeBlobs struct {
	data map[string][]byte
	err  error
}

func (f fakeBlobs) Open(ref string) ([]byte, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	data, ok := f.data[ref]
	if !ok {
		return nil, "", errors.New("blob not found: " + ref)
	}
	return data, "image/png", nil
}

// fakeSessions implements core.SessionService; only Messages is exercised
// by details.go, so the rest return zero values.
type fakeSessions struct {
	msgs map[core.SessionID][]core.Message
	err  error
}

func (f fakeSessions) List(context.Context, int) ([]core.Session, error) { return nil, nil }
func (f fakeSessions) ListForCwd(context.Context, string, int) ([]core.Session, error) {
	return nil, nil
}
func (f fakeSessions) Get(context.Context, core.SessionID) (core.Session, error) {
	return core.Session{}, nil
}
func (f fakeSessions) Messages(_ context.Context, id core.SessionID) ([]core.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.msgs[id], nil
}
func (f fakeSessions) Todos(context.Context, core.SessionID) ([]core.Todo, error) { return nil, nil }
func (f fakeSessions) Rename(context.Context, core.SessionID, string) error       { return nil }
func (f fakeSessions) Configure(context.Context, core.SessionID, string, string) error {
	return nil
}

// testRenderer returns a renderer styled from the default theme, for
// details tests that need coderender/mdrender styling.
func testRenderer() *renderer {
	set := theme.Build(theme.Default(), 1)
	return newRenderer(&set)
}

func editBlock(path, oldString, newString string) transcript.Block {
	in, _ := json.Marshal(map[string]string{"path": path, "old_string": oldString, "new_string": newString})
	return transcript.Block{
		ID: "t/c1", Kind: transcript.KindTool,
		Call: &core.ToolCall{ID: "c1", Name: "edit", Input: in},
	}
}

func TestDetails_EditWithContext(t *testing.T) {
	t.Parallel()
	r := testRenderer()
	b := editBlock("a.go", "foo", "bar")
	p := Ports{Project: fakeProject{files: map[string][]byte{
		"a.go": []byte("line one\nbar\nline three\n"),
	}}}

	_, cmd := buildDetails(b, 80, 24, r, p, nil)
	if cmd == nil {
		t.Fatal("buildDetails: cmd = nil, want a Cmd to read the file for context")
	}
	msg, ok := cmd().(detailsMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want detailsMsg", cmd())
	}
	if msg.Block != b.ID {
		t.Errorf("Block = %q, want %q", msg.Block, b.ID)
	}
	if !strings.Contains(msg.Content.Header, "1 hunks") {
		t.Errorf("Header = %q, want it to contain %q", msg.Content.Header, "1 hunks")
	}
}

func TestDetails_EditFileUnreadableFallsBack(t *testing.T) {
	t.Parallel()
	r := testRenderer()
	b := editBlock("a.go", "foo", "bar")
	p := Ports{Project: fakeProject{err: errors.New("permission denied")}}

	content, cmd := buildDetails(b, 80, 24, r, p, nil)
	if cmd == nil {
		t.Fatal("buildDetails: cmd = nil, want a Cmd")
	}
	msg, ok := cmd().(detailsMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want detailsMsg", cmd())
	}
	if msg.Content.Header != content.Header {
		t.Errorf("Header after an unreadable file = %q, want the bare diff's header %q", msg.Content.Header, content.Header)
	}
	if strings.Join(msg.Content.Lines, "\n") != strings.Join(content.Lines, "\n") {
		t.Errorf("Lines after an unreadable file changed from the bare diff")
	}
}

func TestDetails_ReadImageCmd(t *testing.T) {
	t.Parallel()
	r := testRenderer()

	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	in, _ := json.Marshal(map[string]string{"path": "pic.png"})
	b := transcript.Block{
		ID: "t/c1", Kind: transcript.KindTool,
		Call: &core.ToolCall{ID: "c1", Name: "read", Input: in},
		Result: &core.ToolResult{
			CallID: "c1", Name: "read", Output: "image 4x4 (100 B)",
			Media: []core.Media{{Ref: "sha-1", MIME: "image/png"}},
		},
	}
	p := Ports{Blobs: fakeBlobs{data: map[string][]byte{"sha-1": buf.Bytes()}}}
	img := imgrender.New(imgrender.Blocks)

	_, cmd := buildDetails(b, 80, 24, r, p, img)
	if cmd == nil {
		t.Fatal("buildDetails: cmd = nil, want a Cmd to open and render the image")
	}
	msg, ok := cmd().(detailsMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want detailsMsg", cmd())
	}
	if msg.Image == nil {
		t.Fatal("Image = nil, want a rendered Result")
	}
	if len(msg.Image.Lines) == 0 {
		t.Errorf("Image.Lines is empty, want > 0 rows (Blocks protocol)")
	}
}

func TestDetails_BashSpillPath(t *testing.T) {
	t.Parallel()
	r := testRenderer()

	in, _ := json.Marshal(map[string]string{"command": "echo hi"})
	b := transcript.Block{
		ID: "t/c1", Kind: transcript.KindTool,
		Call: &core.ToolCall{ID: "c1", Name: "bash", Input: in},
		Result: &core.ToolResult{
			CallID: "c1", Name: "bash",
			Output: "[output truncated; full output: /tmp/spill.log]\nhi\n[exit code 0]",
		},
	}
	content, cmd := buildDetails(b, 80, 24, r, Ports{}, nil)
	if cmd != nil {
		t.Fatal("buildDetails: cmd != nil, want a synchronous bash result (no port call)")
	}
	joined := strings.Join(content.Lines, "\n")
	if !strings.Contains(joined, "spill: /tmp/spill.log") {
		t.Errorf("Lines = %q, want a %q line", joined, "spill: /tmp/spill.log")
	}
	if !strings.Contains(joined, "exit 0") {
		t.Errorf("Lines = %q, want an %q line", joined, "exit 0")
	}
	if !strings.Contains(joined, "$ echo hi") {
		t.Errorf("Lines = %q, want a %q line", joined, "$ echo hi")
	}
}

func TestDetails_SubagentSummary(t *testing.T) {
	t.Parallel()

	callInput, _ := json.Marshal(map[string]string{"path": "src/main.go"})
	child := core.SessionID("s2")
	msgs := []core.Message{
		{
			ID: "m1", SessionID: child, Role: core.RoleAssistant, Status: core.StatusComplete,
			Parts: []core.Part{
				{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "rc1", Name: "read", Input: callInput}},
				{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "rc1", Name: "read", Output: "1: package main"}},
				{Kind: core.PartText, Text: "done looking around"},
			},
		},
	}
	b := transcript.Block{
		ID: "t/c1", Kind: transcript.KindSubagent,
		Sub: &transcript.Subagent{Child: child, Agent: "explore", Description: "find the bug"},
	}
	p := Ports{Sessions: fakeSessions{msgs: map[core.SessionID][]core.Message{child: msgs}}}

	_, cmd := buildDetails(b, 80, 24, testRenderer(), p, nil)
	if cmd == nil {
		t.Fatal("buildDetails: cmd = nil, want a Cmd to load the child's messages")
	}
	msg, ok := cmd().(detailsMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want detailsMsg", cmd())
	}
	joined := strings.Join(msg.Content.Lines, "\n")
	if !strings.Contains(joined, "read") || !strings.Contains(joined, "src/main.go") {
		t.Errorf("Lines = %q, want the child's read one-liner", joined)
	}
	if !strings.Contains(joined, "done looking around") {
		t.Errorf("Lines = %q, want the child's final text", joined)
	}
}

func TestDetails_SanitizesFileContent(t *testing.T) {
	t.Parallel()
	r := testRenderer()
	b := editBlock("a.go", "foo", "bar")
	hostile := "line one\n\x1b]52;c;aGk=\x07bar\nline three\n"
	p := Ports{Project: fakeProject{files: map[string][]byte{"a.go": []byte(hostile)}}}

	_, cmd := buildDetails(b, 80, 24, r, p, nil)
	msg, ok := cmd().(detailsMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want detailsMsg", cmd())
	}
	joined := strings.Join(msg.Content.Lines, "\n")
	if strings.Contains(joined, "]52;") {
		t.Errorf("output still contains an OSC 52 payload: %q", joined)
	}
}
