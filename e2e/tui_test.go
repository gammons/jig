//go:build jigtest

package e2e

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// tuiTimeout bounds the whole pty smoke test.
const tuiTimeout = 20 * time.Second

// screen accumulates pty output; waitFor blocks on a sync.Cond until
// ansi.Strip(output) contains s, or ctx ends.
type screen struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  []byte
	done bool // the reader hit EOF or an error
}

func newScreen() *screen {
	s := &screen{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// read copies r into the buffer until r fails, waking waiters on every
// chunk.
func (s *screen) read(r io.Reader) {
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		s.mu.Lock()
		s.buf = append(s.buf, chunk[:n]...)
		if err != nil {
			s.done = true
		}
		s.cond.Broadcast()
		s.mu.Unlock()
		if err != nil {
			return
		}
	}
}

// text is the ansi-stripped output so far; the caller holds mu.
func (s *screen) text() string { return ansi.Strip(string(s.buf)) }

// snapshot returns the stripped output so far.
func (s *screen) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text()
}

// mark returns the current output length, so a later waitFor can match
// only what arrived after it.
func (s *screen) mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

// waitFor blocks until the stripped output after offset from (see mark)
// contains want, failing the test if ctx ends or the pty closes first.
func (s *screen) waitFor(ctx context.Context, t *testing.T, from int, want string) {
	t.Helper()
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cond.Broadcast()
	})
	defer stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for !strings.Contains(ansi.Strip(string(s.buf[from:])), want) {
		if ctx.Err() != nil || s.done {
			t.Fatalf("waiting for %q: %v (reader done: %v)\nscreen so far:\n%s", want, ctx.Err(), s.done, s.text())
		}
		s.cond.Wait()
	}
}

func TestE2E_TUISmoke(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".jig", "config.toml"), "[permissions]\nread = \"ask\"\n")
	script := writeScript(t, env, "tui.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "hello from jig"}},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	ctx, cancel := context.WithTimeout(context.Background(), tuiTimeout)
	defer cancel()
	cmd := command(ctx, env, "--cwd", env.work)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "JIG_IMAGES=off")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	scr := newScreen()
	go scr.read(tty)

	write := func(s string) {
		t.Helper()
		if _, err := tty.WriteString(s); err != nil {
			t.Fatalf("writing %q: %v", s, err)
		}
	}
	step := func(want, keys string) {
		t.Helper()
		scr.waitFor(ctx, t, 0, want)
		write(keys)
	}

	step("Trust this project", "t")
	// Before any run, the status bar shows the configured default model.
	scr.waitFor(ctx, t, 0, "build · m1")
	step("Message build", "hi\r")
	step("hello from jig", "\x10")
	step("Switch model", "model")
	write("\r")
	from := scr.mark()
	scr.waitFor(ctx, t, from, "m2")
	write("m2\r")
	// The status bar shows the model by its short ID (spec §7.5).
	scr.waitFor(ctx, t, from, "build · m2")
	write("\x04")

	err = cmd.Wait()
	if code := exitCode(t, err, scr.snapshot()); code != 0 {
		t.Fatalf("jig exited %d\nscreen:\n%s", code, scr.snapshot())
	}
}
