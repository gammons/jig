package app

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func debugGetenv(v string) func(string) string {
	return func(k string) string {
		if k == "JIG_DEBUG" {
			return v
		}
		return ""
	}
}

func TestDebugLog_OffCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	log, closer, err := openDebugLog(debugGetenv(""), dir)
	if err != nil {
		t.Fatalf("openDebugLog: %v", err)
	}
	if log.Enabled(t.Context(), slog.LevelDebug) {
		t.Error("logger enabled with JIG_DEBUG unset")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jig-debug.log")); !os.IsNotExist(err) {
		t.Errorf("jig-debug.log: stat err = %v, want not-exist", err)
	}
}

func TestDebugLog_OnWritesAndTruncates(t *testing.T) {
	dir := t.TempDir()
	for _, msg := range []string{"first", "second"} {
		log, closer, err := openDebugLog(debugGetenv("1"), dir)
		if err != nil {
			t.Fatalf("openDebugLog: %v", err)
		}
		log.Debug(msg)
		if err := closer.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	path := filepath.Join(dir, "jig-debug.log")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "msg=second") || strings.Contains(string(got), "msg=first") {
		t.Errorf("log = %q, want only the second run", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestDebugLog_UnwritableDirReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	log, closer, err := openDebugLog(debugGetenv("1"), dir)
	if err == nil {
		t.Fatal("openDebugLog: want an error")
	}
	if log == nil || log.Enabled(t.Context(), slog.LevelDebug) {
		t.Error("want a non-nil, disabled logger on error")
	}
	if closer == nil {
		t.Fatal("want a non-nil closer on error")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestDebugLog_SanitizesStrings(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, debugHandlerOptions()))
	log.Debug("x", "body", "a\x1b[31mb\nc", "err", errors.New("e\x1b]0;t\x07"))
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Errorf("output contains ESC: %q", out)
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Errorf("output has %d newlines, want 1: %q", n, out)
	}
}

func TestLoadEnv_DoesNotOpenDebugLog(t *testing.T) {
	env := newTestEnv(t)
	env.vars["JIG_DEBUG"] = "1"
	path := filepath.Join(env.workDir, "jig-debug.log")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEnv(env.workDir, env.getenv, staticTrust(false)); err != nil {
		t.Fatalf("loadEnv: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "keep" {
		t.Errorf("jig-debug.log = %q, %v; want %q", got, err, "keep")
	}
}

func TestNewRuntime_DebugLogStart(t *testing.T) {
	env := newTestEnv(t)
	env.vars["JIG_DEBUG"] = "1"
	t.Setenv("TMPDIR", t.TempDir())
	e, err := loadEnv(env.workDir, env.getenv, staticTrust(false))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := newRuntime(t.Context(), e, staticAsker(false), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(env.workDir, "jig-debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`msg="debug log start"`, "cat=app", "pid=", "default_model="} {
		if !strings.Contains(string(got), want) {
			t.Errorf("log = %q, want it to contain %q", got, want)
		}
	}
}
