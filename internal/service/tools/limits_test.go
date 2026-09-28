package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/service/permission"
)

func TestRead_OutputCappedAt50KB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	line := strings.Repeat("x", 999) // "<n>: " + 999 bytes per line
	var b strings.Builder
	for range 1000 {
		b.WriteString(line + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := NewRead(OSFS(), NewTracker(), nil).Run(context.Background(), rcFor(dir), mustCall(t, "read", map[string]any{"path": path}))
	if err != nil || res.IsError {
		t.Fatalf("Run: %v %+v", err, res)
	}
	lines := strings.Split(res.Output, "\n")
	last := lines[len(lines)-1]
	shown := len(lines) - 1
	want := fmt.Sprintf("(output truncated at 50 KB; continue with offset %d)", shown+1)
	if last != want {
		t.Fatalf("last line = %q, want %q", last, want)
	}
	body := strings.Join(lines[:shown], "\n")
	if len(body) > 50*1024 {
		t.Errorf("body = %d bytes, want <= %d", len(body), 50*1024)
	}
	if shown < 40 || !strings.HasPrefix(lines[shown-1], fmt.Sprintf("%d: ", shown)) {
		t.Errorf("shown %d lines, last numbered line %q", shown, lines[shown-1])
	}
}

func TestReadGlobGrep_Subjects(t *testing.T) {
	rc := ext.RunContext{WorkDir: "/work"}
	subj := func(tool ext.Tool, input string) string {
		s, ok := tool.(ext.Subjecter)
		if !ok {
			t.Fatalf("%s does not implement ext.Subjecter", tool.Name())
		}
		return s.Subject(rc, json.RawMessage(input))
	}
	read := NewRead(OSFS(), NewTracker(), nil)
	if got := subj(read, `{"path":"sub/.env"}`); got != "/work/sub/.env" {
		t.Errorf("read subject = %q", got)
	}
	for _, tool := range []ext.Tool{NewGlob(&fakeSearcher{}), NewGrep(&fakeSearcher{})} {
		if got := subj(tool, `{"pattern":"x"}`); got != "/work" {
			t.Errorf("%s subject (no path) = %q, want /work", tool.Name(), got)
		}
		if got := subj(tool, `{"pattern":"x","path":"secrets"}`); got != "/work/secrets" {
			t.Errorf("%s subject = %q, want /work/secrets", tool.Name(), got)
		}
		if got := subj(tool, `{"pattern":"x","path":"/etc"}`); got != "/etc" {
			t.Errorf("%s subject (abs) = %q, want /etc", tool.Name(), got)
		}
	}
}

func TestRead_EnvDenyPatternBlocks(t *testing.T) {
	cfg := core.PermissionRules{"read": {Default: core.Allow, Patterns: map[string]core.Action{"*.env": core.Deny}}}
	hook := permission.NewHook(cfg, permission.StaticAsker{})
	read := NewRead(OSFS(), NewTracker(), nil)
	rc := ext.RunContext{SessionID: "s", WorkDir: t.TempDir()}

	_, v, err := hook.Before(context.Background(), rc, read, core.ToolCall{Name: "read", Input: json.RawMessage(`{"path":".env"}`)})
	if err != nil || !v.Block {
		t.Errorf("read .env: verdict %+v, err %v; want blocked", v, err)
	}
	_, v, _ = hook.Before(context.Background(), rc, read, core.ToolCall{Name: "read", Input: json.RawMessage(`{"path":"main.go"}`)})
	if v.Block {
		t.Errorf("read main.go blocked: %+v", v)
	}
}
