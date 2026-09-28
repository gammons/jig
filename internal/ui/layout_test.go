package ui

import (
	"testing"

	"github.com/gammons/jig/internal/bubbles/wintree"
)

func TestLayout_Table(t *testing.T) {
	t.Parallel()
	off := false
	on := true
	const promptH = 3
	tests := []struct {
		name          string
		w, h          int
		pref          *bool
		details       bool
		transW, sideW int
		sideVisible   bool
		narrow        bool
	}{
		{name: "200 sidebar clamps to 50", w: 200, h: 50, transW: 150, sideW: 50, sideVisible: true},
		{name: "150 sidebar 32%", w: 150, h: 40, transW: 102, sideW: 48, sideVisible: true},
		{name: "120 sidebar 32%", w: 120, h: 30, transW: 82, sideW: 38, sideVisible: true},
		{name: "119 no sidebar", w: 119, h: 30, transW: 119, narrow: true},
		{name: "150 details half", w: 150, h: 40, details: true, transW: 75, sideW: 75},
		{name: "100 details full width", w: 100, h: 30, details: true, transW: 0, sideW: 100, narrow: true},
		{name: "200 sidebar pref off", w: 200, h: 50, pref: &off, transW: 200},
		{name: "100 sidebar pref on still hidden", w: 100, h: 30, pref: &on, transW: 100, narrow: true},
		{name: "30 sidebar clamps to 30 is irrelevant when narrow", w: 30, h: 10, transW: 30, narrow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := computeLayout(tt.w, tt.h, promptH, tt.pref, tt.details)
			if r.Transcript.W != tt.transW || r.Side.W != tt.sideW {
				t.Errorf("transcript W = %d, side W = %d; want %d, %d", r.Transcript.W, r.Side.W, tt.transW, tt.sideW)
			}
			if r.SideVisible != tt.sideVisible || r.Narrow != tt.narrow || r.DetailsOpen != tt.details {
				t.Errorf("SideVisible, Narrow, DetailsOpen = %v, %v, %v; want %v, %v, %v",
					r.SideVisible, r.Narrow, r.DetailsOpen, tt.sideVisible, tt.narrow, tt.details)
			}
			top := tt.h - promptH - 1
			wantPrompt := wintree.Rect{X: 0, Y: top, W: tt.w, H: promptH}
			wantStatus := wintree.Rect{X: 0, Y: tt.h - 1, W: tt.w, H: 1}
			if r.Prompt != wantPrompt || r.Status != wantStatus {
				t.Errorf("prompt %+v status %+v; want %+v %+v", r.Prompt, r.Status, wantPrompt, wantStatus)
			}
			if r.Transcript.H != top || (r.Side.W > 0 && (r.Side.H != top || r.Side.X != r.Transcript.W)) {
				t.Errorf("transcript %+v side %+v: want height %d, side right of transcript", r.Transcript, r.Side, top)
			}
		})
	}
}

func TestLayout_TinySizesNeverNegative(t *testing.T) {
	t.Parallel()
	for _, sz := range [][2]int{{0, 0}, {1, 1}, {20, 5}, {5, 2}, {-3, -1}} {
		r := computeLayout(sz[0], sz[1], 3, nil, true)
		for _, rc := range []wintree.Rect{r.Transcript, r.Side, r.Prompt, r.Status} {
			if rc.W < 0 || rc.H < 0 || rc.X < 0 || rc.Y < 0 {
				t.Errorf("%dx%d: negative rect %+v", sz[0], sz[1], rc)
			}
		}
		if total := r.Transcript.H + r.Prompt.H + r.Status.H; total > max(sz[1], 0) {
			t.Errorf("%dx%d: rows %d exceed height", sz[0], sz[1], total)
		}
	}
}
