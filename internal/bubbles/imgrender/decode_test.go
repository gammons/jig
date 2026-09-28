package imgrender

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func TestDecode_Webp(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/1x1.webp")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Errorf("bounds = %v, want 1x1", b)
	}
}

func TestDecode_PngJpegGif(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	encoders := map[string]func(*bytes.Buffer) error{
		"png":  func(b *bytes.Buffer) error { return png.Encode(b, src) },
		"jpeg": func(b *bytes.Buffer) error { return jpeg.Encode(b, src, nil) },
		"gif":  func(b *bytes.Buffer) error { return gif.Encode(b, src, nil) },
	}
	for name, enc := range encoders {
		var buf bytes.Buffer
		if err := enc(&buf); err != nil {
			t.Fatalf("%s encode: %v", name, err)
		}
		img, err := Decode(buf.Bytes())
		if err != nil {
			t.Errorf("%s: Decode: %v", name, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != 3 || b.Dy() != 2 {
			t.Errorf("%s: bounds = %v, want 3x2", name, b)
		}
	}
}

func TestDecode_Unsupported(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{nil, []byte("not an image"), []byte("BM\x00\x00\x00\x00")} {
		if _, err := Decode(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Decode(%q) err = %v, want ErrUnsupported", data, err)
		}
	}
}

// A GIF's logical screen size is read from the header alone, so a tiny
// file can claim 65535×65535 pixels; Decode must refuse it before decoding.
func TestDecode_RefusesDecompressionBomb(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	copy(data[6:10], []byte{0xff, 0xff, 0xff, 0xff})
	if _, err := Decode(data); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestDecode_Truncated(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if _, err := Decode(data[:len(data)-20]); err == nil {
		t.Error("truncated png: want error")
	}
}
