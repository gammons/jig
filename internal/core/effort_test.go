package core

import (
	"strings"
	"testing"
)

func TestParseEffort(t *testing.T) {
	tests := []struct {
		in      string
		want    Effort
		wantErr bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"high", EffortHigh, false},
		{"High", EffortHigh, false},
		{" xhigh ", EffortXHigh, false},
		{"none", EffortNone, false},
		{"turbo", "", true},
		{"hi", "", true},
	}
	for _, tt := range tests {
		got, err := ParseEffort(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseEffort(%q) = %q, %v; want %q, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
	_, err := ParseEffort("turbo")
	if err == nil || !strings.Contains(err.Error(), "none, minimal, low, medium, high, xhigh, max") {
		t.Errorf("ParseEffort(turbo) err = %v, want it to list the levels", err)
	}
}

func TestEffortKnown(t *testing.T) {
	for _, e := range EffortLevels() {
		if !e.Known() {
			t.Errorf("%q.Known() = false", e)
		}
	}
	for _, e := range []Effort{"", "turbo", "High"} {
		if e.Known() {
			t.Errorf("%q.Known() = true", e)
		}
	}
}

func TestEffectiveEffort(t *testing.T) {
	levels := ModelInfo{Efforts: []Effort{EffortLow, EffortMedium, EffortHigh}, DefaultEffort: EffortMedium}
	sparse := ModelInfo{Efforts: []Effort{EffortLow, EffortHigh}, DefaultEffort: EffortHigh}
	noDefault := ModelInfo{Efforts: []Effort{EffortLow, EffortHigh}}
	tests := []struct {
		name string
		info ModelInfo
		want Effort
		out  Effort
	}{
		{"no levels, no want", ModelInfo{}, "", ""},
		{"no levels, want high", ModelInfo{DefaultEffort: EffortHigh}, EffortHigh, ""},
		{"unset uses default", levels, "", EffortMedium},
		{"levels, no default", noDefault, "", ""},
		{"exact", levels, EffortLow, EffortLow},
		{"clamp down", levels, EffortMax, EffortHigh},
		{"clamp up", levels, EffortNone, EffortLow},
		{"tie goes lower", sparse, EffortMedium, EffortLow},
		{"unknown want uses default", levels, "turbo", EffortMedium},
	}
	for _, tt := range tests {
		if got := EffectiveEffort(tt.info, tt.want); got != tt.out {
			t.Errorf("%s: EffectiveEffort(%v, %q) = %q, want %q", tt.name, tt.info.Efforts, tt.want, got, tt.out)
		}
	}
}

func TestLowestEffort(t *testing.T) {
	if got := LowestEffort(ModelInfo{}); got != "" {
		t.Errorf("no levels: %q, want empty", got)
	}
	info := ModelInfo{Efforts: []Effort{EffortMedium, EffortLow, EffortHigh}}
	if got := LowestEffort(info); got != EffortLow {
		t.Errorf("LowestEffort = %q, want low", got)
	}
}
