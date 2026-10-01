//go:build jigtest

package e2e

import (
	"context"
	"fmt"
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
	s.waitForFunc(ctx, t, want, func() string {
		return ansi.Strip(string(s.buf[from:]))
	})
}

// waitForRaw blocks until the raw (unstripped) output after offset from
// (see mark) contains want, failing the test if ctx ends or the pty
// closes first. It is for content ansi.Strip would remove, such as an
// OSC 52 clipboard sequence.
func (s *screen) waitForRaw(ctx context.Context, t *testing.T, from int, want string) {
	t.Helper()
	s.waitForFunc(ctx, t, want, func() string {
		return string(s.buf[from:])
	})
}

// waitForFunc blocks on s.cond until got() contains want, failing the
// test if ctx ends or the pty closes first. The caller holds no lock;
// got is called with s.mu held.
func (s *screen) waitForFunc(ctx context.Context, t *testing.T, want string, got func() string) {
	t.Helper()
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cond.Broadcast()
	})
	defer stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for !strings.Contains(got(), want) {
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

// TestE2E_TUISubagentView drives a real task spawn through the TUI: the
// subagent block is selected and pushed into the column, its breadcrumb
// and live child text appear, then popping it closes the column again.
func TestE2E_TUISubagentView(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".jig", "config.toml"), "[permissions]\nread = \"ask\"\n")
	input := `{"agent":"explore","description":"look around","prompt":"find"}`
	script := writeScript(t, env, "tui.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("t1", "task", input)}},
			{Text: "parent done"},
		},
		"m2": {{Text: "**child** found it"}},
	}})
	writeConfig(t, env, jigtestConfig(script, "\n[agents.explore]\nmodel = \"jigtest/m2\"\n"))

	ctx, cancel := context.WithTimeout(context.Background(), tuiTimeout)
	defer cancel()
	cmd := command(ctx, env, "--cwd", env.work)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "JIG_IMAGES=off")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 160})
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
	step("Message build", "delegate\r")
	step("parent done", "\x1b")
	scr.waitFor(ctx, t, 0, "NORMAL") // wait for esc to land in NORMAL before navigating
	// gg selects the first block (the user prompt); j moves onto the
	// subagent block, which comes right after it.
	write("ggj")
	from := scr.mark()
	write("\r")
	scr.waitFor(ctx, t, from, "main › ↳ explore (m2): look around")
	scr.waitFor(ctx, t, from, "child found it")
	from = scr.mark()
	write("\x1b")
	// The column closed: at 160 columns (≥ the 120-column sidebar
	// threshold) the column only ever took the right half, so main's own
	// region (left half, already showing "parent done") never needed to
	// retransmit — the terminal only resends changed cells. The sidebar
	// reclaiming that half instead (its "Subagents" panel) is the
	// observable proof the column closed.
	scr.waitFor(ctx, t, from, "Subagents")
	write("\x04")

	err = cmd.Wait()
	if code := exitCode(t, err, scr.snapshot()); code != 0 {
		t.Fatalf("jig exited %d\nscreen:\n%s", code, scr.snapshot())
	}
}

// wheel-up scrolls the transcript back into text that scrolled off the
// bottom, and a press-drag-release over the reply selects text and
// copies it to the clipboard via an OSC 52 sequence (spec §3.1/§3.2).
func TestE2E_TUIMouseScrollAndCopy(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, ".jig", "config.toml"), "[permissions]\nread = \"ask\"\n")
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	// A fenced code block keeps each line on its own rendered row
	// (plain text would be word-wrapped by glamour, packing several
	// "line NN" entries per row and never needing to scroll).
	reply := "```\n" + strings.Join(lines, "\n") + "\n```"
	script := writeScript(t, env, "tui.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: reply}},
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
	scr.waitFor(ctx, t, 0, "build · m1")
	step("Message build", "hi\r")
	// The reply is 60 lines; once it finishes the view follows the
	// bottom, so the early lines are scrolled off screen.
	scr.waitFor(ctx, t, 0, "line 60")

	// Wheel-up over the transcript, enough notches (of wheelLines=3 each)
	// to scroll all 60 lines back into view, at pane-local (10,5). The
	// renderer redraws only the cells that change, and "line " sits at
	// the same cells on every row, so after the wheel only the numbers
	// are rewritten: wait for the reply's first number, "01", which only
	// a scroll back to the top puts on screen.
	from := scr.mark()
	for range 20 {
		write("\x1b[<64;10;5M")
	}
	scr.waitFor(ctx, t, from, "01 ")

	// Press, drag, and release across the reply's first row ("line 01",
	// screen row 6 below the filled user panel), to select it and copy
	// it via OSC 52 on release.
	from = scr.mark()
	write("\x1b[<0;3;6M")   // press
	write("\x1b[<32;10;6M") // motion
	write("\x1b[<0;10;6m")  // release
	scr.waitForRaw(ctx, t, from, "\x1b]52;c;")
	scr.waitFor(ctx, t, from, "copied ")

	write("\x04")

	err = cmd.Wait()
	if code := exitCode(t, err, scr.snapshot()); code != 0 {
		t.Fatalf("jig exited %d\nscreen:\n%s", code, scr.snapshot())
	}
}
