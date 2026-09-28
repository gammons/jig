package chat

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/service/tools"
)

var _ Imager = (*fakeImager)(nil)

// fakeImager is a scripted Imager for attachment tests.
type fakeImager struct {
	calls [][]byte
	media core.Media
	info  core.ImageInfo
	err   error
}

func (f *fakeImager) Process(data []byte) (core.Media, core.ImageInfo, error) {
	f.calls = append(f.calls, data)
	if f.err != nil {
		return core.Media{}, core.ImageInfo{}, f.err
	}
	return f.media, f.info, nil
}

// withAttachments equips f's Deps with a real *tools.Tracker (as
// ReadMarker), tools.OSFS() (as FileReader), and img (as Imager), then
// rebuilds f.svc over the result.
func (f *fixture) withAttachments(img Imager) *tools.Tracker {
	tr := tools.NewTracker()
	f.deps.Files = tools.OSFS()
	f.deps.Reads = tr
	f.deps.Images = img
	f.svc = New(f.deps)
	return tr
}

func writeAttachment(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSend_TextAttachmentStoredAndMarkedRead(t *testing.T) {
	f := newFixture(t, llmtest.New(llmtest.Text("ok")), llmtest.New())
	tr := f.withAttachments(nil)
	path := writeAttachment(t, f.workDir, "notes.txt", []byte("hello notes"))

	res, err := f.svc.Send(context.Background(), core.SendRequest{Text: "look", Attachments: []string{"notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}

	msgs, err := f.sessions.Messages(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	user := msgs[0]
	if len(user.Parts) != 2 || user.Parts[0].Kind != core.PartText || user.Parts[1].Kind != core.PartAttachment {
		t.Fatalf("user parts = %+v, want [text, attachment]", user.Parts)
	}
	att := user.Parts[1].Attachment
	if att == nil || att.Path != path || att.Content != "hello notes" || att.Media != nil {
		t.Errorf("attachment = %+v", att)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckWritable(res.SessionID, path, info); err != nil {
		t.Errorf("CheckWritable after Send: %v, want nil (attachment marks it read)", err)
	}
}

func TestSend_AttachmentErrorsAreConfigErrors(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string) string // returns the attachment path, relative to dir
		want  string
	}{
		{"missing", func(t *testing.T, dir string) string {
			return "missing.txt"
		}, "no such file"},
		{"directory", func(t *testing.T, dir string) string {
			if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			return "sub"
		}, "is a directory"},
		{"too large", func(t *testing.T, dir string) string {
			writeAttachment(t, dir, "big.txt", bytes.Repeat([]byte("a"), maxAttachmentBytes+1))
			return "big.txt"
		}, "is larger than 50 KB"},
		{"binary", func(t *testing.T, dir string) string {
			writeAttachment(t, dir, "bin.dat", append([]byte("start"), 0))
			return "bin.dat"
		}, "is binary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, llmtest.New(), llmtest.New())
			f.withAttachments(nil)
			rel := c.setup(t, f.workDir)

			_, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hi", Attachments: []string{rel}})
			ce := wantConfigError(t, err)
			if !strings.Contains(ce.Error(), c.want) {
				t.Errorf("err = %q, want it to contain %q", ce.Error(), c.want)
			}
			if roots := f.roots(); len(roots) != 0 {
				t.Errorf("sessions = %d, want 0", len(roots))
			}
		})
	}
}

func TestSend_ImageAttachmentUsesPipeline(t *testing.T) {
	img := &fakeImager{
		media: core.Media{MIME: "image/png", Ref: "r1"},
		info:  core.ImageInfo{Width: 1, Height: 1, Bytes: 4},
	}
	f := newFixture(t, llmtest.New(llmtest.Text("saw it")), llmtest.New())
	f.withAttachments(img)
	data := []byte{1, 2, 3, 4}
	path := writeAttachment(t, f.workDir, "shot.png", data)

	res, err := f.svc.Send(context.Background(), core.SendRequest{Text: "look", Attachments: []string{"shot.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(img.calls) != 1 || !bytes.Equal(img.calls[0], data) {
		t.Errorf("Images.Process calls = %+v, want one call with the file's bytes", img.calls)
	}

	msgs, err := f.sessions.Messages(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	att := msgs[0].Parts[1].Attachment
	if att == nil || att.Path != path || att.Content != "" || att.Media == nil || att.Media.Ref != "r1" {
		t.Errorf("attachment = %+v", att)
	}
}
