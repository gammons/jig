package imgrender

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/webp"
)

// MaxPixels is the largest w*h Decode accepts, checked from the header
// before any pixel is decoded.
const MaxPixels = 50_000_000

var (
	ErrUnsupported = errors.New("unsupported image format")
	ErrTooLarge    = errors.New("image too large")
)

type format int

const (
	fmtUnknown format = iota
	fmtPNG
	fmtJPEG
	fmtGIF
	fmtWebp
)

// Decode decodes png, jpeg, gif (first frame), or webp. The format is
// sniffed from the magic bytes and only that decoder runs, so decoders
// registered elsewhere in the binary are never reached. Dimensions over
// MaxPixels are refused from the header alone, and a decoder panic on
// hostile input becomes an error.
func Decode(data []byte) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("decode image: %v", r)
		}
	}()
	f := sniff(data)
	if f == fmtUnknown {
		return nil, ErrUnsupported
	}
	cfg, err := decodeConfig(f, data)
	if err != nil {
		return nil, fmt.Errorf("decode image header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("decode image: invalid dimensions %dx%d", cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("%w: %dx%d exceeds %d pixels", ErrTooLarge, cfg.Width, cfg.Height, MaxPixels)
	}
	img, err = decodeFormat(f, data)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

func sniff(data []byte) format {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return fmtPNG
	case bytes.HasPrefix(data, []byte("\xff\xd8")):
		return fmtJPEG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return fmtGIF
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return fmtWebp
	}
	return fmtUnknown
}

func decodeConfig(f format, data []byte) (image.Config, error) {
	r := bytes.NewReader(data)
	switch f {
	case fmtPNG:
		return png.DecodeConfig(r)
	case fmtJPEG:
		return jpeg.DecodeConfig(r)
	case fmtGIF:
		return gif.DecodeConfig(r)
	case fmtWebp:
		return webp.DecodeConfig(r)
	}
	return image.Config{}, ErrUnsupported
}

func decodeFormat(f format, data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	switch f {
	case fmtPNG:
		return png.Decode(r)
	case fmtJPEG:
		return jpeg.Decode(r)
	case fmtGIF:
		return gif.Decode(r)
	case fmtWebp:
		return webp.Decode(r)
	}
	return nil, ErrUnsupported
}
