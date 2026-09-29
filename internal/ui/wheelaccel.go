package ui

import (
	"math"
	"time"
)

// Wheel acceleration (the curve opencode uses): notches that come fast
// scroll further, so a flick crosses a long transcript quickly while a
// slow notch still moves exactly wheelLines. A streak is notches in one
// direction over one pane, each within accelStreak of the last; the step
// is wheelLines times accelFactor of the average gap between the last
// accelHistory notches, and the fraction a step can't scroll carries to
// the next notch.
const (
	accelStreak  = 150 * time.Millisecond // a longer pause starts a new streak
	accelMinGap  = 6 * time.Millisecond   // closer notches are one burst: not counted
	accelHistory = 3                      // gaps averaged
	accelA       = 0.8
	accelTau     = 3.0
	accelMax     = 6.0
)

// accelFactor is the step multiplier for an average gap avg between
// notches: 1 + A·(e^(100ms/avg/τ) − 1), capped at accelMax.
func accelFactor(avg time.Duration) float64 {
	ms := float64(avg) / float64(time.Millisecond)
	if ms <= 0 {
		return accelMax
	}
	return min(1+accelA*(math.Exp(100/ms/accelTau)-1), accelMax)
}

// wheelAccel is one streak's state: its direction and pane, the time of
// its last counted notch, the recent gaps, and the carried fraction.
type wheelAccel struct {
	dir   int
	pane  pane
	last  time.Time
	gaps  []time.Duration
	carry float64
}

// lines is how far a notch at now in dir (+1 down, -1 up) over p scrolls:
// signed, wheelLines at the start of a streak, more as it speeds up.
func (w *wheelAccel) lines(now time.Time, dir int, p pane) int {
	gap := now.Sub(w.last)
	switch {
	case w.last.IsZero() || dir != w.dir || p != w.pane || gap > accelStreak:
		*w = wheelAccel{dir: dir, pane: p, last: now}
		return dir * wheelLines
	case gap < accelMinGap:
		return dir * wheelLines
	}
	w.last = now
	w.gaps = append(w.gaps, gap)
	if len(w.gaps) > accelHistory {
		w.gaps = w.gaps[1:]
	}
	var sum time.Duration
	for _, g := range w.gaps {
		sum += g
	}
	step := wheelLines*accelFactor(sum/time.Duration(len(w.gaps))) + w.carry
	n := math.Floor(step)
	w.carry = step - n
	return dir * int(n)
}
