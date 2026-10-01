package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/importer"
)

func TestDefaultOpencodeDB(t *testing.T) {
	getenv := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}

	got := defaultOpencodeDB(getenv(map[string]string{"XDG_DATA_HOME": "/x"}))
	if want := "/x/opencode/opencode.db"; got != want {
		t.Errorf("defaultOpencodeDB(XDG_DATA_HOME=/x) = %q, want %q", got, want)
	}

	got = defaultOpencodeDB(getenv(map[string]string{"HOME": "/h"}))
	if want := "/h/.local/share/opencode/opencode.db"; got != want {
		t.Errorf("defaultOpencodeDB(HOME=/h) = %q, want %q", got, want)
	}
}

func TestImportCmd_Usage(t *testing.T) {
	for _, args := range [][]string{{}, {"claude"}} {
		var out, errw bytes.Buffer
		env := newTestEnv(t)
		code := importCmd(t.Context(), args, Stdio{Out: &out, Err: &errw}, env.getenv)
		if code != exitConfig {
			t.Errorf("args %v: exit = %d, want %d", args, code, exitConfig)
		}
		if !strings.Contains(errw.String(), "usage: jig import opencode") {
			t.Errorf("args %v: stderr = %q, want usage line", args, errw.String())
		}
	}
}

func TestPrintSummary(t *testing.T) {
	s := importer.Summary{
		Imported:       3,
		Skipped:        1,
		OrphansAsRoots: 1,
		Failed:         []importer.Failure{{ID: "ses_bad", Err: errors.New("boom")}},
		Stats: importer.Stats{
			SkippedMessages:   []string{"ses_x: msg_1: bad json"},
			DroppedTypes:      map[string]int{"idle": 2},
			UntranslatedTools: map[string]int{"webfetch": 4},
		},
	}

	var buf bytes.Buffer
	printSummary(&buf, s, false)
	out := buf.String()
	for _, want := range []string{
		"imported: 3",
		"already present: 1",
		"failed: 1",
		"ses_bad",
		"idle: 2",
		"webfetch: 4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("printSummary output missing %q:\n%s", want, out)
		}
	}
	if strings.HasPrefix(out, "dry run:") {
		t.Errorf("printSummary without dryRun wrote a dry-run line:\n%s", out)
	}

	var dryBuf bytes.Buffer
	printSummary(&dryBuf, s, true)
	lines := strings.Split(dryBuf.String(), "\n")
	if len(lines) == 0 || lines[0] != "dry run: nothing was written" {
		t.Errorf("printSummary(dryRun) first line = %q, want %q", lines[0], "dry run: nothing was written")
	}
}

func TestPrintSummary_Sanitizes(t *testing.T) {
	s := importer.Summary{
		Failed: []importer.Failure{{ID: core.SessionID("ses_evil"), Err: errors.New("boom\x1b[31mred\x1b[0m\ninjected")}},
		Stats: importer.Stats{
			UntranslatedTools: map[string]int{"evil\x1b[31mtool\n": 1},
			DroppedTypes:      map[string]int{},
		},
	}

	var buf bytes.Buffer
	printSummary(&buf, s, false)
	out := buf.String()
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("printSummary output contains ESC: %q", out)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.Contains(line, "ses_evil") && strings.Count(line, "\n") != 0 {
			t.Errorf("failure line split across multiple lines: %q", line)
		}
	}
}
