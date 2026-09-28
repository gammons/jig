package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/pathid"
)

const screenshotCommand = "agent-browser screenshot"

// Screenshots attaches the image an `agent-browser screenshot` run saved
// to bash's result. The command and its output are model-controlled, so
// the named file is attached only if, after resolving symlinks, it is a
// regular file under the workdir, or a screenshot* file under the OS temp
// dir.
type Screenshots struct {
	img     Imager
	fs      FS
	tempDir string
	pathRE  *regexp.Regexp
	ansiRE  *regexp.Regexp
}

// NewScreenshots returns a Screenshots that decodes images with img,
// reads them through fs, and accepts screenshot* files under tempDir.
func NewScreenshots(img Imager, fs FS, tempDir string) *Screenshots {
	return &Screenshots{
		img:     img,
		fs:      fs,
		tempDir: tempDir,
		pathRE:  regexp.MustCompile(`(\S+\.(?i:png|jpe?g|webp))`),
		ansiRE:  regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`),
	}
}

// Attach returns the image named by an `agent-browser screenshot` run's
// output, as Media, or nil. The command must start (after TrimSpace) with
// "agent-browser screenshot". The path is the last image path in output,
// trimmed of quotes and trailing `.,;:)`; relative paths resolve against
// rc.WorkDir. Any failure is silent: the output already names the file.
func (s *Screenshots) Attach(rc ext.RunContext, command, output string) []core.Media {
	if !isScreenshotCommand(command) {
		return nil
	}
	matches := s.pathRE.FindAllString(s.ansiRE.ReplaceAllString(output, ""), -1)
	if len(matches) == 0 {
		return nil
	}
	p := strings.Trim(matches[len(matches)-1], "\"'`")
	p = strings.TrimRight(p, ".,;:)\"'`")
	if p == "" {
		return nil
	}

	resolved, ok := s.allowed(rc.WorkDir, resolvePath(rc.WorkDir, p))
	if !ok {
		return nil
	}
	info, err := s.fs.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxImageBytes {
		return nil
	}
	data, err := s.fs.ReadFile(resolved)
	if err != nil || len(data) > maxImageBytes {
		return nil
	}
	m, _, err := s.img.Process(data)
	if err != nil {
		return nil
	}
	return []core.Media{m}
}

// isScreenshotCommand reports whether command (after TrimSpace) is an
// "agent-browser screenshot" invocation: that prefix, then end of string
// or whitespace.
func isScreenshotCommand(command string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(command), screenshotCommand)
	if !ok {
		return false
	}
	return rest == "" || unicode.IsSpace([]rune(rest)[0])
}

// allowed resolves abs's symlinks (the file must exist) and returns the
// resolved path if it is strictly under the workdir, or strictly under
// tempDir with a base name starting "screenshot" (R13). Both checks use
// the resolved path, never abs.
func (s *Screenshots) allowed(workDir, abs string) (string, bool) {
	r, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.Abs(r)
	if err != nil {
		return "", false
	}
	if workDir != "" && within(pathid.Key(workDir), resolved) {
		return resolved, true
	}
	if s.tempDir != "" && within(pathid.Key(s.tempDir), resolved) &&
		strings.HasPrefix(filepath.Base(resolved), "screenshot") {
		return resolved, true
	}
	return "", false
}

// within reports whether p is strictly inside root. Both must be clean
// absolute paths.
func within(root, p string) bool {
	prefix := root
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	return len(p) > len(prefix) && strings.HasPrefix(p, prefix)
}
