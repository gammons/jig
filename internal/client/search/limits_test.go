package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const longLineMarker = " [... omitted end of long line]"

func TestGrep_LongLinesTruncated(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWrite(t, filepath.Join(dir, "f.txt"), "TODO "+strings.Repeat("x", 2000)+"\nshort TODO\n")

	for name, s := range searcherVariants(t) {
		t.Run(name, func(t *testing.T) {
			got, err := s.Grep(context.Background(), dir, "TODO", "", 0)
			if err != nil {
				t.Fatalf("Grep: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("Grep = %+v, want 2 matches", got)
			}
			want := "TODO " + strings.Repeat("x", 495) + longLineMarker
			if got[0].Text != want {
				t.Errorf("long line len %d, ends %q; want 500 chars + marker", len(got[0].Text), got[0].Text[max(0, len(got[0].Text)-40):])
			}
			if got[1].Text != "short TODO" {
				t.Errorf("short line = %q", got[1].Text)
			}
		})
	}
}

// TestRgGrep_StopsAtLimit runs a fake rg that prints two matches and then
// hangs: Grep must return after limit matches, killing it.
func TestRgGrep_StopsAtLimit(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "rg")
	script := "#!/bin/sh\nprintf 'b.go:1:one\\na.go:2:two\\n'\nexec sleep 30\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &rgSearcher{rgPath: fake}

	type result struct {
		ms  []Match
		err error
	}
	done := make(chan result, 1)
	go func() {
		ms, err := s.Grep(context.Background(), dir, "x", "", 2)
		done <- result{ms, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Grep: %v", r.err)
		}
		if len(r.ms) != 2 || r.ms[0].Path != "a.go" || r.ms[1].Path != "b.go" {
			t.Errorf("Grep = %+v, want a.go:2 and b.go:1", r.ms)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Grep did not stop reading rg after limit matches")
	}
}
