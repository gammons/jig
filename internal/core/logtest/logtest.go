// Package logtest provides a test slog.Logger that writes debug-level text
// lines, without timestamps, to an in-memory Buffer.
package logtest

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
)

// Buffer is a mutex-guarded io.Writer that collects log output.
type Buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Lines returns the complete lines written so far, without trailing newlines.
func (b *Buffer) Lines() []string {
	b.mu.Lock()
	s := b.buf.String()
	b.mu.Unlock()
	end := strings.LastIndexByte(s, '\n')
	if end < 0 {
		return nil
	}
	return strings.Split(s[:end], "\n")
}

// Find returns the lines whose message is msg: those containing msg="<msg>"
// when msg has a space, else msg=<msg> followed by a space or end of line.
func (b *Buffer) Find(msg string) []string {
	var out []string
	for _, l := range b.Lines() {
		if hasMsg(l, msg) {
			out = append(out, l)
		}
	}
	return out
}

func hasMsg(line, msg string) bool {
	if strings.Contains(msg, " ") {
		return strings.Contains(line, `msg="`+msg+`"`)
	}
	key := "msg=" + msg
	for rest := line; ; {
		i := strings.Index(rest, key)
		if i < 0 {
			return false
		}
		rest = rest[i+len(key):]
		if rest == "" || rest[0] == ' ' {
			return true
		}
	}
}

// New returns a debug-level text logger and the Buffer it writes to. The
// top-level time attribute is dropped so lines are deterministic.
func New() (*slog.Logger, *Buffer) {
	buf := &Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	return slog.New(h), buf
}
