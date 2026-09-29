package overlay

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Fill returns box with bg as the background of every cell that has no
// background of its own, so a modal box paints a solid panel color even
// across padding and the ANSI resets inside its styled runs. Cells with
// a background keep it. A nil bg returns box unchanged.
func Fill(box string, bg color.Color) string {
	if bg == nil || box == "" {
		return box
	}
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	canvas := lipgloss.NewCanvas(w, h)
	canvas.Compose(lipgloss.NewLayer(box))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Mutate through CellAt's live pointer (never SetCell): see
			// Center for the wide-character reason.
			cell := canvas.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			if cell.Style.Bg == nil {
				cell.Style.Bg = bg
			}
		}
	}
	return canvas.Render()
}
