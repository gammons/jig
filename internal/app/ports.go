package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/client/search"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/blobfs"
	"github.com/gammons/jig/internal/pathid"
	"github.com/gammons/jig/internal/service/agents"
)

var (
	_ core.CatalogService = catalogPort{}
	_ core.AgentService   = (*agents.Service)(nil)
	_ core.ProjectService = projectPort{}
	_ core.BlobService    = blobPort{}
	_ core.EditorService  = editorPort{}
	_ core.ExecCommand    = execCmd{}
)

// catalogProviders lists catalog providers, e.g. *catalog.Catalog.
type catalogProviders interface {
	Providers() []core.ProviderInfo
}

// credentialChecker reports whether a provider's credentials are
// resolvable right now, e.g. *llm.Source.
type credentialChecker interface {
	HasCredentials(providerID string) bool
}

// catalogPort implements core.CatalogService over a catalog and an
// llm.Source's credential check.
type catalogPort struct {
	cat catalogProviders
	src credentialChecker
}

// Providers returns every catalog provider, sorted by ID (as cat.Providers
// already is), each paired with whether it has usable credentials now.
func (p catalogPort) Providers() []core.ProviderStatus {
	infos := p.cat.Providers()
	out := make([]core.ProviderStatus, len(infos))
	for i, info := range infos {
		out[i] = core.ProviderStatus{Info: info, Configured: p.src.HasCredentials(info.ID)}
	}
	return out
}

// maxProjectFiles caps ProjectService.Files (R19).
const maxProjectFiles = 20000

// maxProjectFileSize caps ProjectService.ReadFile.
const maxProjectFileSize = 10 * 1024 * 1024

// projectPort implements core.ProjectService, confined to the workdir,
// the runtime's private spill dir, and the blob store's directory.
type projectPort struct {
	workDir  string
	spillDir string
	blobDir  string
	search   search.Searcher
}

// Files returns every eligible file under p.workDir (gitignore-aware, at
// most maxProjectFiles), marking the ones `git status` sees as changed.
func (p projectPort) Files(ctx context.Context) ([]core.ProjectFile, error) {
	paths, err := p.search.Glob(ctx, p.workDir, "**/*", maxProjectFiles)
	if err != nil {
		return nil, err
	}
	modified, err := search.GitModified(ctx, p.workDir)
	if err != nil {
		return nil, err
	}
	modSet := make(map[string]bool, len(modified))
	for _, m := range modified {
		modSet[m] = true
	}
	out := make([]core.ProjectFile, len(paths))
	for i, path := range paths {
		out[i] = core.ProjectFile{Path: path, Modified: modSet[path]}
	}
	return out, nil
}

// ReadFile resolves path against p.workDir (if relative), confines it to
// the workdir, spill dir, or blob dir, rejects anything but a regular
// file (a symlink or FIFO could otherwise block the caller forever), and
// returns its bytes if it is at most maxProjectFileSize.
func (p projectPort) ReadFile(_ context.Context, path string) ([]byte, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(p.workDir, abs)
	}
	if err := p.confine(abs, path); err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path %s is not a regular file", path)
	}
	if info.Size() > maxProjectFileSize {
		return nil, fmt.Errorf("path %s is too large (%d bytes, max %d)", path, info.Size(), maxProjectFileSize)
	}
	return readBounded(abs, path)
}

// readBounded opens abs and reads at most maxProjectFileSize+1 bytes, so
// a file that grows past the cap after the Stat check in ReadFile still
// can't be read past it.
func readBounded(abs, path string) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProjectFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProjectFileSize {
		return nil, fmt.Errorf("path %s is too large (max %d bytes)", path, maxProjectFileSize)
	}
	return data, nil
}

// confine returns an error unless abs's canonical form (symlinks
// resolved) equals, or is nested under, p.workDir, p.spillDir, or
// p.blobDir. orig is the path as given, used in the error message.
func (p projectPort) confine(abs, orig string) error {
	k := pathid.Key(abs)
	for _, root := range []string{p.workDir, p.spillDir, p.blobDir} {
		if root == "" {
			continue
		}
		rk := pathid.Key(root)
		if k == rk || strings.HasPrefix(k, rk+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("path %s is outside the project", orig)
}

// blobPort implements core.BlobService over a blobfs.Store.
type blobPort struct{ blobs *blobfs.Store }

// Open returns ref's bytes and their detected MIME type.
func (p blobPort) Open(ref string) ([]byte, string, error) {
	data, err := p.blobs.Open(ref)
	if err != nil {
		return nil, "", err
	}
	return data, http.DetectContentType(data), nil
}

// editorPort implements core.EditorService: $VISUAL, else $EDITOR, else
// vi, editing a temp file in the runtime's private spill dir.
type editorPort struct {
	spillDir string
	getenv   func(string) string
}

// Edit writes text to a temp file in p.spillDir and returns the editor
// command to run over it, plus a result func that reads it back.
func (p editorPort) Edit(text string) (core.ExecCommand, func() (string, error), error) {
	f, err := os.CreateTemp(p.spillDir, "prompt-*.md")
	if err != nil {
		return nil, nil, err
	}
	path := f.Name()
	if err := writeAndClose(f, text); err != nil {
		os.Remove(path)
		return nil, nil, err
	}

	fields := strings.Fields(p.editorCommand())
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	return execCmd{cmd}, editResult(path), nil
}

// editorCommand returns $VISUAL, else $EDITOR, else "vi", skipping a
// value that is empty or all whitespace.
func (p editorPort) editorCommand() string {
	if v := strings.TrimSpace(p.getenv("VISUAL")); v != "" {
		return v
	}
	if e := strings.TrimSpace(p.getenv("EDITOR")); e != "" {
		return e
	}
	return "vi"
}

// writeAndClose writes text to f and closes it, regardless of the write's
// outcome.
func writeAndClose(f *os.File, text string) error {
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// editResult returns a func that reads path back, trims one trailing
// newline, and removes the file.
func editResult(path string) func() (string, error) {
	return func() (string, error) {
		defer os.Remove(path)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	}
}

// execCmd adapts *exec.Cmd to core.ExecCommand.
type execCmd struct{ cmd *exec.Cmd }

func (e execCmd) Run() error            { return e.cmd.Run() }
func (e execCmd) SetStdin(r io.Reader)  { e.cmd.Stdin = r }
func (e execCmd) SetStdout(w io.Writer) { e.cmd.Stdout = w }
func (e execCmd) SetStderr(w io.Writer) { e.cmd.Stderr = w }
