package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/skillfs"
	"github.com/gammons/jig/internal/data/store"
)

const osc52 = "\x1b]52;c;aGk=\x07"

func TestPrintWarnings_Sanitized(t *testing.T) {
	var buf bytes.Buffer
	printWarnings(&buf, []skillfs.Warning{{Path: "/p/" + osc52 + "x.md", Msg: "bad\x1b[2J\nline"}})
	if got := buf.String(); strings.ContainsRune(got, '\x1b') || strings.Count(got, "\n") != 1 {
		t.Errorf("printWarnings wrote %q, want one line with no ESC", got)
	}
}

func TestSessions_TitleSanitized(t *testing.T) {
	env := newTestEnv(t)
	st, err := store.Open(t.Context(), filepath.Join(env.vars["XDG_DATA_HOME"], "jig", "jig.db"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	sess := core.Session{ID: "ses_a", Title: "evil" + osc52 + "\ntitle", Agent: "build", CreatedAt: at, UpdatedAt: at}
	if err := st.CreateSession(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	st.Close()

	code, out, stderr := env.run(t, "sessions")
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if strings.ContainsRune(out, '\x1b') || strings.Count(out, "\n") != 1 {
		t.Errorf("stdout = %q, want one line with no ESC", out)
	}
}

func TestDiscover_BrowserWarningsSanitized(t *testing.T) {
	e := mustLoadEnv(t, newTestEnv(t), (&countingDecider{}).decide)
	e.browserWarns = []string{"warning: agent-browser skills path: " + osc52 + "\nboom"}
	var buf bytes.Buffer
	discover(e, &buf)
	if got := buf.String(); strings.ContainsRune(got, '\x1b') || strings.Count(got, "\n") != 1 {
		t.Errorf("discover wrote %q, want one line with no ESC", got)
	}
}
