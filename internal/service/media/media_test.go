package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"testing"
)

type fakeBlobs struct {
	puts [][]byte
}

func (f *fakeBlobs) Put(data []byte) (string, error) {
	f.puts = append(f.puts, bytes.Clone(data))
	return "ref", nil
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func solid(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	return img
}

func noise(w, h int) *image.RGBA {
	r := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		if i%4 == 3 {
			img.Pix[i] = 0xff
			continue
		}
		img.Pix[i] = uint8(r.UintN(256))
	}
	return img
}

func decodeStored(t *testing.T, fb *fakeBlobs) image.Image {
	t.Helper()
	if len(fb.puts) != 1 {
		t.Fatalf("puts = %d, want 1", len(fb.puts))
	}
	img, _, err := image.Decode(bytes.NewReader(fb.puts[0]))
	if err != nil {
		t.Fatalf("decode stored: %v", err)
	}
	return img
}

func TestProcess_ScalesLongEdge(t *testing.T) {
	fb := &fakeBlobs{}
	m, info, err := New(fb).Process(encodePNG(t, solid(3000, 1000)))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if info.Width != 1568 || info.Height != 523 {
		t.Fatalf("info = %+v, want 1568x523", info)
	}
	if m.MIME != "image/png" || m.Ref != "ref" {
		t.Fatalf("media = %+v", m)
	}
	if info.Bytes != len(fb.puts[0]) {
		t.Fatalf("Bytes = %d, want %d", info.Bytes, len(fb.puts[0]))
	}
	b := decodeStored(t, fb).Bounds()
	if b.Dx() != 1568 || b.Dy() != 523 {
		t.Fatalf("stored = %v", b)
	}
}

func TestProcess_SmallImageUnscaled(t *testing.T) {
	fb := &fakeBlobs{}
	_, info, err := New(fb).Process(encodePNG(t, solid(10, 10)))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if info.Width != 10 || info.Height != 10 {
		t.Fatalf("info = %+v", info)
	}
}

func TestProcess_TallImageScales(t *testing.T) {
	fb := &fakeBlobs{}
	var buf bytes.Buffer
	pal := image.NewPaletted(image.Rect(0, 0, 100, 2000), color.Palette{color.Black, color.White})
	if err := gif.Encode(&buf, pal, nil); err != nil {
		t.Fatal(err)
	}
	m, info, err := New(fb).Process(buf.Bytes())
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if info.Width != 78 || info.Height != 1568 || m.MIME != "image/png" {
		t.Fatalf("info = %+v media = %+v", info, m)
	}
}

func TestProcess_JPEGInput(t *testing.T) {
	fb := &fakeBlobs{}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solid(20, 30), nil); err != nil {
		t.Fatal(err)
	}
	_, info, err := New(fb).Process(buf.Bytes())
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if info.Width != 20 || info.Height != 30 {
		t.Fatalf("info = %+v", info)
	}
}

func TestProcess_OpaquePhotoBecomesJPEG(t *testing.T) {
	fb := &fakeBlobs{}
	m, info, err := New(fb).Process(encodePNG(t, noise(1500, 1500)))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if m.MIME != "image/jpeg" {
		t.Fatalf("MIME = %q", m.MIME)
	}
	if info.Width != 1500 || info.Height != 1500 {
		t.Fatalf("info = %+v", info)
	}
	if _, format, err := image.Decode(bytes.NewReader(fb.puts[0])); err != nil || format != "jpeg" {
		t.Fatalf("stored format = %q, err = %v", format, err)
	}
}

// TestProcess_TransparentPhotoFlattensToJPEG: a non-opaque image whose
// PNG is over the budget is flattened onto white and sent as JPEG q85
// when that fits, rather than refused.
func TestProcess_TransparentPhotoFlattensToJPEG(t *testing.T) {
	fb := &fakeBlobs{}
	img := noise(1500, 1500)
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 0, 0, 0, 0
	m, info, err := New(fb).Process(encodePNG(t, img))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if m.MIME != "image/jpeg" || info.Width != 1500 || info.Height != 1500 {
		t.Fatalf("media = %+v info = %+v, want a 1500x1500 JPEG", m, info)
	}
	if n := base64.StdEncoding.EncodedLen(info.Bytes); n > MaxBytes {
		t.Fatalf("base64 size = %d, want <= %d", n, MaxBytes)
	}
	stored := decodeStored(t, fb)
	if r, g, b, _ := stored.At(0, 0).RGBA(); r>>8 < 0xe0 || g>>8 < 0xe0 || b>>8 < 0xe0 {
		t.Errorf("transparent pixel = %d,%d,%d, want flattened to (near) white", r>>8, g>>8, b>>8)
	}
}

// TestProcess_LimitIsOnBase64Size pins that the budget is measured on the
// base64 encoding providers see, not on the raw bytes.
func TestProcess_LimitIsOnBase64Size(t *testing.T) {
	raw := encodePNG(t, solid(10, 10))
	fb := &fakeBlobs{}
	if _, _, err := New(fb).process(raw, len(raw)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("raw-sized limit: err = %v, want ErrTooLarge", err)
	}
	if _, _, err := New(fb).process(raw, base64.StdEncoding.EncodedLen(len(raw))); err != nil {
		t.Fatalf("base64-sized limit: err = %v, want nil", err)
	}
}

func TestProcess_TransparentStillTooLargeAfterFlatten(t *testing.T) {
	fb := &fakeBlobs{}
	img := noise(50, 50)
	img.Pix[3] = 0
	_, _, err := New(fb).process(encodePNG(t, img), 100)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if len(fb.puts) != 0 {
		t.Fatalf("puts = %d, want 0", len(fb.puts))
	}
}

func TestProcess_Webp(t *testing.T) {
	data, err := os.ReadFile("testdata/1x1.webp")
	if err != nil {
		t.Fatal(err)
	}
	fb := &fakeBlobs{}
	m, info, err := New(fb).Process(data)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if m.MIME != "image/png" || info.Width != 1 || info.Height != 1 {
		t.Fatalf("media = %+v info = %+v", m, info)
	}
	decodeStored(t, fb)
}

func patchIHDR(t *testing.T, data []byte, w, h uint32) []byte {
	t.Helper()
	out := bytes.Clone(data)
	// signature(8) + length(4) + "IHDR"(4) + width(4) + height(4) ...
	if string(out[12:16]) != "IHDR" {
		t.Fatalf("no IHDR at 12")
	}
	binary.BigEndian.PutUint32(out[16:20], w)
	binary.BigEndian.PutUint32(out[20:24], h)
	crc := crc32.ChecksumIEEE(out[12:29])
	binary.BigEndian.PutUint32(out[29:33], crc)
	return out
}

func TestProcess_RefusesHugeDimensions(t *testing.T) {
	data := patchIHDR(t, encodePNG(t, solid(1, 1)), 100000, 100000)
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		t.Fatalf("patched header should parse: %v", err)
	}
	fb := &fakeBlobs{}
	_, _, err := New(fb).Process(data)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_RefusesHugeInput(t *testing.T) {
	_, _, err := New(&fakeBlobs{}).Process(make([]byte, MaxInput+1))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_CorruptImageIsError(t *testing.T) {
	fb := &fakeBlobs{}
	full := encodePNG(t, noise(50, 50))
	if _, _, err := New(fb).Process(full[:len(full)/2]); err == nil {
		t.Fatal("truncated png: want error")
	}
	_, _, err := New(fb).Process([]byte("not an image"))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	webp, err := os.ReadFile("testdata/1x1.webp")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(fb).Process(webp[:len(webp)-6]); err == nil {
		t.Fatal("truncated webp: want error")
	}
	if len(fb.puts) != 0 {
		t.Fatalf("puts = %d, want 0", len(fb.puts))
	}
}

func TestProcess_TooLargeAfterEncode(t *testing.T) {
	fb := &fakeBlobs{}
	_, _, err := New(fb).process(encodePNG(t, noise(50, 50)), 100)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if len(fb.puts) != 0 {
		t.Fatalf("puts = %d, want 0", len(fb.puts))
	}
}

func TestProcess_PutError(t *testing.T) {
	_, _, err := New(errBlobs{}).Process(encodePNG(t, solid(2, 2)))
	if err == nil {
		t.Fatal("want error")
	}
}

type errBlobs struct{}

func (errBlobs) Put([]byte) (string, error) { return "", errors.New("disk full") }

func TestIsImagePath(t *testing.T) {
	cases := map[string]bool{
		"a.png": true, "a.PNG": true, "dir/b.jpg": true, "c.JPEG": true,
		"d.gif": true, "e.webp": true, "f.bmp": false, "png": false,
		"g.png.txt": false, "": false, "h.": false,
	}
	for p, want := range cases {
		if got := IsImagePath(p); got != want {
			t.Errorf("IsImagePath(%q) = %v, want %v", p, got, want)
		}
	}
}
