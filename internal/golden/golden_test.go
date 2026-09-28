package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTB is a minimal testing.TB stand-in that records Errorf/Fatalf calls
// instead of failing the real test, so TestAssert_MismatchWritesActual can
// observe Assert's failure path without actually failing.
type fakeTB struct {
	testing.TB
	errors []string
	fatals []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
}

func TestAssert_UpdateThenMatch(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("update", func(t *testing.T) {
		t.Setenv("JIG_UPDATE_GOLDEN", "1")
		Assert(t, "sample", "hello\nworld\n")
	})

	t.Run("match", func(t *testing.T) {
		Assert(t, "sample", "hello\nworld\n")
	})
}

func TestAssert_MismatchWritesActual(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("seed", func(t *testing.T) {
		t.Setenv("JIG_UPDATE_GOLDEN", "1")
		Assert(t, "sample", "line one\nline two\n")
	})

	fake := &fakeTB{}
	Assert(fake, "sample", "line one\nCHANGED\n")

	if len(fake.errors) == 0 {
		t.Fatalf("Assert did not report an error on mismatch (fatals=%v)", fake.fatals)
	}
	msg := fake.errors[0]
	if !strings.Contains(msg, `"line two"`) {
		t.Errorf("error message %q does not quote the expected differing line", msg)
	}
	if !strings.Contains(msg, `"CHANGED"`) {
		t.Errorf("error message %q does not quote the actual differing line", msg)
	}

	actualPath := filepath.Join("testdata", "golden", "sample.ansi.actual")
	if _, err := os.Stat(actualPath); err != nil {
		t.Errorf("expected %s to exist: %v", actualPath, err)
	}
}
