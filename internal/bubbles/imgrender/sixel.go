package imgrender

import (
	"image"
	"image/color/palette"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

// renderSixel reserves cols×rows blank cells and encodes img, scaled to
// fill them, as a sixel payload for Place.
func (r *Renderer) renderSixel(img image.Image, cols, rows int) Result {
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = strings.Repeat(" ", cols)
	}
	return Result{Lines: lines, Sixel: encodeSixel(rasterize(img, cols*r.cellW, rows*r.cellH))}
}

// encodeSixel dithers img onto the web-safe palette (Floyd–Steinberg) and
// encodes it as a sixel DCS: raster attributes, the used palette entries
// in index order, then six-row bands, each color's row run-length encoded.
func encodeSixel(img *image.RGBA) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	pal := image.NewPaletted(image.Rect(0, 0, w, h), palette.WebSafe)
	draw.FloydSteinberg.Draw(pal, pal.Bounds(), img, b.Min)

	var used [256]bool
	for _, i := range pal.Pix {
		used[i] = true
	}
	var sb strings.Builder
	sb.WriteString("\x1bPq\"1;1;" + strconv.Itoa(w) + ";" + strconv.Itoa(h))
	for i, ok := range used {
		if !ok {
			continue
		}
		cr, cg, cb, _ := pal.Palette[i].RGBA()
		sb.WriteString("#" + strconv.Itoa(i) + ";2;" + pct(cr) + ";" + pct(cg) + ";" + pct(cb))
	}

	bits := make([][]byte, 256)
	for y0 := 0; y0 < h; y0 += 6 {
		if y0 > 0 {
			sb.WriteByte('-')
		}
		var present [256]bool
		for dy := range min(6, h-y0) {
			row := pal.Pix[(y0+dy)*pal.Stride:]
			for x := range w {
				i := row[x]
				if !present[i] {
					present[i] = true
					if bits[i] == nil {
						bits[i] = make([]byte, w)
					}
					clear(bits[i])
				}
				bits[i][x] |= 1 << dy
			}
		}
		first := true
		for i, ok := range present {
			if !ok {
				continue
			}
			if !first {
				sb.WriteByte('$')
			}
			first = false
			sb.WriteString("#" + strconv.Itoa(i))
			writeSixelRow(&sb, bits[i])
		}
	}
	sb.WriteString("\x1b\\")
	return sb.String()
}

// writeSixelRow writes one color's band row, dropping trailing empty
// columns and run-length encoding runs of four or more.
func writeSixelRow(sb *strings.Builder, row []byte) {
	end := len(row)
	for end > 0 && row[end-1] == 0 {
		end--
	}
	for x := 0; x < end; {
		n := 1
		for x+n < end && row[x+n] == row[x] {
			n++
		}
		c := byte('?' + row[x])
		if n >= 4 {
			sb.WriteString("!" + strconv.Itoa(n))
			sb.WriteByte(c)
		} else {
			for range n {
				sb.WriteByte(c)
			}
		}
		x += n
	}
}

// pct converts a 16-bit color channel to a 0–100 sixel percentage.
func pct(v uint32) string {
	return strconv.Itoa(int((v*100 + 0x7fff) / 0xffff))
}
