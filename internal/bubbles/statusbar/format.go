package statusbar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// spinnerFrames are the braille dots used for the running indicator.
const spinnerFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// spinnerFrame returns the glyph for frame, wrapping around spinnerFrames.
func spinnerFrame(frame int) rune {
	runes := []rune(spinnerFrames)
	n := len(runes)
	i := frame % n
	if i < 0 {
		i += n
	}
	return runes[i]
}

// formatElapsed renders d as "12s" under a minute, else "3m04s".
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

// formatK renders n in thousands with a "k" suffix at or above 1000, else
// as a plain integer, e.g. 12000 -> "12k", 200000 -> "200k", 42 -> "42".
func formatK(n int64) string {
	if n >= 1000 {
		return strconv.FormatInt(n/1000, 10) + "k"
	}
	return strconv.FormatInt(n, 10)
}

// formatCtx renders "used/limit" through formatK, e.g. "12k/200k".
func formatCtx(used, limit int64) string {
	return formatK(used) + "/" + formatK(limit)
}

// formatCost renders cost as "$0.42".
func formatCost(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

// indicatorsText joins the enabled indicators, or "" when none are set.
func indicatorsText(s State) string {
	var parts []string
	if s.Pending > 0 {
		parts = append(parts, "⚠ "+strconv.Itoa(s.Pending))
	}
	if s.Queued {
		parts = append(parts, "⏳")
	}
	if s.Untrusted {
		parts = append(parts, "untrusted")
	}
	return strings.Join(parts, " ")
}
