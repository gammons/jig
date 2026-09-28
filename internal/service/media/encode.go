package media

import (
	"bytes"
	"encoding/base64"
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

// fit encodes img (see encode) so its base64 size is ≤ limit. A PNG over
// the limit (only possible for a non-opaque image, since an opaque one
// over JPEGOver is already JPEG) is flattened onto white and retried as
// JPEG; a result still over it is ErrTooLarge.
func fit(img *image.RGBA, limit int) ([]byte, string, error) {
	out, mime, err := encode(img)
	if err != nil {
		return nil, "", err
	}
	if fits(out, limit) {
		return out, mime, nil
	}
	if mime == mimePNG {
		if out, err = encodeJPEG(flatten(img)); err != nil {
			return nil, "", err
		}
		if fits(out, limit) {
			return out, mimeJPEG, nil
		}
	}
	return nil, "", fmt.Errorf("%w: encoded image is %d bytes as base64 (max %d)", ErrTooLarge, base64.StdEncoding.EncodedLen(len(out)), limit)
}

func fits(out []byte, limit int) bool {
	return base64.StdEncoding.EncodedLen(len(out)) <= limit
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
	out, err := encodeJPEG(img)
	return out, mimeJPEG, err
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// flatten composites img over an opaque white background.
func flatten(img *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	return dst
}
