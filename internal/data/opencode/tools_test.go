package opencode

import (
	"encoding/json"
	"testing"
)

// decode unmarshals raw JSON into a comparable map, failing the test on
// error. A nil/empty raw message decodes to a nil map.
func decode(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return v
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestTranslateCall(t *testing.T) {
	tests := []struct {
		name       string
		callName   string
		input      map[string]any
		wantName   string
		wantInput  map[string]any
		translated bool
	}{
		{
			name:     "read",
			callName: "read",
			input:    map[string]any{"filePath": "/a"},
			wantName: "read",
			wantInput: map[string]any{
				"path": "/a",
			},
			translated: true,
		},
		{
			name:     "edit",
			callName: "edit",
			input: map[string]any{
				"filePath":   "/a",
				"oldString":  "x",
				"newString":  "y",
				"replaceAll": true,
			},
			wantName: "edit",
			wantInput: map[string]any{
				"path":        "/a",
				"old_string":  "x",
				"new_string":  "y",
				"replace_all": true,
			},
			translated: true,
		},
		{
			name:     "write",
			callName: "write",
			input:    map[string]any{"filePath": "/a", "content": "hi"},
			wantName: "write",
			wantInput: map[string]any{
				"path":    "/a",
				"content": "hi",
			},
			translated: true,
		},
		{
			name:     "bash",
			callName: "bash",
			input:    map[string]any{"command": "ls", "timeout": float64(5000)},
			wantName: "bash",
			wantInput: map[string]any{
				"command":    "ls",
				"timeout_ms": float64(5000),
			},
			translated: true,
		},
		{
			name:     "shell",
			callName: "shell",
			input:    map[string]any{"command": "ls", "timeout": float64(5000), "workdir": "/w"},
			wantName: "bash",
			wantInput: map[string]any{
				"command":    "ls",
				"timeout_ms": float64(5000),
				"workdir":    "/w",
			},
			translated: true,
		},
		{
			name:       "grep unchanged but translated",
			callName:   "grep",
			input:      map[string]any{"pattern": "foo"},
			wantName:   "grep",
			wantInput:  map[string]any{"pattern": "foo"},
			translated: true,
		},
		{
			name:       "glob unchanged but translated",
			callName:   "glob",
			input:      map[string]any{"pattern": "*.go"},
			wantName:   "glob",
			wantInput:  map[string]any{"pattern": "*.go"},
			translated: true,
		},
		{
			name:     "todowrite",
			callName: "todowrite",
			input: map[string]any{
				"todos": []any{
					map[string]any{"content": "c", "status": "pending", "priority": "high"},
				},
			},
			wantName: "todo",
			wantInput: map[string]any{
				"todos": []any{
					map[string]any{"content": "c", "status": "pending", "priority": "high"},
				},
			},
			translated: true,
		},
		{
			name:       "skill",
			callName:   "skill",
			input:      map[string]any{"name": "s"},
			wantName:   "skill",
			wantInput:  map[string]any{"id": "s"},
			translated: true,
		},
		{
			name:     "task",
			callName: "task",
			input: map[string]any{
				"subagent_type": "general",
				"description":   "d",
				"prompt":        "p",
				"task_id":       "ses_c",
			},
			wantName: "task",
			wantInput: map[string]any{
				"agent":       "general",
				"description": "d",
				"prompt":      "p",
				"session_id":  "ses_c",
			},
			translated: true,
		},
		{
			name:     "subagent",
			callName: "subagent",
			input: map[string]any{
				"agent":       "explore",
				"description": "d",
				"prompt":      "p",
				"sessionID":   "ses_c",
			},
			wantName: "task",
			wantInput: map[string]any{
				"agent":       "explore",
				"description": "d",
				"prompt":      "p",
				"session_id":  "ses_c",
			},
			translated: true,
		},
		{
			name:       "webfetch untranslated",
			callName:   "webfetch",
			input:      map[string]any{"url": "https://example.com"},
			wantName:   "webfetch",
			wantInput:  map[string]any{"url": "https://example.com"},
			translated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotInput, gotTranslated := translateCall(tt.callName, mustJSON(t, tt.input))
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
			if gotTranslated != tt.translated {
				t.Errorf("translated = %v, want %v", gotTranslated, tt.translated)
			}
			gotDecoded := decode(t, gotInput)
			wantDecoded := decode(t, mustJSON(t, tt.wantInput))
			gotJSON, _ := json.Marshal(gotDecoded)
			wantJSON, _ := json.Marshal(wantDecoded)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("input = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestTranslateCall_NoOverwrite(t *testing.T) {
	input := mustJSON(t, map[string]any{"path": "/keep", "filePath": "/other"})
	gotName, gotInput, translated := translateCall("read", input)
	if gotName != "read" {
		t.Fatalf("name = %q, want read", gotName)
	}
	if !translated {
		t.Fatalf("translated = false, want true")
	}
	var got map[string]any
	if err := json.Unmarshal(gotInput, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["path"] != "/keep" {
		t.Errorf("path = %v, want /keep (should not be overwritten)", got["path"])
	}
}

func TestTranslateCall_NonObjectInput(t *testing.T) {
	input := json.RawMessage(`"oops"`)
	gotName, gotInput, translated := translateCall("read", input)
	if gotName != "read" {
		t.Fatalf("name = %q, want read", gotName)
	}
	if translated {
		t.Fatalf("translated = true, want false for non-object input")
	}
	if string(gotInput) != string(input) {
		t.Errorf("input = %s, want unchanged %s", gotInput, input)
	}
}

func TestTranslateTaskResult(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		metadataID string
		want       string
	}{
		{
			name:       "metadata id used, body extracted",
			output:     "<task id=\"ses_x\" state=\"completed\">\n<task_result>\nbody\n</task_result>\n</task>",
			metadataID: "ses_c",
			want:       "<task_result session_id=\"ses_c\">\nbody\n</task_result>",
		},
		{
			name:       "no metadata id, uses tag id",
			output:     "<task id=\"ses_x\" state=\"completed\">\n<task_result>\nbody\n</task_result>\n</task>",
			metadataID: "",
			want:       "<task_result session_id=\"ses_x\">\nbody\n</task_result>",
		},
		{
			name:       "no id anywhere, output unchanged",
			output:     "plain",
			metadataID: "",
			want:       "plain",
		},
		{
			name:       "id but no task_result tags, whole output wrapped",
			output:     "plain",
			metadataID: "ses_c",
			want:       "<task_result session_id=\"ses_c\">\nplain\n</task_result>",
		},
		{
			name:       "metadata id containing a quote is %q-escaped like task.wrapResult",
			output:     "plain",
			metadataID: `ses_c"evil`,
			want:       "<task_result session_id=\"ses_c\\\"evil\">\nplain\n</task_result>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateTaskResult(tt.output, tt.metadataID)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
