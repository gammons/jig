// Package imgrender turns decoded images into terminal cells: protocol
// detection, safe decoding, and rendering fitted to a cell box. It does no
// I/O; anything that must bypass the frame (a kitty upload, a sixel
// payload) is returned as a string for the caller to send with tea.Raw.
package imgrender

import (
	"fmt"
	"image"
	"math"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Default cell size in pixels (R24), used until the terminal reports one.
const (
	DefaultCellWidth  = 8
	DefaultCellHeight = 16
)

// Result is one rendered image.
type Result struct {
	Lines  []string // exactly Rows lines, each of display width Cols
	Upload string   // kitty: APC upload sequence (send once via tea.Raw); "" otherwise
	Sixel  string   // sixel payload to place over the reserved cells; "" otherwise
}

// Renderer renders images for one protocol at one cell size. Callers key
// each image so protocols with terminal-side state (kitty) can reuse it.
type Renderer struct {
	proto Protocol
	cellW int
	cellH int
}

// Option configures a Renderer.
type Option func(*Renderer)

// WithCellSize sets the cell size in pixels (default 8×16). Non-positive
// values are ignored.
func WithCellSize(w, h int) Option {
	return func(r *Renderer) {
		if w > 0 && h > 0 {
			r.cellW, r.cellH = w, h
		}
	}
}

// New returns a Renderer for protocol p.
func New(p Protocol, opts ...Option) *Renderer {
	r := &Renderer{proto: p, cellW: DefaultCellWidth, cellH: DefaultCellHeight}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Protocol reports the protocol r renders with.
func (r *Renderer) Protocol() Protocol { return r.proto }

// Render fits img into maxCols×maxRows cells, keeping its aspect ratio
// and never scaling it past its natural size at the cell size (but always
// at least 1×1 cell). A non-positive box or an empty image gives an empty
// Result. key identifies the image across calls. Kitty and Sixel render
// as Blocks for now.
func (r *Renderer) Render(key string, img image.Image, maxCols, maxRows int) Result {
	if img == nil || maxCols <= 0 || maxRows <= 0 {
		return Result{}
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return Result{}
	}
	if r.proto == Off {
		label := fmt.Sprintf("[image %dx%d]", b.Dx(), b.Dy())
		return Result{Lines: []string{ansi.Truncate(label, maxCols, "…")}}
	}
	cols, rows := fitCells(b.Dx(), b.Dy(), r.cellW, r.cellH, maxCols, maxRows)
	return Result{Lines: halfBlocks(img, cols, rows)}
}

// fitCells scales a w×h px image, at cellW×cellH px per cell, to fit
// maxCols×maxRows cells without upscaling, clamping each side to ≥ 1.
func fitCells(w, h, cellW, cellH, maxCols, maxRows int) (cols, rows int) {
	natCols := float64(w) / float64(cellW)
	natRows := float64(h) / float64(cellH)
	s := min(1, float64(maxCols)/natCols, float64(maxRows)/natRows)
	cols = min(max(int(math.Round(natCols*s)), 1), maxCols)
	rows = min(max(int(math.Round(natRows*s)), 1), maxRows)
	return cols, rows
}

// Place returns the tea.Raw payload drawing res.Sixel at screen cell
// (x, y), 0-based: save the cursor, move, draw, restore. "" if res has no
// sixel payload.
func Place(res Result, x, y int) string {
	if res.Sixel == "" {
		return ""
	}
	return fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", y+1, x+1, res.Sixel)
}
