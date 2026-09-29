package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

// mcpfakeBin is the path to the mcpfake binary built once in TestMain.
var mcpfakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpfake")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	mcpfakeBin = filepath.Join(dir, "mcpfake")
	cmd := exec.Command("go", "build", "-o", mcpfakeBin, "../../../e2e/testdata/mcpfake")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("building mcpfake: " + err.Error())
	}

	os.Exit(m.Run())
}

// fakeScript is the JSON shape mcpfake reads from $MCPFAKE_SCRIPT.
type fakeScript struct {
	Tools          []fakeTool `json:"tools"`
	CrashOnCall    bool       `json:"crash_on_call"`
	HangInitialize bool       `json:"hang_initialize"`
	Stderr         string     `json:"stderr"`
}

type fakeTool struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ReadOnly    bool      `json:"read_only"`
	Reply       fakeReply `json:"reply"`
}

type fakeReply struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

// writeScript writes s as JSON to a temp file and returns its path.
func writeScript(t *testing.T, s fakeScript) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshaling script: %v", err)
	}
	path := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	return path
}

// dialFake dials mcpfake, configured by scriptPath, with s.Command/Args/Env
// filled in.
func dialFake(ctx context.Context, scriptPath string) (*Conn, error) {
	return Dial(ctx, Spec{
		Name:      "fake",
		Transport: core.MCPStdio,
		Command:   mcpfakeBin,
		Env:       append(os.Environ(), "MCPFAKE_SCRIPT="+scriptPath),
	}, nil)
}

// pgidEmpty reports whether pgid has no living members, on linux only
// (elsewhere it always reports true so callers skip the assertion).
func pgidEmpty(pgid int) bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

// waitForPgidEmpty polls pgidEmpty with a bounded number of ticks (no
// time.Sleep), failing the test if the group is still non-empty after.
func waitForPgidEmpty(t *testing.T, pgid int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if pgidEmpty(pgid) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("process group %d still has members after 2s", pgid)
		case <-tick.C:
		}
	}
}

func TestStdio_FakeServer(t *testing.T) {
	scriptPath := writeScript(t, fakeScript{
		Tools: []fakeTool{
			{Name: "greet", Description: "greets", Reply: fakeReply{Text: "hi there"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := dialFake(ctx, scriptPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	tools, err := conn.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "greet" {
		t.Fatalf("tools = %+v, want one tool named greet", tools)
	}

	res, err := conn.CallTool(ctx, "greet", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "hi there" {
		t.Fatalf("CallTool result = %+v, want text %q", res, "hi there")
	}

	pid := conn.proc.cmd.Process.Pid
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitForPgidEmpty(t, pid)
}

func TestStdio_CrashClosesDone(t *testing.T) {
	scriptPath := writeScript(t, fakeScript{
		CrashOnCall: true,
		Stderr:      "about to crash",
		Tools: []fakeTool{
			{Name: "boom", Reply: fakeReply{Text: "never seen"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := dialFake(ctx, scriptPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// CallTool may return an error (transport dropped mid-call) or a
	// successful-looking result if the process exits before the write is
	// observed; either way the process crashes and Done must close.
	_, _ = conn.CallTool(ctx, "boom", nil)

	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close within 5s of the server crashing")
	}
	err = conn.Err()
	if err == nil {
		t.Fatal("Err() = nil, want a non-nil end error after a crash")
	}
	if !strings.Contains(err.Error(), "about to crash") {
		t.Errorf("Err() = %v, want it to contain the stderr tail %q", err, "about to crash")
	}
}

func TestStdio_StartupTimeout(t *testing.T) {
	scriptPath := writeScript(t, fakeScript{HangInitialize: true, Stderr: "hanging"})

	var pid int
	onStart := func(p int) { pid = p }

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	type dialOutcome struct {
		conn *Conn
		err  error
	}
	done := make(chan dialOutcome, 1)
	go func() {
		conn, err := dialStdio(ctx, Spec{
			Name:      "fake",
			Transport: core.MCPStdio,
			Command:   mcpfakeBin,
			Env:       append(os.Environ(), "MCPFAKE_SCRIPT="+scriptPath),
		}, nil, onStart)
		done <- dialOutcome{conn, err}
	}()

	var outcome dialOutcome
	select {
	case outcome = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Dial did not return within 2s of its 200ms deadline")
	}
	if outcome.err == nil {
		outcome.conn.Close()
		t.Fatal("Dial succeeded, want context.DeadlineExceeded")
	}
	if !errors.Is(outcome.err, context.DeadlineExceeded) {
		t.Errorf("Dial err = %v, want context.DeadlineExceeded", outcome.err)
	}
	if pid == 0 {
		t.Fatal("onStart was never called; can't check the process group")
	}
	waitForPgidEmpty(t, pid)
}
