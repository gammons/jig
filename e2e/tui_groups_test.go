//go:build jigtest

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/creack/pty"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// TestE2E_TUIToolCallGroup drives the built binary under a pty: three
// reads across two steps fold into one group, and o expands it. The pty
// stream is redrawn cell by cell (a header changing in place from
// "exploring" to "explored · 3 reads" never arrives as one string), so
// the test waits only for text drawn fresh: the reply, the NORMAL badge,
// and the ▾ that expanding draws. The header's exact text is pinned by
// the App-level tests in internal/ui.
func TestE2E_TUIToolCallGroup(t *testing.T) {
	env := newEnv(t)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeFile(t, filepath.Join(env.work, name), "package x\n")
	}
	script := writeScript(t, env, "tui.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("r1", "read", `{"path":"a.go"}`), call("r2", "read", `{"path":"b.go"}`)}},
			{Calls: []jigtest.Call{call("r3", "read", `{"path":"c.go"}`)}},
			{Text: "read all three"},
		},
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

	// No project config, so there is no trust dialog; reads default to allow.
	scr.waitFor(ctx, t, 0, "Message build")
	write("go\r")
	scr.waitFor(ctx, t, 0, "read all three")
	from := scr.mark()
	write("\x1b") // esc: NORMAL, the selection on the reply
	scr.waitFor(ctx, t, from, "NORMAL")
	write("k") // up to the group header
	from = scr.mark()
	write("o")
	scr.waitFor(ctx, t, from, "▾")
	write("\x04")

	err = cmd.Wait()
	if code := exitCode(t, err, scr.snapshot()); code != 0 {
		t.Fatalf("jig exited %d\nscreen:\n%s", code, scr.snapshot())
	}
}
