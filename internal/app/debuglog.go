package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/client/llm"
	"github.com/gammons/jig/internal/clock"
)

const (
	debugEnvVar     = "JIG_DEBUG"
	debugLogName    = "jig-debug.log"
	debugTimeLayout = "2006-01-02T15:04:05.000000Z07:00"
	debugMaxValue   = 4096
	debugTruncated  = "…[truncated]"
)

// nopCloser is the closer of a debug log that opened no file.
type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// openDebugLog opens workDir/jig-debug.log (truncating it) when JIG_DEBUG
// is set, and returns a logger writing to it. When unset it returns a
// discarding logger and a no-op closer; on an open error it does the same
// and also returns the error. An existing jig-debug.log that is not a
// regular file (a symlink, say) is refused, never followed or truncated.
func openDebugLog(getenv func(string) string, workDir string) (*slog.Logger, io.Closer, error) {
	off := slog.New(slog.DiscardHandler)
	if getenv(debugEnvVar) == "" {
		return off, nopCloser{}, nil
	}
	path := filepath.Join(workDir, debugLogName)
	info, err := os.Lstat(path)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return off, nopCloser{}, errors.New(debugLogName + " is not a regular file")
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return off, nopCloser{}, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|openNoFollow, 0o644)
	if err != nil {
		return off, nopCloser{}, err
	}
	return slog.New(slog.NewTextHandler(f, debugHandlerOptions())), f, nil
}

// debugHandlerOptions logs at debug level with a fixed-width UTC time and
// strings and errors made safe for a terminal and a single line.
func debugHandlerOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: replaceDebugAttr}
}

// replaceDebugAttr formats the time, and makes every string and error value
// one sanitized line with URL userinfo redacted and at most debugMaxValue
// bytes.
func replaceDebugAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		return slog.String(a.Key, a.Value.Time().UTC().Format(debugTimeLayout))
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, safeDebugValue(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, safeDebugValue(err.Error()))
		}
	default:
	}
	return a
}

func safeDebugValue(s string) string {
	return capDebugValue(redactUserinfo(ansi.SanitizeLine(s)))
}

// capDebugValue cuts s to at most debugMaxValue bytes at a rune boundary,
// marking a cut with debugTruncated.
func capDebugValue(s string) string {
	if len(s) <= debugMaxValue {
		return s
	}
	n := debugMaxValue
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + debugTruncated
}

// redactUserinfo replaces the userinfo of every scheme://user[:pass]@host
// in s with "…", so credentials in a URL never reach the log.
func redactUserinfo(s string) string {
	const sep = "://"
	if !strings.Contains(s, sep) {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, sep)
		if i < 0 {
			break
		}
		b.WriteString(s[:i+len(sep)])
		s = s[i+len(sep):]
		authority := s
		if end := strings.IndexAny(s, "/ \t\n\""); end >= 0 {
			authority = s[:end]
		}
		if at := strings.LastIndex(authority, "@"); at > 0 {
			b.WriteString("…@")
			s = s[at+1:]
		}
	}
	b.WriteString(s)
	return b.String()
}

// debugHTTPClient is the client whose transport logs every provider HTTP
// exchange to log, or nil when log is disabled (JIG_DEBUG unset, or the
// log could not be opened).
func debugHTTPClient(log *slog.Logger, clk clock.Clock) *http.Client {
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		return nil
	}
	return &http.Client{Transport: llm.NewLoggingTransport(http.DefaultTransport, log, clk)}
}

// startDebugLog opens the debug log for e, warning on errw (and carrying
// on without a log) when it cannot, then logs its first line.
func startDebugLog(e env, errw io.Writer) (*slog.Logger, io.Closer) {
	log, closer, err := openDebugLog(e.getenv, e.workDir)
	if err != nil {
		printLine(errw, "warning: "+debugEnvVar+": "+err.Error())
	}
	log.Debug("debug log start", "cat", "app", "version", version(),
		"workdir", e.workDir, "pid", os.Getpid(), "default_model", e.cfg().DefaultModel)
	return log, closer
}
