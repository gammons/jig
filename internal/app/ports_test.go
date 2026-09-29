package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/gammons/jig/internal/client/search"
	"github.com/gammons/jig/internal/core"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestProjectPort_ReadFileConfinement(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "w")
	sibling := filepath.Join(root, "w2")
	spillDir := filepath.Join(root, "spill")
	blobDir := filepath.Join(root, "blobs")
	elsewhere := filepath.Join(root, "elsewhere")

	mustWriteFile(t, filepath.Join(workDir, "a.txt"), "workdir file")
	mustWriteFile(t, filepath.Join(spillDir, "prompt-1.md"), "spill file")
	mustWriteFile(t, filepath.Join(blobDir, "ab", "abcd"), "blob bytes")
	mustWriteFile(t, filepath.Join(sibling, "a.txt"), "sibling file")
	outsideTarget := filepath.Join(elsewhere, "secret.txt")
	mustWriteFile(t, outsideTarget, "secret")
	if err := os.Symlink(outsideTarget, filepath.Join(workDir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(workDir, "big.bin")
	mustWriteFile(t, big, "x")
	if err := os.Truncate(big, 11*1024*1024); err != nil {
		t.Fatal(err)
	}

	p := projectPort{workDir: workDir, spillDir: spillDir, blobDir: blobDir}
	ctx := context.Background()

	if got, err := p.ReadFile(ctx, "a.txt"); err != nil || string(got) != "workdir file" {
		t.Errorf("ReadFile(workdir file) = %q, %v", got, err)
	}
	if got, err := p.ReadFile(ctx, filepath.Join(spillDir, "prompt-1.md")); err != nil || string(got) != "spill file" {
		t.Errorf("ReadFile(spill file) = %q, %v", got, err)
	}
	if got, err := p.ReadFile(ctx, filepath.Join(blobDir, "ab", "abcd")); err != nil || string(got) != "blob bytes" {
		t.Errorf("ReadFile(blob) = %q, %v", got, err)
	}

	if _, err := p.ReadFile(ctx, "../x"); err == nil {
		t.Error("ReadFile(../x): want error")
	}
	if _, err := p.ReadFile(ctx, filepath.Join(sibling, "a.txt")); err == nil {
		t.Error("ReadFile(absolute path elsewhere): want error")
	}
	if _, err := p.ReadFile(ctx, "link.txt"); err == nil {
		t.Error("ReadFile(symlink out of workdir): want error")
	}
	if _, err := p.ReadFile(ctx, filepath.Join(sibling, "a.txt")); err == nil {
		t.Error("ReadFile(sibling dir /w2 vs /w): want error")
	}
	if _, err := p.ReadFile(ctx, big); err == nil {
		t.Error("ReadFile(11 MiB file): want error")
	}
}

// readFileBounded runs p.ReadFile(ctx, path) on its own goroutine and
// fails the test if it does not return within the timeout, so a
// regression that blocks on a device or FIFO fails fast instead of
// hanging the suite.
func readFileBounded(t *testing.T, p projectPort, path string) ([]byte, error) {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := p.ReadFile(context.Background(), path)
		done <- result{data, err}
	}()
	select {
	case r := <-done:
		return r.data, r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("ReadFile(%q) did not return within 5s", path)
		return nil, nil
	}
}

func TestProjectPort_ReadFile_RejectsDeviceSymlink(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	workDir := t.TempDir()
	if err := os.Symlink("/dev/zero", filepath.Join(workDir, "zero")); err != nil {
		t.Fatal(err)
	}
	p := projectPort{workDir: workDir}

	if _, err := readFileBounded(t, p, "zero"); err == nil {
		t.Error("ReadFile(symlink to /dev/zero): want error")
	}
}

func TestProjectPort_ReadFile_RejectsFIFO(t *testing.T) {
	workDir := t.TempDir()
	fifoPath := filepath.Join(workDir, "pipe")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	p := projectPort{workDir: workDir}

	if _, err := readFileBounded(t, p, "pipe"); err == nil {
		t.Error("ReadFile(FIFO): want error")
	}
}

func TestProjectPort_ReadFile_RejectsDirectory(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := projectPort{workDir: workDir}
	if _, err := p.ReadFile(context.Background(), "adir"); err == nil {
		t.Error("ReadFile(directory): want error")
	}
}

func TestProjectPort_FilesMarksModified(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package main\n")
	mustWriteFile(t, filepath.Join(dir, "b.go"), "package main\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package main\n\n// changed\n")

	p := projectPort{workDir: dir, search: search.NewWalker()}
	got, err := p.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]bool, len(got))
	for _, f := range got {
		byPath[f.Path] = f.Modified
	}
	if !byPath["a.go"] {
		t.Errorf("Files: a.go Modified = %v, want true", byPath["a.go"])
	}
	if byPath["b.go"] {
		t.Errorf("Files: b.go Modified = %v, want false", byPath["b.go"])
	}
}

func TestProjectPort_Branch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "feat/status")

	got, err := projectPort{workDir: dir}.Branch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "feat/status" {
		t.Errorf("Branch = %q, want %q", got, "feat/status")
	}
}

func TestEditorPort_RoundTrip(t *testing.T) {
	spillDir := t.TempDir()
	scriptPath := filepath.Join(t.TempDir(), "editor.sh")
	script := "#!/bin/sh\necho '!' >> \"$1\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string {
		if k == "EDITOR" {
			return scriptPath
		}
		return ""
	}
	p := editorPort{spillDir: spillDir, getenv: getenv}

	cmd, result, err := p.Edit("hello")
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.SetStdout(&stderr)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v (output: %s)", err, stderr.String())
	}

	got, err := result()
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello!" {
		t.Errorf("result = %q, want %q", got, "hello!")
	}

	entries, err := os.ReadDir(spillDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("spillDir entries = %v, want none (temp file removed)", entries)
	}
}

func TestEditorPort_EditorCommand_SkipsWhitespaceOnlyVISUAL(t *testing.T) {
	getenv := func(k string) string {
		switch k {
		case "VISUAL":
			return "   "
		case "EDITOR":
			return "nano"
		}
		return ""
	}
	p := editorPort{getenv: getenv}
	if got := p.editorCommand(); got != "nano" {
		t.Errorf("editorCommand = %q, want %q (VISUAL is whitespace-only)", got, "nano")
	}
}

func TestEditorPort_EditorCommand_FallsBackToVi(t *testing.T) {
	getenv := func(string) string { return "  " }
	p := editorPort{getenv: getenv}
	if got := p.editorCommand(); got != "vi" {
		t.Errorf("editorCommand = %q, want %q (both whitespace-only)", got, "vi")
	}
}

type fakeCatalogProviders struct{ infos []core.ProviderInfo }

func (f fakeCatalogProviders) Providers() []core.ProviderInfo { return f.infos }

type fakeCredentialChecker struct{ configured map[string]bool }

func (f fakeCredentialChecker) HasCredentials(id string) bool { return f.configured[id] }

func TestCatalogPort_Configured(t *testing.T) {
	cat := fakeCatalogProviders{infos: []core.ProviderInfo{{ID: "anthropic"}, {ID: "openai"}}}
	src := fakeCredentialChecker{configured: map[string]bool{"anthropic": true}}
	p := catalogPort{cat: cat, src: src}

	got := p.Providers()
	want := []core.ProviderStatus{
		{Info: core.ProviderInfo{ID: "anthropic"}, Configured: true},
		{Info: core.ProviderInfo{ID: "openai"}, Configured: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Providers = %+v, want %+v", got, want)
	}
}
