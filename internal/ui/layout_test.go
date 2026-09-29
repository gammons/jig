package ui

import (
	"strings"
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
			top := tt.h - footerRows - promptH - 1
			wantPrompt := wintree.Rect{X: 0, Y: top, W: tt.w, H: promptH}
			wantStatus := wintree.Rect{X: 0, Y: tt.h - footerRows - 1, W: tt.w, H: 1}
			if r.Prompt != wantPrompt || r.Status != wantStatus {
				t.Errorf("prompt %+v status %+v; want %+v %+v", r.Prompt, r.Status, wantPrompt, wantStatus)
			}
			wantFooter := wintree.Rect{X: 0, Y: tt.h - footerRows, W: tt.w, H: footerRows}
			if r.Footer != wantFooter {
				t.Errorf("footer %+v; want %+v (blank rows below the status bar)", r.Footer, wantFooter)
			}
			wantGap := wintree.Rect{X: 0, Y: top - gapRows, W: tt.w, H: gapRows}
			if r.Gap != wantGap {
				t.Errorf("gap %+v; want %+v (blank rows between transcript and prompt)", r.Gap, wantGap)
			}
			transH := top - gapRows
			if r.Transcript.H != transH || (r.Side.W > 0 && (r.Side.H != transH || r.Side.X != r.Transcript.W)) {
				t.Errorf("transcript %+v side %+v: want height %d, side right of transcript", r.Transcript, r.Side, transH)
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
		if total := r.Transcript.H + r.Gap.H + r.Prompt.H + r.Status.H + r.Footer.H; total > max(sz[1], 0) {
			t.Errorf("%dx%d: rows %d exceed height", sz[0], sz[1], total)
		}
	}
}

// TestLayout_GapYieldsFirst: when the terminal is too short, the footer
// and then the gap are dropped before the transcript loses its last row,
// and neither ever takes rows from the prompt or status bar.
func TestLayout_GapYieldsFirst(t *testing.T) {
	t.Parallel()
	const promptH = 3
	tests := []struct {
		h, transH, gapH, promptH, footerH int
	}{
		{h: 7, transH: 1, gapH: 1, promptH: 3, footerH: 1}, // room for everything
		{h: 6, transH: 1, gapH: 1, promptH: 3, footerH: 0}, // footer goes first
		{h: 5, transH: 1, gapH: 0, promptH: 3, footerH: 0}, // then the gap
		{h: 4, transH: 0, gapH: 0, promptH: 3, footerH: 0}, // no transcript, no gap
		{h: 2, transH: 0, gapH: 0, promptH: 2, footerH: 0}, // prompt squeezed, gap still 0
	}
	for _, tt := range tests {
		r := computeLayout(80, tt.h, promptH, nil, false)
		if r.Transcript.H != tt.transH || r.Gap.H != tt.gapH || r.Prompt.H != tt.promptH || r.Footer.H != tt.footerH {
			t.Errorf("h=%d: transcript %d gap %d prompt %d footer %d; want %d %d %d %d",
				tt.h, r.Transcript.H, r.Gap.H, r.Prompt.H, r.Footer.H, tt.transH, tt.gapH, tt.promptH, tt.footerH)
		}
		if r.Footer.H > 0 && r.Footer.Y != r.Status.Y+r.Status.H {
			t.Errorf("h=%d: footer %+v not directly below status %+v", tt.h, r.Footer, r.Status)
		}
	}
}

func TestCompose_GapIsBlankRow(t *testing.T) {
	t.Parallel()
	lay := computeLayout(10, 7, 3, nil, false)
	rows := strings.Split(compose(lay, "T", "", "P", "S"), "\n")
	if len(rows) != 7 {
		t.Fatalf("got %d rows, want 7: %q", len(rows), rows)
	}
	if rows[1] != strings.Repeat(" ", 10) {
		t.Errorf("row 1 = %q, want a blank 10-column gap row", rows[1])
	}
	if !strings.HasPrefix(rows[2], "P") {
		t.Errorf("row 2 = %q, want the prompt right after the gap", rows[2])
	}
}

func TestCompose_FooterIsBlankBottomRow(t *testing.T) {
	t.Parallel()
	lay := computeLayout(10, 7, 3, nil, false)
	rows := strings.Split(compose(lay, "T", "", "P", "S"), "\n")
	if len(rows) != 7 {
		t.Fatalf("got %d rows, want 7: %q", len(rows), rows)
	}
	if !strings.HasPrefix(rows[5], "S") {
		t.Errorf("row 5 = %q, want the status bar just above the footer", rows[5])
	}
	if rows[6] != strings.Repeat(" ", 10) {
		t.Errorf("row 6 = %q, want a blank 10-column footer row", rows[6])
	}
}
