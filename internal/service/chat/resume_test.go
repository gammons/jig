package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestSend_ModelFlagAcceptsAlias(t *testing.T) {
	main := llmtest.New()
	f := newFixture(t, main, llmtest.New(llmtest.Text("T")))
	f.llms.clients[core.ModelRef{Provider: "other", Model: "m"}] = llmtest.New(llmtest.Text("from other"))

	res, err := f.svc.Send(context.Background(), core.SendRequest{Model: "alt", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if textOf(res.Message) != "from other" || f.get(res.SessionID).Model != "other/m" {
		t.Errorf("message = %+v, session model = %q; want other/m", res.Message, f.get(res.SessionID).Model)
	}

	_, err = f.svc.Send(context.Background(), core.SendRequest{Model: "nosuch", Text: "hi"})
	ce := wantConfigError(t, err)
	if !strings.Contains(ce.Error(), `model alias "nosuch"`) {
		t.Errorf("err = %q, want it to name the alias", ce.Error())
	}
}

func TestSend_ResumeInOtherCwdIsConfigError(t *testing.T) {
	f := newFixture(t, llmtest.New(llmtest.Text("first")), llmtest.New(llmtest.Text("T")))
	first, err := f.svc.Send(context.Background(), core.SendRequest{Text: "one"})
	if err != nil {
		t.Fatal(err)
	}

	d := f.deps
	d.WorkDir = t.TempDir()
	other := New(d)
	t.Cleanup(func() { _ = other.Close(context.Background()) })

	_, err = other.Send(context.Background(), core.SendRequest{SessionID: first.SessionID, Text: "two"})
	ce := wantConfigError(t, err)
	want := "session " + string(first.SessionID) + " belongs to " + f.workDir + "; re-run with --cwd " + f.workDir
	if ce.Error() != want {
		t.Errorf("err = %q, want %q", ce.Error(), want)
	}
}

func TestSend_ResumeThroughSymlinkedCwd(t *testing.T) {
	f := newFixture(t, llmtest.New(llmtest.Text("first"), llmtest.Text("second")), llmtest.New(llmtest.Text("T")))
	first, err := f.svc.Send(context.Background(), core.SendRequest{Text: "one"})
	if err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(f.workDir, link); err != nil {
		t.Fatal(err)
	}
	d := f.deps
	d.WorkDir = link
	other := New(d)
	t.Cleanup(func() { _ = other.Close(context.Background()) })

	if _, err := other.Send(context.Background(), core.SendRequest{SessionID: first.SessionID, Text: "two"}); err != nil {
		t.Errorf("resume through symlinked cwd: err = %v, want nil", err)
	}
}

// getFailing is a Sessions whose Get always fails with err.
type getFailing struct {
	Sessions
	err error
}

func (g getFailing) Get(context.Context, core.SessionID) (core.Session, error) {
	return core.Session{}, g.err
}

func TestSend_ResumeGetErrorKinds(t *testing.T) {
	f := newFixture(t, llmtest.New(), llmtest.New())

	t.Run("not found is a ConfigError", func(t *testing.T) {
		_, err := f.svc.Send(context.Background(), core.SendRequest{SessionID: "ses_missing", Text: "x"})
		wantConfigError(t, err)
	})

	t.Run("transient error is a run error", func(t *testing.T) {
		d := f.deps
		transient := errors.New("database is locked")
		d.Sessions = getFailing{Sessions: d.Sessions, err: transient}
		svc := New(d)
		t.Cleanup(func() { _ = svc.Close(context.Background()) })

		_, err := svc.Send(context.Background(), core.SendRequest{SessionID: "ses_x", Text: "x"})
		var ce *ConfigError
		if err == nil || errors.As(err, &ce) {
			t.Fatalf("err = %v (%T), want a non-ConfigError", err, err)
		}
		if !errors.Is(err, transient) {
			t.Errorf("err = %v, want it to wrap %v", err, transient)
		}
	})
}
