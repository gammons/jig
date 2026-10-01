package theme

import (
	"math"
	"testing"
)

func TestContrast(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		a, b   string
		want   float64
		wantOK bool
	}{
		{"black on white", "#000000", "#FFFFFF", 21, true},
		{"white on black", "#FFFFFF", "#000000", 21, true},
		{"identical", "#336699", "#336699", 1, true},
		{"bare ANSI index a", "3", "#FFFFFF", 0, false},
		{"bare ANSI index b", "#000000", "9", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Contrast(c.a, c.b)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if diff := math.Abs(got - c.want); diff > 0.01 {
				t.Errorf("Contrast(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestPickMatchColor_ANSIFallsBackToPrimary(t *testing.T) {
	t.Parallel()
	p := Complete(Palette{
		Name: "ansi-test",
		BaseColors: BaseColors{
			Primary: "5", Accent: "6", Warning: "3", Error: "1",
			Background: "0", Surface: "0", SurfaceDark: "0",
			Text: "7", TextMuted: "8", Border: "8",
		},
	})
	if got := pickMatchColor(p); got != p.Primary {
		t.Errorf("pickMatchColor = %q, want Primary %q (a bare ANSI background)", got, p.Primary)
	}
}

func TestPickMatchColor_PicksHighestContrast(t *testing.T) {
	t.Parallel()
	p := Complete(Palette{
		Name: "contrast-test",
		BaseColors: BaseColors{
			// Against a white background, Accent's near-black wins over
			// Warning and Primary's light grays.
			Primary: "#EEEEEE", Accent: "#101010", Warning: "#DDDDDD", Error: "#FF0000",
			Background: "#FFFFFF", Surface: "#FFFFFF", SurfaceDark: "#EEEEEE",
			Text: "#CCCCCC", TextMuted: "#999999", Border: "#AAAAAA",
		},
	})
	if got := pickMatchColor(p); got != p.Accent {
		t.Errorf("pickMatchColor = %q, want Accent %q (the highest-contrast candidate)", got, p.Accent)
	}
}

// TestGaugeTrackVisibleOnEveryTheme: the sidebar gauge's unfilled track
// keeps a visible contrast against Background on every hex built-in
// (Border alone fell to ~1.1 on some).
func TestGaugeTrackVisibleOnEveryTheme(t *testing.T) {
	t.Parallel()
	for _, raw := range Builtin() {
		p := Complete(raw)
		r, ok := Contrast(gaugeTrackColor(p), p.Background)
		if !ok {
			if got := gaugeTrackColor(p); got != p.TextMuted {
				t.Errorf("%s: gaugeTrackColor = %q, want TextMuted %q for an ANSI palette", p.Name, got, p.TextMuted)
			}
			continue
		}
		if r < 1.5 {
			t.Errorf("%s: gauge track contrast %.2f, want >= 1.5", p.Name, r)
		}
	}
}

// TestSoftenedDiffAndSuccessColors: the removed-line background is a
// light wash of Error, not Error itself; successColor sits between
// Accent and TextMuted; and both keep their raw color on an ANSI-16
// palette, where nothing can be mixed.
func TestSoftenedDiffAndSuccessColors(t *testing.T) {
	t.Parallel()
	p := Default()
	bg := removedLineBg(p)
	if bg == p.Error {
		t.Errorf("removedLineBg = Error %q, want a softened tint", bg)
	}
	if r, _ := Contrast(bg, p.Background); r > 1.6 {
		t.Errorf("removedLineBg contrast vs Background = %.2f, want a subtle wash (<= 1.6)", r)
	}
	if got := successColor(p); got == p.Accent || got == p.TextMuted {
		t.Errorf("successColor = %q, want a blend of Accent %q and TextMuted %q", got, p.Accent, p.TextMuted)
	}

	ansiP, _ := Lookup("ANSI Dark", nil)
	ansiP = Complete(ansiP)
	if got := removedLineBg(ansiP); got != ansiP.Error {
		t.Errorf("ANSI removedLineBg = %q, want Error %q", got, ansiP.Error)
	}
	if got := successColor(ansiP); got != ansiP.Accent {
		t.Errorf("ANSI successColor = %q, want Accent %q", got, ansiP.Accent)
	}
}
