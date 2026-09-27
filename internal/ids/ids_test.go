package ids

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
)

func TestGen_SortableAndPrefixed(t *testing.T) {
	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := New(fake, rand.Reader)

	a := g.Next("ses")
	fake.Advance(time.Millisecond)
	b := g.Next("ses")

	if !strings.HasPrefix(a, "ses_") {
		t.Errorf("a = %q, want prefix %q", a, "ses_")
	}
	if !strings.HasPrefix(b, "ses_") {
		t.Errorf("b = %q, want prefix %q", b, "ses_")
	}
	if a >= b {
		t.Errorf("want a < b, got a=%q b=%q", a, b)
	}
}
