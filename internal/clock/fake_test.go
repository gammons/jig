package clock

import (
	"testing"
	"time"
)

func TestFake_AfterFiresOnAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)

	ch := f.After(5 * time.Second)

	f.Advance(4 * time.Second)
	select {
	case <-ch:
		t.Fatal("channel fired before deadline")
	default:
	}

	f.Advance(time.Second)
	select {
	case got := <-ch:
		want := start.Add(5 * time.Second)
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	default:
		t.Fatal("channel did not fire at deadline")
	}
}

func TestFake_NowAdvances(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)

	f.Advance(3 * time.Second)
	f.Advance(2 * time.Second)

	want := start.Add(5 * time.Second)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFake_BlockUntilWaiters(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)

	done := make(chan time.Time, 1)
	go func() {
		done <- <-f.After(time.Minute)
	}()

	f.BlockUntilWaiters(1)
	f.Advance(time.Minute)

	select {
	case got := <-done:
		want := start.Add(time.Minute)
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for After to fire")
	}
}
