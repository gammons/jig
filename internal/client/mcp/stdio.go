package mcp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gammons/jig/internal/client/shell"
)

// stderrTailBytes bounds how much of a stdio server's stderr is kept in
// memory for Conn.Err.
const stderrTailBytes = 4096

// stdioProc holds the pieces of a stdio connection that outlive the SDK
// session: the running command (so Close can kill its whole process
// group) and its stderr tail.
type stdioProc struct {
	cmd *exec.Cmd

	mu   sync.Mutex
	tail []byte
}

// stderrTail returns the trailing stderrTailBytes of the child's stderr,
// trimmed of surrounding whitespace.
func (p *stdioProc) stderrTail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return trimTail(p.tail)
}

// Write implements io.Writer, keeping only the trailing stderrTailBytes.
func (p *stdioProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tail = appendTail(p.tail, b, stderrTailBytes)
	return len(b), nil
}

// close kills the child's whole process group, so a stdio server that
// spawned grandchildren (e.g. npx -> node) doesn't leave them behind.
func (p *stdioProc) close() {
	if p.cmd.Process == nil {
		return
	}
	_ = shell.KillGroup(p.cmd.Process.Pid)
}

// appendTail appends b to tail, keeping only the last max bytes.
func appendTail(tail, b []byte, max int) []byte {
	tail = append(tail, b...)
	if len(tail) > max {
		tail = tail[len(tail)-max:]
	}
	return tail
}

// dialStdio starts s.Command as a child process in its own session and
// process group (so Close and a failed/timed-out Dial can kill it and
// every descendant), and connects the SDK client to it over stdio.
func dialStdio(ctx context.Context, s Spec, onToolsChanged func()) (*Conn, error) {
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	shell.ConfigureGroup(cmd)

	proc := &stdioProc{cmd: cmd}
	cmd.Stderr = proc

	client := newClient(s, onToolsChanged)
	transport := &sdkmcp.CommandTransport{Command: cmd}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		proc.close()
		return nil, err
	}
	return newConn(session, proc), nil
}

// trimTail trims tail's surrounding whitespace, returning "" for an empty
// or all-whitespace tail.
func trimTail(tail []byte) string {
	return strings.TrimSpace(string(tail))
}
