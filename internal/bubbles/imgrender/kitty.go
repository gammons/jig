package imgrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strconv"
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// maxKittyID is the largest image id a 24-bit foreground color can carry.
const maxKittyID = 1<<24 - 1

// kittyChunk is the kitty protocol's maximum base64 payload per APC.
const kittyChunk = 4096

// cellSize is the virtual placement size a key's image was last sent at.
type cellSize struct{ cols, rows int }

// renderKitty renders img as unicode-placeholder cells for the image id
// bound to key, with the upload attached whenever (cols, rows) differs
// from the size key was last uploaded at: re-transmitting an id replaces
// the terminal's image and placements, so only the latest size is live.
// Callers hold r.mu.
func (r *Renderer) renderKitty(key string, img image.Image, cols, rows int) Result {
	id, ok := r.ids[key]
	if !ok {
		id = r.nextID
		r.nextID++
		if r.nextID > maxKittyID {
			r.nextID = 1
		}
		r.ids[key] = id
	}
	res := Result{Lines: placeholderLines(id, cols, rows)}
	size := cellSize{cols: cols, rows: rows}
	if cur, ok := r.uploaded[key]; ok && cur == size {
		return res
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, rasterize(img, cols*r.cellW, rows*r.cellH)); err != nil {
		return res
	}
	res.Upload = kittyUpload(id, base64.StdEncoding.EncodeToString(buf.Bytes()), cols, rows, r.tmux)
	r.uploaded[key] = size
	return res
}

// kittyUpload is the APC sequence transmitting a PNG (base64 payload) as
// image id and creating a cols×rows virtual placement for unicode
// placeholders. The payload is split into ≤ 4096-byte chunks; only the
// first carries the keys, and m=1 marks all but the last. Under tmux each
// chunk is wrapped for passthrough.
func kittyUpload(id uint32, payload string, cols, rows int, tmux bool) string {
	var sb strings.Builder
	for i := 0; i < len(payload); i += kittyChunk {
		end := min(i+kittyChunk, len(payload))
		more := "1"
		if end == len(payload) {
			more = "0"
		}
		var seq strings.Builder
		seq.WriteString("\x1b_G")
		if i == 0 {
			seq.WriteString("a=T,f=100,t=d,i=" + strconv.FormatUint(uint64(id), 10) +
				",U=1,c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows) + ",q=2,")
		}
		seq.WriteString("m=" + more + ";" + payload[i:end] + "\x1b\\")
		if tmux {
			sb.WriteString(wrapTmux(seq.String()))
		} else {
			sb.WriteString(seq.String())
		}
	}
	return sb.String()
}

// wrapTmux wraps seq for tmux passthrough, doubling its escapes.
func wrapTmux(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// placeholderLines is rows lines of cols placeholder cells, each carrying
// its row and column diacritics, colored with the image id as a 24-bit
// foreground (R = id>>16, G = id>>8, B = id&0xff) and ending with a reset.
func placeholderLines(id uint32, cols, rows int) []string {
	dia := diacritics()
	sgr := "\x1b[38;2;" + strconv.Itoa(int(id>>16&0xff)) + ";" +
		strconv.Itoa(int(id>>8&0xff)) + ";" + strconv.Itoa(int(id&0xff)) + "m"
	lines := make([]string, rows)
	var sb strings.Builder
	for row := range rows {
		sb.Reset()
		sb.WriteString(sgr)
		for col := range cols {
			sb.WriteRune(ansi.PlaceholderRune)
			sb.WriteRune(dia[row])
			sb.WriteRune(dia[col])
		}
		sb.WriteString("\x1b[0m")
		lines[row] = sb.String()
	}
	return lines
}
