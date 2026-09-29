package ui

import (
	"strings"
	"testing"

	"github.com/gammons/jig/internal/bubbles/ansi"
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
		// Widths are inside the 1-cell margin: a w-wide terminal lays out
		// w-2 columns. Narrow is still decided by the terminal's width.
		{name: "200 sidebar clamps to 50", w: 200, h: 50, transW: 148, sideW: 50, sideVisible: true},
		{name: "150 sidebar 32%", w: 150, h: 40, transW: 101, sideW: 47, sideVisible: true},
		{name: "120 sidebar 32%", w: 120, h: 30, transW: 81, sideW: 37, sideVisible: true},
		{name: "119 no sidebar", w: 119, h: 30, transW: 117, narrow: true},
		{name: "150 details half", w: 150, h: 40, details: true, transW: 74, sideW: 74},
		{name: "100 details full width", w: 100, h: 30, details: true, transW: 0, sideW: 98, narrow: true},
		{name: "200 sidebar pref off", w: 200, h: 50, pref: &off, transW: 198},
		{name: "100 sidebar pref on still hidden", w: 100, h: 30, pref: &on, transW: 98, narrow: true},
		{name: "30 sidebar clamps to 30 is irrelevant when narrow", w: 30, h: 10, transW: 28, narrow: true},
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
			if r.MX != margin || r.MY != margin {
				t.Errorf("margins = %d, %d; want %d on every side", r.MX, r.MY, margin)
			}
			inner := tt.w - 2*margin
			// The status bar is the last row inside the bottom margin,
			// across the full inner width.
			wantStatus := wintree.Rect{X: margin, Y: tt.h - margin - 1, W: inner, H: 1}
			if r.Status != wantStatus {
				t.Errorf("status %+v; want %+v", r.Status, wantStatus)
			}
			// belowRows blank rows separate the prompt from the status bar.
			top := wantStatus.Y - belowRows - promptH
			// With the side slot beside it, the prompt lines up with the
			// transcript's blocks: one column less than the transcript
			// (its scrollbar/border column) and rightPad less again.
			// Otherwise (no side slot, or the details split taking the
			// whole width) it is full width.
			spans := tt.sideW > 0 && tt.transW > 0
			wantPrompt := wintree.Rect{X: margin, Y: top, W: inner, H: promptH}
			if spans {
				wantPrompt.W = tt.transW - 1 - rightPad
			}
			if r.Prompt != wantPrompt {
				t.Errorf("prompt %+v; want %+v", r.Prompt, wantPrompt)
			}
			transH := top - gapRows - margin
			if r.Transcript.H != transH || r.Transcript.X != margin || r.Transcript.Y != margin {
				t.Errorf("transcript %+v: want at (%d,%d), height %d", r.Transcript, margin, margin, transH)
			}
			gapW := inner
			if r.SideSpans {
				gapW = tt.transW
			}
			wantGap := wintree.Rect{X: margin, Y: top - gapRows, W: gapW, H: gapRows}
			if r.Gap != wantGap {
				t.Errorf("gap %+v; want %+v (blank rows between transcript and prompt)", r.Gap, wantGap)
			}
			wantBelow := wintree.Rect{X: margin, Y: top + promptH, W: gapW, H: belowRows}
			if r.Below != wantBelow {
				t.Errorf("below %+v; want %+v (blank rows between prompt and status bar)", r.Below, wantBelow)
			}
			if r.Side.W == 0 {
				return
			}
			// Beside a transcript, the side slot runs from the top margin
			// down beside the gap, the prompt, and the row below it, to
			// the status bar.
			wantSide := wintree.Rect{X: margin + tt.transW, Y: margin, W: tt.sideW, H: transH}
			if tt.transW > 0 {
				wantSide.H = transH + gapRows + promptH + belowRows
			}
			if r.Side != wantSide {
				t.Errorf("side %+v; want %+v", r.Side, wantSide)
			}
			if r.SideSpans != spans {
				t.Errorf("SideSpans = %v, want %v", r.SideSpans, spans)
			}
		})
	}
}

func TestLayout_TinySizesNeverNegative(t *testing.T) {
	t.Parallel()
	for _, sz := range [][2]int{{0, 0}, {1, 1}, {20, 5}, {5, 2}, {-3, -1}} {
		r := computeLayout(sz[0], sz[1], 3, nil, true)
		for _, rc := range []wintree.Rect{r.Transcript, r.Side, r.Prompt, r.Status, r.Gap, r.Below} {
			if rc.W < 0 || rc.H < 0 || rc.X < 0 || rc.Y < 0 {
				t.Errorf("%dx%d: negative rect %+v", sz[0], sz[1], rc)
			}
		}
		if total := 2*r.MY + r.Transcript.H + r.Gap.H + r.Prompt.H + r.Below.H + r.Status.H; total > max(sz[1], 0) {
			t.Errorf("%dx%d: rows %d exceed height", sz[0], sz[1], total)
		}
		if total := 2*r.MX + max(r.Transcript.W+r.Side.W, r.Status.W); total > max(sz[0], 0) {
			t.Errorf("%dx%d: columns %d exceed width", sz[0], sz[1], total)
		}
	}
}

// TestLayout_MarginAndGapsYieldFirst: when the terminal is too short, the
// top/bottom margin goes first, then the row below the prompt, then the
// gap above it, before the transcript loses its last row; none ever takes
// rows from the prompt or status bar.
func TestLayout_MarginAndGapsYieldFirst(t *testing.T) {
	t.Parallel()
	const promptH = 3
	tests := []struct {
		h, transH, gapH, belowH, promptH, my int
	}{
		{h: 9, transH: 1, gapH: 1, belowH: 1, promptH: 3, my: 1}, // room for everything
		{h: 8, transH: 2, gapH: 1, belowH: 1, promptH: 3, my: 0}, // the margin goes first
		{h: 7, transH: 1, gapH: 1, belowH: 1, promptH: 3, my: 0},
		{h: 6, transH: 1, gapH: 1, belowH: 0, promptH: 3, my: 0}, // then the row below the prompt
		{h: 5, transH: 1, gapH: 0, belowH: 0, promptH: 3, my: 0}, // then the gap
		{h: 4, transH: 0, gapH: 0, belowH: 0, promptH: 3, my: 0}, // no transcript, no gaps
		{h: 2, transH: 0, gapH: 0, belowH: 0, promptH: 2, my: 0}, // prompt squeezed, gaps still 0
	}
	for _, tt := range tests {
		r := computeLayout(80, tt.h, promptH, nil, false)
		if r.Transcript.H != tt.transH || r.Gap.H != tt.gapH || r.Below.H != tt.belowH || r.Prompt.H != tt.promptH || r.MY != tt.my {
			t.Errorf("h=%d: transcript %d gap %d below %d prompt %d margin %d; want %d %d %d %d %d",
				tt.h, r.Transcript.H, r.Gap.H, r.Below.H, r.Prompt.H, r.MY, tt.transH, tt.gapH, tt.belowH, tt.promptH, tt.my)
		}
		if r.Below.H > 0 && (r.Below.Y != r.Prompt.Y+r.Prompt.H || r.Status.Y != r.Below.Y+r.Below.H) {
			t.Errorf("h=%d: below %+v not between prompt %+v and status %+v", tt.h, r.Below, r.Prompt, r.Status)
		}
	}
}

// TestLayout_SideMarginYieldsWhenNarrow: a terminal too narrow for a
// useful body drops the left/right margin rather than squeezing it.
func TestLayout_SideMarginYieldsWhenNarrow(t *testing.T) {
	t.Parallel()
	if r := computeLayout(marginMinW, 20, 3, nil, false); r.MX != margin {
		t.Errorf("w=%d: MX = %d, want %d", marginMinW, r.MX, margin)
	}
	r := computeLayout(marginMinW-1, 20, 3, nil, false)
	if r.MX != 0 || r.Transcript.X != 0 || r.Transcript.W != marginMinW-1 {
		t.Errorf("w=%d: MX = %d, transcript %+v; want no side margin, full width", marginMinW-1, r.MX, r.Transcript)
	}
}

// TestCompose_MarginOnEverySide: the frame is exactly w×h, with a blank
// first and last row and a blank first and last column around the body.
func TestCompose_MarginOnEverySide(t *testing.T) {
	t.Parallel()
	lay := computeLayout(30, 10, 3, nil, false)
	rows := strings.Split(compose(lay, "T", "", "P", "S", "|"), "\n")
	if len(rows) != 10 {
		t.Fatalf("got %d rows, want 10: %q", len(rows), rows)
	}
	blank := strings.Repeat(" ", 30)
	if rows[0] != blank || rows[9] != blank {
		t.Errorf("first/last rows = %q / %q, want blank margins", rows[0], rows[9])
	}
	for i, r := range rows {
		if len([]rune(r)) != 30 {
			t.Errorf("row %d is %d cells, want 30: %q", i, len([]rune(r)), r)
		}
		if !strings.HasPrefix(r, " ") || !strings.HasSuffix(r, " ") {
			t.Errorf("row %d = %q, want a blank first and last column", i, r)
		}
	}
	if !strings.HasPrefix(rows[1], " T") {
		t.Errorf("row 1 = %q, want the transcript right inside the margin", rows[1])
	}
	if !strings.HasPrefix(rows[8], " S") {
		t.Errorf("row 8 = %q, want the status bar just above the bottom margin", rows[8])
	}
	if strings.TrimSpace(rows[7]) != "" {
		t.Errorf("row 7 = %q, want a blank row between the prompt and the status bar", rows[7])
	}
	if !strings.HasPrefix(rows[6], " P") && strings.TrimSpace(rows[6]) != "" {
		t.Errorf("row 6 = %q, want the prompt's last row", rows[6])
	}
}

// TestCompose_SideRunsBesideThePrompt: with a sidebar, the gap, prompt,
// and below-prompt rows carry the transcript's border column, then the
// side slot, which runs down to the status bar; the status bar spans the
// full width.
func TestCompose_SideRunsBesideThePrompt(t *testing.T) {
	t.Parallel()
	lay := computeLayout(130, 12, 3, nil, false)
	if !lay.SideSpans {
		t.Fatalf("layout %+v: want the side slot spanning down beside the prompt", lay)
	}
	side := strings.Repeat("S\n", lay.Side.H)
	rows := strings.Split(compose(lay, "T", side, "P", "STATUS", "|"), "\n")
	bx := lay.Transcript.X + lay.Transcript.W - 1 // the border column
	for y := lay.Gap.Y; y < lay.Below.Y+lay.Below.H; y++ {
		r := []rune(rows[y])
		if r[bx] != '|' {
			t.Errorf("row %d col %d = %q, want the border beside the gap/prompt: %q", y, bx, r[bx], rows[y])
		}
		if r[lay.Side.X] != 'S' {
			t.Errorf("row %d col %d = %q, want the side slot beside the gap/prompt", y, lay.Side.X, r[lay.Side.X])
		}
	}
	if !strings.HasPrefix(rows[lay.Prompt.Y], " P") {
		t.Errorf("prompt row = %q, want the prompt inside the margin", rows[lay.Prompt.Y])
	}
	if !strings.HasPrefix(rows[lay.Status.Y], " STATUS") {
		t.Errorf("status row = %q, want the full-width status bar", rows[lay.Status.Y])
	}
}

// TestCompose_ExactFrameSize: whatever the regions hold (wide runes,
// styles, tabs, lines far too long or too many), the frame is exactly
// w×h cells, for every layout shape.
func TestCompose_ExactFrameSize(t *testing.T) {
	t.Parallel()
	off := false
	lines := func(n int, s string) string { return strings.TrimSuffix(strings.Repeat(s+"\n", n), "\n") }
	long := "\x1b[31m世界🙂\x1b[0m\tx" + strings.Repeat("w", 300)
	for _, sz := range [][2]int{{200, 50}, {130, 12}, {119, 30}, {30, 10}, {19, 6}, {5, 3}} {
		for _, pref := range []*bool{nil, &off} {
			for _, details := range []bool{false, true} {
				lay := computeLayout(sz[0], sz[1], 3, pref, details)
				out := compose(lay, lines(sz[1]+5, long), lines(sz[1]+5, long), lines(4, long), long, "\x1b[34m│\x1b[0m")
				rows := strings.Split(out, "\n")
				if len(rows) != sz[1] {
					t.Fatalf("%v pref=%v details=%v: %d rows, want %d", sz, pref, details, len(rows), sz[1])
				}
				for i, r := range rows {
					if w := ansi.Width(r); w != sz[0] {
						t.Errorf("%v pref=%v details=%v: row %d is %d cells, want %d", sz, pref, details, i, w, sz[0])
					}
				}
			}
		}
	}
}
