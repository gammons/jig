package media

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

// scale copies src into a fresh RGBA image whose long edge is ≤ MaxEdge,
// using CatmullRom when it has to shrink.
func scale(src image.Image) *image.RGBA {
	sb := src.Bounds()
	w, h := fitEdge(sb.Dx(), sb.Dy())
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == sb.Dx() && h == sb.Dy() {
		draw.Draw(dst, dst.Bounds(), src, sb.Min, draw.Src)
		return dst
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Src, nil)
	return dst
}

// fitEdge returns w×h scaled so the long edge is ≤ MaxEdge, rounding the
// short edge to nearest and never below 1.
func fitEdge(w, h int) (int, int) {
	switch {
	case w <= MaxEdge && h <= MaxEdge:
		return w, h
	case w >= h:
		return MaxEdge, max(1, int((int64(h)*MaxEdge+int64(w)/2)/int64(w)))
	default:
		return max(1, int((int64(w)*MaxEdge+int64(h)/2)/int64(h))), MaxEdge
	}
}

// encode writes img as PNG, or as JPEG when that PNG exceeds JPEGOver and
// img is fully opaque.
func encode(img *image.RGBA) ([]byte, string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("encode png: %w", err)
	}
	if buf.Len() <= JPEGOver || !img.Opaque() {
		return buf.Bytes(), mimePNG, nil
	}
	buf.Reset()
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, "", fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), mimeJPEG, nil
}
