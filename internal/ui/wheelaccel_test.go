package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
)

// notches feeds w one notch per gap (the first at t0) in dir over p and
// returns the lines each scrolled.
func notches(w *wheelAccel, t0 time.Time, dir int, p mouseRegion, gaps ...time.Duration) []int {
	now := t0
	out := []int{w.lines(now, dir, p)}
	for _, g := range gaps {
		now = now.Add(g)
		out = append(out, w.lines(now, dir, p))
	}
	return out
}

func TestWheelAccel_SlowNotchesScrollWheelLines(t *testing.T) {
	t.Parallel()
	var w wheelAccel
	got := notches(&w, testStart(), 1, regionTranscript, 200*time.Millisecond, 400*time.Millisecond, time.Second)
	for i, n := range got {
		if n != wheelLines {
			t.Errorf("notch %d scrolled %d lines, want %d (a slow notch never accelerates)", i, n, wheelLines)
		}
	}
}

func TestWheelAccel_FastStreakAcceleratesToTheCap(t *testing.T) {
	t.Parallel()
	var w wheelAccel
	gaps := make([]time.Duration, 12)
	for i := range gaps {
		gaps[i] = 10 * time.Millisecond
	}
	got := notches(&w, testStart(), 1, regionTranscript, gaps...)
	if got[0] != wheelLines {
		t.Errorf("first notch scrolled %d, want %d", got[0], wheelLines)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("notch %d scrolled %d, less than the notch before (%d): a steady streak never slows", i, got[i], got[i-1])
		}
	}
	if last, want := got[len(got)-1], int(wheelLines*accelMax); last != want {
		t.Errorf("a sustained 10 ms streak reached %d lines/notch, want the cap %d", last, want)
	}
}

// TestWheelAccel_Curve pins opencode's curve: for an average gap avg (ms)
// between notches, the step is 1 + 0.8·(e^(100/avg/3) − 1) times
// wheelLines, capped at 6×.
func TestWheelAccel_Curve(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		avg  time.Duration
		want float64
	}{
		{150 * time.Millisecond, 1.1991},
		{100 * time.Millisecond, 1.3165},
		{50 * time.Millisecond, 1.7582},
		{25 * time.Millisecond, 3.2349},
		{10 * time.Millisecond, accelMax}, // 22.6 uncapped
	} {
		if got := accelFactor(tt.avg); got < tt.want-0.001 || got > tt.want+0.001 {
			t.Errorf("accelFactor(%v) = %.4f, want %.4f", tt.avg, got, tt.want)
		}
	}
}

func TestWheelAccel_FractionsCarry(t *testing.T) {
	t.Parallel()
	var w wheelAccel
	// 50 ms gaps: 1.76× of 3 = 5.27 lines/notch; 10 notches after the
	// history fills must total ≈ 52.7, not 10×5 = 50.
	gaps := make([]time.Duration, 13)
	for i := range gaps {
		gaps[i] = 50 * time.Millisecond
	}
	got := notches(&w, testStart(), 1, regionTranscript, gaps...)
	sum := 0
	for _, n := range got[4:] {
		sum += n
	}
	if sum < 52 || sum > 53 {
		t.Errorf("10 notches at 1.76× scrolled %d lines in all, want 52–53 (fractions carried)", sum)
	}
}

func TestWheelAccel_StreakResets(t *testing.T) {
	t.Parallel()
	fast := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
	warm := func() (*wheelAccel, time.Time) {
		var w wheelAccel
		t0 := testStart()
		notches(&w, t0, 1, regionTranscript, fast...)
		return &w, t0.Add(30 * time.Millisecond)
	}
	for _, tt := range []struct {
		name string
		next func(w *wheelAccel, now time.Time) int
	}{
		{"a pause over 150 ms", func(w *wheelAccel, now time.Time) int {
			return w.lines(now.Add(151*time.Millisecond), 1, regionTranscript)
		}},
		{"a reversed direction", func(w *wheelAccel, now time.Time) int {
			return -w.lines(now.Add(10*time.Millisecond), -1, regionTranscript)
		}},
		{"another pane", func(w *wheelAccel, now time.Time) int {
			return w.lines(now.Add(10*time.Millisecond), 1, regionDetails)
		}},
	} {
		w, now := warm()
		if n := tt.next(w, now); n != wheelLines {
			t.Errorf("after %s, the next notch scrolled %d lines, want %d (a new streak)", tt.name, n, wheelLines)
		}
	}
}

// TestWheelAccel_BurstNotchesDontAccelerate: notches under 6 ms after the
// last counted one (a terminal delivering one gesture as a burst) scroll
// wheelLines each and don't count toward the streak's speed.
func TestWheelAccel_BurstNotchesDontAccelerate(t *testing.T) {
	t.Parallel()
	var w wheelAccel
	got := notches(&w, testStart(), 1, regionTranscript, 0, time.Millisecond, 2*time.Millisecond)
	for i, n := range got {
		if n != wheelLines {
			t.Errorf("burst notch %d scrolled %d, want %d", i, n, wheelLines)
		}
	}
}

// TestMouse_FastWheelAcceleratesTranscript: through the App, a fast
// streak of wheel notches (App clock) scrolls the transcript by the
// accelerated amounts.
func TestMouse_FastWheelAcceleratesTranscript(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(120, 30), withResume(core.Session{ID: "ses_1", Agent: "build"}, benchHistory(200), nil))
	want := ta.app.w.list
	var ref wheelAccel
	now := ta.clk.Now()
	total := 0
	for i := range 8 {
		if i > 0 {
			ta.clk.Advance(10 * time.Millisecond)
			now = now.Add(10 * time.Millisecond)
		}
		n := ref.lines(now, -1, regionTranscript)
		total += n
		want.ScrollBy(n)
		ta.mouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	}
	if -total <= 8*wheelLines {
		t.Fatalf("test setup: the streak scrolled %d lines, no more than unaccelerated", -total)
	}
	if got, w := ta.app.w.list.View(), want.View(); got != w {
		t.Errorf("transcript after a fast streak differs from ScrollBy of the accelerated total (%d lines)", -total)
	}
}
