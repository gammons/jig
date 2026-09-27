// Package ids generates sortable, prefixed identifiers: a lowercase hex
// timestamp component (so IDs generated later sort after earlier ones)
// followed by random bits for uniqueness within the same millisecond.
package ids

import (
	"encoding/base32"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/gammons/jig/internal/clock"
)

// Gen generates IDs. It is safe for concurrent use.
type Gen struct {
	mu  sync.Mutex
	clk clock.Clock
	rnd io.Reader
}

// New returns a Gen that reads timestamps from clk and randomness from rnd.
func New(clk clock.Clock, rnd io.Reader) *Gen {
	return &Gen{clk: clk, rnd: rnd}
}

// randChars is the number of base32 characters drawn from rnd per ID. Each
// base32 character encodes 5 bits, so 8 random bytes (64 bits) are enough
// to produce 10 characters without padding.
const randChars = 10

// Next returns a new ID: prefix + "_" + 12 lowercase hex digits of the
// current unix-ms timestamp (zero-padded) + 10 lowercase base32 characters
// read from rnd. IDs generated later sort lexically after IDs generated
// earlier, because the timestamp component is fixed-width and left-padded.
func (g *Gen) Next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	ms := g.clk.Now().UnixMilli()
	hexPart := fmt.Sprintf("%012x", uint64(ms))

	buf := make([]byte, 8)
	if _, err := io.ReadFull(g.rnd, buf); err != nil {
		panic(fmt.Sprintf("ids: reading random bytes: %v", err))
	}
	randPart := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf))[:randChars]

	return prefix + "_" + hexPart + randPart
}
