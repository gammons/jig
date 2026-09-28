package theme

import "math"

// Contrast returns the WCAG 2.x contrast ratio between two colors, each
// "#RRGGBB" (1 for identical luminance, 21 for black on white). ok is
// false if either color isn't hex (e.g. a bare ANSI-16 index), which this
// package has no RGB value for.
func Contrast(a, b string) (ratio float64, ok bool) {
	ar, ag, ab, ok1 := parseHex(a)
	br, bg, bb, ok2 := parseHex(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	la, lb := relLuminance(ar, ag, ab), relLuminance(br, bg, bb)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), true
}

// relLuminance is a color's relative luminance per WCAG 2.x: linearize
// each sRGB channel, then weight by how the eye perceives it.
func relLuminance(r, g, b uint8) float64 {
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b)
}

// linearize converts an 8-bit sRGB channel to linear light.
func linearize(c uint8) float64 {
	cs := float64(c) / 255
	if cs <= 0.04045 {
		return cs / 12.92
	}
	return math.Pow((cs+0.055)/1.055, 2.4)
}

// pickMatchColor picks the picker's matched-rune color: whichever of
// Warning, Primary, Accent, or Text (as a last resort, since it always
// contrasts with a readable Background but defeats the point of a
// highlight on its own) has the highest WCAG contrast against Background.
// A bare ANSI-16 index Background has no known RGB, so those palettes
// fall back to Primary, matching the rest of the picker's chrome.
func pickMatchColor(p Palette) string {
	if _, _, _, ok := parseHex(p.Background); !ok {
		return p.Primary
	}
	best, bestRatio := p.Primary, -1.0
	for _, c := range []string{p.Warning, p.Primary, p.Accent, p.Text} {
		if ratio, ok := Contrast(c, p.Background); ok && ratio > bestRatio {
			best, bestRatio = c, ratio
		}
	}
	return best
}
