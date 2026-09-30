package logtest_test

import (
	"testing"

	"github.com/gammons/jig/internal/core/logtest"
)

func TestNew_DropsTimeAndFindsByMsg(t *testing.T) {
	log, buf := logtest.New()
	log.Debug("run start", "cat", "run", "steps", 2)
	lines := buf.Find("run start")
	if len(lines) != 1 || lines[0] != `level=DEBUG msg="run start" cat=run steps=2` {
		t.Errorf("lines = %q", lines)
	}
}

func TestFind_ExactWordMatch(t *testing.T) {
	log, buf := logtest.New()
	log.Debug("x")
	log.Debug("xy")
	log.Debug("x", "k", "v")
	if got := buf.Find("x"); len(got) != 2 {
		t.Errorf("Find(x) = %q, want 2 lines", got)
	}
	if got := buf.Find("xy"); len(got) != 1 {
		t.Errorf("Find(xy) = %q, want 1 line", got)
	}
}

func TestLines_ExcludesPartialLine(t *testing.T) {
	_, buf := logtest.New()
	if _, err := buf.Write([]byte("a\nb\npartial")); err != nil {
		t.Fatal(err)
	}
	got := buf.Lines()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Lines = %q", got)
	}
}
