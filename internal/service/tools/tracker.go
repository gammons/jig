// Package tools implements jig's built-in file and shell tools: read,
// write, edit, bash, glob, grep, and todo. Each tool is an ext.Tool that
// registers through ext.Registry; this package holds no service-wide
// state beyond what a tool's own constructor captures.
package tools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// FS is the file-system capability read, write, and edit need. OSFS
// returns the real implementation; tests may substitute a fake.
type FS interface {
	ReadFile(string) ([]byte, error)
	WriteFile(string, []byte, fs.FileMode) error
	Stat(string) (fs.FileInfo, error)
	ReadDir(string) ([]fs.DirEntry, error)
	MkdirAll(string, fs.FileMode) error
}

// OSFS returns an FS backed by the real operating system file system.
func OSFS() FS { return osFS{} }

type osFS struct{}

func (osFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (osFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (osFS) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }

func (osFS) ReadDir(path string) ([]fs.DirEntry, error) { return os.ReadDir(path) }

func (osFS) MkdirAll(path string, perm fs.FileMode) error { return os.MkdirAll(path, perm) }

// readRecord is what Tracker remembers about a file read: the mtime and
// size observed at read time, so a later write or edit can detect a
// change made outside jig.
type readRecord struct {
	modTime time.Time
	size    int64
}

// Tracker remembers, per session, which files have been read and their
// mtime/size at read time, so write and edit can refuse to touch a file
// that was never read or has changed since.
type Tracker struct {
	mu    sync.Mutex
	reads map[core.SessionID]map[string]readRecord
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{reads: make(map[core.SessionID]map[string]readRecord)}
}

// MarkRead records that sid has read path, with info's mtime and size at
// the time of the read.
func (t *Tracker) MarkRead(sid core.SessionID, path string, info fs.FileInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.reads[sid] == nil {
		t.reads[sid] = make(map[string]readRecord)
	}
	t.reads[sid][path] = readRecord{modTime: info.ModTime(), size: info.Size()}
}

// CheckWritable reports whether sid may write or edit path. current is
// the file's current fs.FileInfo, or nil if the file does not yet exist
// (always writable). Otherwise it is an error if sid has never read path,
// or if path's mtime or size has changed since it was read.
func (t *Tracker) CheckWritable(sid core.SessionID, path string, current fs.FileInfo) error {
	if current == nil {
		return nil
	}

	t.mu.Lock()
	rec, ok := t.reads[sid][path]
	t.mu.Unlock()

	if !ok {
		return errNotRead(path)
	}
	if !rec.modTime.Equal(current.ModTime()) || rec.size != current.Size() {
		return errChanged(path)
	}
	return nil
}

// errNotRead reports that path must be read before it can be written.
func errNotRead(path string) error {
	return fmt.Errorf("%s has not been read in this session; read it first", path)
}

// errChanged reports that path changed since it was last read.
func errChanged(path string) error {
	return fmt.Errorf("%s was modified since it was last read; read it again", path)
}

// resolvePath resolves path against workDir when it is relative, and
// cleans the result.
func resolvePath(workDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(workDir, path))
}

// subjectPath extracts the "path" field of a tool's raw JSON input and
// resolves it exactly as Run does (via resolvePath, against rc.WorkDir),
// for use as an ext.Subjecter subject. It returns "" if input does not
// parse or has no path, so Subject and Run can never disagree about where
// a relative path points.
func subjectPath(rc ext.RunContext, input json.RawMessage) string {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
		return ""
	}
	return resolvePath(rc.WorkDir, in.Path)
}

// errResult builds an IsError ToolResult for call with msg as its output.
func errResult(call core.ToolCall, msg string) core.ToolResult {
	return core.ToolResult{CallID: call.ID, Name: call.Name, Output: msg, IsError: true}
}

// okResult builds a successful ToolResult for call with output as its
// output.
func okResult(call core.ToolCall, output string) core.ToolResult {
	return core.ToolResult{CallID: call.ID, Name: call.Name, Output: output}
}
