//go:build jigtest

package e2e

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
	"github.com/gammons/jig/internal/data/opencode/opencodetest"
)

// jsonStr marshals v to a JSON string, panicking on error (v is always a
// hand-built literal in these tests).
func jsonStr(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// writeOpencodeFixture writes a fixture opencode DB in env.root holding
// ses_root (a user message, an assistant step with an edit call and a task
// call naming ses_child, then a final text step), its child ses_child, and
// an empty root ses_empty. All three sessions live in env.work. It returns
// the DB's path.
func writeOpencodeFixture(t *testing.T, env *testEnv) string {
	t.Helper()
	const model = `{"providerID":"jigtest","id":"m1"}`

	assistantStep1 := jsonStr(t, map[string]any{
		"time":   map[string]any{"created": 1000, "completed": 1001},
		"agent":  "build",
		"model":  map[string]any{"id": "m1", "providerID": "jigtest"},
		"finish": "stop",
		"cost":   0,
		"tokens": map[string]any{"input": 1, "output": 1, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
		"content": []any{
			map[string]any{
				"type": "tool", "id": "toolu_1", "name": "edit",
				"state": map[string]any{
					"status":  "completed",
					"input":   map[string]any{"filePath": "foo.txt", "oldString": "a", "newString": "b"},
					"content": []any{map[string]any{"type": "text", "text": "ok"}},
				},
			},
			map[string]any{
				"type": "tool", "id": "toolu_2", "name": "task",
				"state": map[string]any{
					"status":   "completed",
					"input":    map[string]any{"subagent_type": "general", "task_id": "ses_child"},
					"content":  []any{map[string]any{"type": "text", "text": "child result"}},
					"metadata": map[string]any{"sessionId": "ses_child"},
				},
			},
		},
	})
	assistantStep2 := jsonStr(t, map[string]any{
		"time":    map[string]any{"created": 1002, "completed": 1003},
		"agent":   "build",
		"model":   map[string]any{"id": "m1", "providerID": "jigtest"},
		"finish":  "stop",
		"cost":    0,
		"tokens":  map[string]any{"input": 1, "output": 1, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
		"content": []any{map[string]any{"type": "text", "text": "all done"}},
	})
	childAssistant := jsonStr(t, map[string]any{
		"time":    map[string]any{"created": 1500, "completed": 1501},
		"agent":   "general",
		"model":   map[string]any{"id": "m1", "providerID": "jigtest"},
		"finish":  "stop",
		"cost":    0,
		"tokens":  map[string]any{"input": 1, "output": 1, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
		"content": []any{map[string]any{"type": "text", "text": "child done"}},
	})

	sessions := []opencodetest.Session{
		{
			ID: "ses_root", Directory: env.work, Title: "root session", Model: model,
			Created: 1000, Updated: 1003,
			Messages: []opencodetest.Message{
				{ID: "msg_user", Type: "user", Seq: 0, Created: 999, Data: jsonStr(t, map[string]any{"text": "describe the task"})},
				{ID: "msg_a1", Type: "assistant", Seq: 1, Created: 1001, Data: assistantStep1},
				{ID: "msg_a2", Type: "assistant", Seq: 2, Created: 1003, Data: assistantStep2},
			},
		},
		{
			ID: "ses_child", ParentID: "ses_root", Directory: env.work, Title: "child session", Model: model,
			Created: 1200, Updated: 1501,
			Messages: []opencodetest.Message{
				{ID: "msg_c1", Type: "assistant", Seq: 0, Created: 1501, Data: childAssistant},
			},
		},
		{
			ID: "ses_empty", Directory: env.work, Title: "empty session", Model: model,
			Created: 1600, Updated: 1600,
		},
	}

	path := filepath.Join(env.root, "opencode.db")
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("opencodetest.Write: %v", err)
	}
	return path
}

func TestE2E_ImportOpencode(t *testing.T) {
	env := newEnv(t)
	dbPath := writeOpencodeFixture(t, env)

	stdout, stderr, code := runJig(t, env, "import", "opencode", "--db", dbPath)
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "imported: 3") {
		t.Errorf("stdout = %q, want it to contain %q", stdout, "imported: 3")
	}

	ids := sessionIDs(t, env)
	hasRoot, hasEmpty, hasChild := false, false, false
	for _, id := range ids {
		switch id {
		case "ses_root":
			hasRoot = true
		case "ses_empty":
			hasEmpty = true
		case "ses_child":
			hasChild = true
		}
	}
	if !hasRoot || !hasEmpty {
		t.Errorf("sessionIDs = %v, want ses_root and ses_empty", ids)
	}
	if hasChild {
		t.Errorf("sessionIDs = %v, want no ses_child (it is a subagent session)", ids)
	}

	stdout, stderr, code = runJig(t, env, "import", "opencode", "--db", dbPath)
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "already present: 3") {
		t.Errorf("second import stdout = %q, want it to contain %q", stdout, "already present: 3")
	}

	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "continuing root"}},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))
	stdout, stderr, code = runPrompt(t, env, "--session", "ses_root", "continue")
	wantCode(t, code, 0, stdout, stderr)

	script2 := writeScript(t, env, "script2.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {{Text: "continuing empty"}},
	}})
	writeConfig(t, env, jigtestConfig(script2, ""))
	stdout, stderr, code = runPrompt(t, env, "--session", "ses_empty", "continue")
	wantCode(t, code, 0, stdout, stderr)
}

func TestE2E_ImportOpencode_DryRun(t *testing.T) {
	env := newEnv(t)
	dbPath := writeOpencodeFixture(t, env)

	stdout, stderr, code := runJig(t, env, "import", "opencode", "--db", dbPath, "--dry-run")
	wantCode(t, code, 0, stdout, stderr)

	// Check before sessionIDs: `jig sessions` itself opens (and so
	// creates) jig.db, which would mask the dry run creating it.
	jigDB := filepath.Join(env.root, "data", "jig", "jig.db")
	if _, err := os.Stat(jigDB); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s = %v, want fs.ErrNotExist (dry run must create no jig database)", jigDB, err)
	}

	if ids := sessionIDs(t, env); len(ids) != 0 {
		t.Errorf("sessionIDs after dry run = %v, want none", ids)
	}
}

// TestE2E_ImportOpencode_DryRunAfterRealImport checks that a dry run after a
// real import still reports accurate "already present" counts, since the
// store already exists and is safe to open read/write without a dry run
// writing through it.
func TestE2E_ImportOpencode_DryRunAfterRealImport(t *testing.T) {
	env := newEnv(t)
	dbPath := writeOpencodeFixture(t, env)

	stdout, stderr, code := runJig(t, env, "import", "opencode", "--db", dbPath)
	wantCode(t, code, 0, stdout, stderr)

	stdout, stderr, code = runJig(t, env, "import", "opencode", "--db", dbPath, "--dry-run")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "already present: 3") {
		t.Errorf("dry-run stdout after a real import = %q, want it to contain %q", stdout, "already present: 3")
	}
}

func TestE2E_ImportOpencode_BadDB(t *testing.T) {
	env := newEnv(t)

	_, stderr, code := runJig(t, env, "import", "opencode", "--db", filepath.Join(env.root, "missing.db"))
	wantCode(t, code, 2, "", stderr)

	// A failed import must not have created jig's own database (finding 2
	// of the final review: opening and migrating jig.db used to happen
	// before the opencode source was validated).
	jigDBBeforeSessions := filepath.Join(env.root, "data", "jig", "jig.db")
	if _, err := os.Stat(jigDBBeforeSessions); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s = %v, want fs.ErrNotExist (a failed import must create no jig database)", jigDBBeforeSessions, err)
	}

	// Creates the jig data dir's own jig.db.
	if _, _, code := runJig(t, env, "sessions"); code != 0 {
		t.Fatalf("jig sessions: exit %d", code)
	}
	jigDB := filepath.Join(env.root, "data", "jig", "jig.db")
	_, stderr, code = runJig(t, env, "import", "opencode", "--db", jigDB)
	wantCode(t, code, 2, "", stderr)
	if !strings.Contains(stderr, "not an opencode database") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "not an opencode database")
	}
}
