package core

import (
	"fmt"
	"slices"
	"strings"
)

// Effort is a reasoning effort level. "" means unset: no effort is sent.
type Effort string

// The effort scale, lowest first (see EffortLevels).
const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// EffortLevels returns every level on the scale, lowest first.
func EffortLevels() []Effort {
	return []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
}

// rank is e's position on the scale, or -1 when e is not on it.
func (e Effort) rank() int { return slices.Index(EffortLevels(), e) }

// Known reports whether e is on the scale ("" is not).
func (e Effort) Known() bool { return e.rank() >= 0 }

// ParseEffort parses s, trimmed and lowercased, as an Effort. "" parses
// to "" (unset); anything off the scale is an error naming the levels.
func ParseEffort(s string) (Effort, error) {
	e := Effort(strings.ToLower(strings.TrimSpace(s)))
	if e == "" || e.Known() {
		return e, nil
	}
	names := make([]string, 0, len(EffortLevels()))
	for _, l := range EffortLevels() {
		names = append(names, string(l))
	}
	return "", fmt.Errorf("unknown effort %q (want one of: %s)", s, strings.Join(names, ", "))
}

// EffectiveEffort is the level a request to a model described by info
// carries when want was asked for: "" when the model has no levels; the
// catalog default when want is unset (or off the scale); want itself when
// the model lists it; else the model's level nearest want on the scale,
// the lower one on a tie.
func EffectiveEffort(info ModelInfo, want Effort) Effort {
	if len(info.Efforts) == 0 {
		return ""
	}
	if !want.Known() {
		return info.DefaultEffort
	}
	if slices.Contains(info.Efforts, want) {
		return want
	}
	best, bestDist := Effort(""), -1
	for _, e := range info.Efforts {
		r := e.rank()
		if r < 0 {
			continue
		}
		d := r - want.rank()
		if d < 0 {
			d = -d
		}
		if bestDist < 0 || d < bestDist || (d == bestDist && r < best.rank()) {
			best, bestDist = e, d
		}
	}
	if best == "" {
		return info.DefaultEffort
	}
	return best
}

// LowestEffort is the lowest level info lists, or "" when it lists none.
func LowestEffort(info ModelInfo) Effort {
	var low Effort
	for _, e := range info.Efforts {
		if e.Known() && (low == "" || e.rank() < low.rank()) {
			low = e
		}
	}
	return low
}
