package imgrender

import (
	"image"
	"image/color"
	"strconv"

	"golang.org/x/image/draw"
)

// Transparent pixels are composited over a neutral mid gray: it keeps both
// dark-on-transparent and light-on-transparent artwork visible on light
// and dark terminal themes alike.
const neutral = 0x80

// rasterize scales img to exactly w×h px (ApproxBiLinear; a plain copy
// when the size already matches) over the neutral background, giving an
// opaque RGBA.
func rasterize(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{R: neutral, G: neutral, B: neutral, A: 0xff}), image.Point{}, draw.Src)
	b := img.Bounds()
	if b.Dx() == w && b.Dy() == h {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
		return dst
	}
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// halfBlocks renders img as rows lines of cols '▀' cells: each cell's top
// pixel is the foreground and its bottom pixel the background, as 24-bit
// SGR. Every line ends with a reset.
func halfBlocks(img image.Image, cols, rows int) []string {
	px := rasterize(img, cols, rows*2)
	lines := make([]string, rows)
	buf := make([]byte, 0, cols*40+8)
	for row := range rows {
		buf = buf[:0]
		for x := range cols {
			top := px.RGBAAt(x, row*2)
			bot := px.RGBAAt(x, row*2+1)
			buf = append(buf, "\x1b[38;2;"...)
			buf = appendRGB(buf, top)
			buf = append(buf, ";48;2;"...)
			buf = appendRGB(buf, bot)
			buf = append(buf, "m▀"...)
		}
		buf = append(buf, "\x1b[0m"...)
		lines[row] = string(buf)
	}
	return lines
}

func appendRGB(buf []byte, c color.RGBA) []byte {
	buf = strconv.AppendUint(buf, uint64(c.R), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(c.G), 10)
	buf = append(buf, ';')
	return strconv.AppendUint(buf, uint64(c.B), 10)
}
