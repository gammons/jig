package shell

import "os"

// spillMode is the file mode used for a Spec.SpillPath file.
const spillMode = 0o600

// capture is an io.Writer that a running command's combined stdout+stderr
// is written to. It tracks the total byte count, optionally mirrors every
// write to a spill file on disk, and optionally keeps only the trailing
// tailBytes of the data in memory.
type capture struct {
	tailBytes int
	buf       []byte
	total     int64
	spill     *os.File
}

// newCapture returns a capture ready to receive writes. If spillPath is
// non-empty, it is created (or truncated) with mode 0o600 and every write
// is mirrored to it.
func newCapture(tailBytes int, spillPath string) (*capture, error) {
	c := &capture{tailBytes: tailBytes}
	if tailBytes > 0 {
		c.buf = make([]byte, 0, tailBytes)
	}
	if spillPath != "" {
		f, err := os.OpenFile(spillPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, spillMode)
		if err != nil {
			return nil, err
		}
		c.spill = f
	}
	return c, nil
}

// Write implements io.Writer. It always succeeds from the caller's
// perspective (a spill-file write error does not fail the command).
func (c *capture) Write(p []byte) (int, error) {
	c.total += int64(len(p))
	if c.spill != nil {
		_, _ = c.spill.Write(p)
	}
	if c.tailBytes > 0 {
		c.appendTail(p)
	} else {
		c.buf = append(c.buf, p...)
	}
	return len(p), nil
}

// appendTail appends p to buf, keeping only the last c.tailBytes bytes.
func (c *capture) appendTail(p []byte) {
	if len(p) >= c.tailBytes {
		c.buf = append(c.buf[:0], p[len(p)-c.tailBytes:]...)
		return
	}
	if len(c.buf)+len(p) <= c.tailBytes {
		c.buf = append(c.buf, p...)
		return
	}
	overflow := len(c.buf) + len(p) - c.tailBytes
	copy(c.buf, c.buf[overflow:])
	c.buf = append(c.buf[:len(c.buf)-overflow], p...)
}

// tail returns the bytes currently retained in memory.
func (c *capture) tail() []byte { return c.buf }

// Close closes the spill file, if any.
func (c *capture) Close() error {
	if c.spill != nil {
		return c.spill.Close()
	}
	return nil
}
