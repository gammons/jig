package app

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/client/llm"
	"github.com/gammons/jig/internal/clock"
)

const (
	debugEnvVar    = "JIG_DEBUG"
	debugLogName   = "jig-debug.log"
	debugTimeLayer = "2006-01-02T15:04:05.000000Z07:00"
)

// openDebugLog opens workDir/jig-debug.log (truncating it) when JIG_DEBUG
// is set, and returns a logger writing to it. When unset it returns a
// discarding logger and a no-op closer; on an open error it does the same
// and also returns the error.
func openDebugLog(getenv func(string) string, workDir string) (*slog.Logger, io.Closer, error) {
	off := slog.New(slog.DiscardHandler)
	if getenv(debugEnvVar) == "" {
		return off, io.NopCloser(nil), nil
	}
	f, err := os.OpenFile(filepath.Join(workDir, debugLogName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return off, io.NopCloser(nil), err
	}
	return slog.New(slog.NewTextHandler(f, debugHandlerOptions())), f, nil
}

// debugHandlerOptions logs at debug level with a fixed-width UTC time and
// strings and errors made safe for a terminal and a single line.
func debugHandlerOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: replaceDebugAttr}
}

func replaceDebugAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		return slog.String(a.Key, a.Value.Time().UTC().Format(debugTimeLayer))
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, ansi.SanitizeLine(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, ansi.SanitizeLine(err.Error()))
		}
	default:
	}
	return a
}

// debugHTTPClient is the client whose transport logs every provider HTTP
// exchange to log, or nil when JIG_DEBUG is unset.
func debugHTTPClient(getenv func(string) string, log *slog.Logger, clk clock.Clock) *http.Client {
	if getenv(debugEnvVar) == "" {
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
