// Package media turns user-supplied image bytes into a bounded, model-ready
// image stored in the blob store.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	"golang.org/x/image/webp"

	"github.com/gammons/jig/internal/core"
)

const (
	MaxEdge   = 1568       // longest edge after scaling, in pixels
	JPEGOver  = 1 << 20    // PNG size above which an opaque image becomes JPEG
	MaxBytes  = 5 << 20    // largest encoded result accepted
	MaxPixels = 50_000_000 // largest decoded w*h accepted
	MaxInput  = 20 << 20   // largest input accepted
)

const (
	jpegQuality = 85
	mimePNG     = "image/png"
	mimeJPEG    = "image/jpeg"
	fmtWebp     = "webp"
)

var (
	ErrUnsupported = errors.New("unsupported image format")
	ErrTooLarge    = errors.New("image too large")
)

// BlobStore is the content-addressed store the encoded image goes into.
type BlobStore interface {
	Put(data []byte) (string, error)
}

// Pipeline decodes, scales, re-encodes, and stores images.
type Pipeline struct {
	blobs BlobStore
}

// New returns a Pipeline that stores its results in blobs.
func New(blobs BlobStore) *Pipeline {
	return &Pipeline{blobs: blobs}
}

// Process decodes png/jpeg/gif(first frame)/webp, scales so the long edge is
// ≤ MaxEdge (x/image/draw CatmullRom), re-encodes as PNG, or as JPEG q85
// when that PNG exceeds JPEGOver and the image is fully opaque, refuses
// results > MaxBytes, stores the bytes, and returns the media and the
// post-scaling size.
func (p *Pipeline) Process(data []byte) (core.Media, core.ImageInfo, error) {
	return p.process(data, MaxBytes)
}

func (p *Pipeline) process(data []byte, limit int) (core.Media, core.ImageInfo, error) {
	if len(data) > MaxInput {
		return core.Media{}, core.ImageInfo{}, fmt.Errorf("%w: input is %d bytes (max %d)", ErrTooLarge, len(data), MaxInput)
	}
	format, err := checkConfig(data)
	if err != nil {
		return core.Media{}, core.ImageInfo{}, err
	}
	src, err := decode(format, data)
	if err != nil {
		return core.Media{}, core.ImageInfo{}, err
	}
	if err := checkDims(src.Bounds().Dx(), src.Bounds().Dy()); err != nil {
		return core.Media{}, core.ImageInfo{}, err
	}
	img := scale(src)
	out, mime, err := encode(img)
	if err != nil {
		return core.Media{}, core.ImageInfo{}, err
	}
	if len(out) > limit {
		return core.Media{}, core.ImageInfo{}, fmt.Errorf("%w: encoded image is %d bytes (max %d)", ErrTooLarge, len(out), limit)
	}
	ref, err := p.blobs.Put(out)
	if err != nil {
		return core.Media{}, core.ImageInfo{}, fmt.Errorf("store image: %w", err)
	}
	b := img.Bounds()
	return core.Media{MIME: mime, Ref: ref}, core.ImageInfo{Width: b.Dx(), Height: b.Dy(), Bytes: len(out)}, nil
}

// IsImagePath reports whether path has an image extension Process accepts.
func IsImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func isWebp(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// checkConfig sniffs the format and refuses oversized dimensions from the
// header alone, before anything is decoded.
func checkConfig(data []byte) (format string, err error) {
	defer recoverDecode(&err)
	var cfg image.Config
	if isWebp(data) {
		format = fmtWebp
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	} else {
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
		if errors.Is(err, image.ErrFormat) {
			return "", ErrUnsupported
		}
	}
	if err != nil {
		return "", fmt.Errorf("decode image header: %w", err)
	}
	switch format {
	case "png", "jpeg", "gif", fmtWebp:
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, format)
	}
	return format, checkDims(cfg.Width, cfg.Height)
}

func checkDims(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("decode image: invalid dimensions %dx%d", w, h)
	}
	if int64(w)*int64(h) > MaxPixels {
		return fmt.Errorf("%w: %dx%d exceeds %d pixels", ErrTooLarge, w, h, MaxPixels)
	}
	return nil
}

// decode decodes with the decoder for the sniffed format only, so formats
// registered elsewhere in the binary are never reached.
func decode(format string, data []byte) (img image.Image, err error) {
	defer recoverDecode(&err)
	r := bytes.NewReader(data)
	switch format {
	case "png":
		img, err = png.Decode(r)
	case "jpeg":
		img, err = jpeg.Decode(r)
	case "gif":
		img, err = gif.Decode(r)
	case fmtWebp:
		img, err = webp.Decode(r)
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// recoverDecode turns a decoder panic on hostile input into an error.
func recoverDecode(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("decode image: %v", r)
	}
}
