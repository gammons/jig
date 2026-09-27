package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCapture_TailWrapsAcrossMultipleWrites exercises appendTail's shift
// branch: a sequence of small writes that first fills the tail buffer
// exactly, then overflows it, forcing the front to be dropped.
func TestCapture_TailWrapsAcrossMultipleWrites(t *testing.T) {
	c := newCapture(10, "")
	writes := []string{"abc", "def", "ghij", "klmno"} // 3+3+4+5 = 15 bytes total
	for _, w := range writes {
		if _, err := c.Write([]byte(w)); err != nil {
			t.Fatalf("Write(%q): %v", w, err)
		}
	}

	if got, want := string(c.tail()), "fghijklmno"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if c.total != 15 {
		t.Errorf("total = %d, want 15", c.total)
	}
	if cap(c.buf) > 10 {
		t.Errorf("cap(buf) = %d, want <= 10", cap(c.buf))
	}
}

// TestCapture_WriteLargerThanCapacityAfterPartialFill exercises the
// len(p) >= tailBytes branch when it arrives after some data is already
// buffered: the whole existing buffer is discarded, not merged.
func TestCapture_WriteLargerThanCapacityAfterPartialFill(t *testing.T) {
	c := newCapture(5, "")
	if _, err := c.Write([]byte("ab")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := c.Write([]byte("1234567890")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, want := string(c.tail()), "67890"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if c.total != 12 {
		t.Errorf("total = %d, want 12", c.total)
	}
	if cap(c.buf) > 5 {
		t.Errorf("cap(buf) = %d, want <= 5", cap(c.buf))
	}
}

// TestCapture_ExactCapacitySingleWrite exercises the len(p) >= tailBytes
// branch when p's length exactly equals tailBytes.
func TestCapture_ExactCapacitySingleWrite(t *testing.T) {
	c := newCapture(6, "")
	if _, err := c.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, want := string(c.tail()), "abcdef"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if c.total != 6 {
		t.Errorf("total = %d, want 6", c.total)
	}
	if cap(c.buf) > 6 {
		t.Errorf("cap(buf) = %d, want <= 6", cap(c.buf))
	}
}

// TestCapture_ExactCapacityAcrossWrites exercises the len(buf)+len(p) ==
// tailBytes boundary of the no-overflow branch.
func TestCapture_ExactCapacityAcrossWrites(t *testing.T) {
	c := newCapture(10, "")
	if _, err := c.Write([]byte("abcdef")); err != nil { // 6 bytes
		t.Fatalf("Write: %v", err)
	}
	if _, err := c.Write([]byte("ghij")); err != nil { // +4 = 10, exact fit
		t.Fatalf("Write: %v", err)
	}

	if got, want := string(c.tail()), "abcdefghij"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if c.total != 10 {
		t.Errorf("total = %d, want 10", c.total)
	}
}

// TestCapture_NoTailBytesKeepsEverything verifies tailBytes == 0 disables
// truncation: capture just accumulates every write.
func TestCapture_NoTailBytesKeepsEverything(t *testing.T) {
	c := newCapture(0, "")
	for i := 0; i < 5; i++ {
		if _, err := c.Write([]byte("hello ")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	want := "hello hello hello hello hello "
	if got := string(c.tail()); got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if c.total != int64(len(want)) {
		t.Errorf("total = %d, want %d", c.total, len(want))
	}
}

// TestCapture_SpillMirrorsEveryWrite verifies a capture with a spill path
// writes every Write's bytes to disk, independent of tailBytes truncation.
func TestCapture_SpillMirrorsEveryWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	c := newCapture(4, path)
	for _, w := range []string{"ab", "cdef", "gh"} {
		if _, err := c.Write([]byte(w)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if c.SpillErr() != nil {
		t.Fatalf("SpillErr = %v, want nil", c.SpillErr())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(data), "abcdefgh"; got != want {
		t.Errorf("spill file = %q, want %q", got, want)
	}
	if got, want := string(c.tail()), "efgh"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
}

// TestCapture_OpenFailureSetsSpillErrButKeepsCapturing verifies that when
// the spill file cannot be opened, capture still buffers normally and
// records the failure in SpillErr instead of failing outright.
func TestCapture_OpenFailureSetsSpillErrButKeepsCapturing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "out.log")
	c := newCapture(10, path)
	if c.SpillErr() == nil {
		t.Fatal("SpillErr = nil, want non-nil for an unopenable spill path")
	}

	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := string(c.tail()), "hello"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if c.total != 5 {
		t.Errorf("total = %d, want 5", c.total)
	}
}

// TestCapture_CloseErrorRecordedAsSpillErr verifies a Close() failure is
// captured too, when no earlier write already recorded one.
func TestCapture_CloseErrorRecordedAsSpillErr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.log")
	c := newCapture(0, path)
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Close the underlying file out from under capture so capture's own
	// Close() call fails.
	closeErr := c.spill.Close()
	if closeErr != nil {
		t.Fatalf("pre-closing spill file: %v", closeErr)
	}

	if err := c.Close(); err == nil {
		t.Fatal("Close() = nil, want an error for a double-close")
	}
	if c.SpillErr() == nil {
		t.Fatal("SpillErr = nil, want non-nil after a Close() failure")
	}
	if !errors.Is(c.SpillErr(), os.ErrClosed) {
		t.Errorf("SpillErr = %v, want it to wrap os.ErrClosed", c.SpillErr())
	}
}
