// Package opencode reads opencode's SQLite session database, read-only,
// and translates its sessions into jig's internal/core values so they can
// be imported into jig's own store. It never writes to or deletes from
// opencode's database.
package opencode

import (
	"encoding/json"
	"regexp"
	"strings"
)

// toolRename describes how one opencode tool call maps onto jig's tool
// set: the jig tool name and the input key renames to apply.
type toolRename struct {
	jigName string
	keys    map[string]string
}

// toolRenames returns a fresh copy of the opencode→jig tool translation
// table (spec §5.4). It is a function, not a package var, so callers can
// never observe or mutate shared state.
func toolRenames() map[string]toolRename {
	return map[string]toolRename{
		"read": {
			jigName: "read",
			keys:    map[string]string{"filePath": "path"},
		},
		"edit": {
			jigName: "edit",
			keys: map[string]string{
				"filePath":   "path",
				"oldString":  "old_string",
				"newString":  "new_string",
				"replaceAll": "replace_all",
			},
		},
		"write": {
			jigName: "write",
			keys:    map[string]string{"filePath": "path"},
		},
		"bash": {
			jigName: "bash",
			keys:    map[string]string{"timeout": "timeout_ms"},
		},
		"shell": {
			jigName: "bash",
			keys:    map[string]string{"timeout": "timeout_ms"},
		},
		"grep": {
			jigName: "grep",
			keys:    map[string]string{},
		},
		"glob": {
			jigName: "glob",
			keys:    map[string]string{},
		},
		"todowrite": {
			jigName: "todo",
			keys:    map[string]string{},
		},
		"skill": {
			jigName: "skill",
			keys:    map[string]string{"name": "id"},
		},
		"task": {
			jigName: "task",
			keys: map[string]string{
				"subagent_type": "agent",
				"task_id":       "session_id",
			},
		},
		"subagent": {
			jigName: "task",
			keys:    map[string]string{"sessionID": "session_id"},
		},
	}
}

// translateCall translates one opencode tool call's name and input into
// jig's equivalents, per spec §5.4. translated is false when name is
// outside the rename table (input is returned unchanged in that case).
// Input that is not a JSON object is also returned unchanged.
func translateCall(name string, input json.RawMessage) (jigName string, jigInput json.RawMessage, translated bool) {
	rename, ok := toolRenames()[name]
	if !ok {
		return name, input, false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return rename.jigName, input, false
	}

	out := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	for from, to := range rename.keys {
		v, present := fields[from]
		if !present {
			continue
		}
		if _, taken := fields[to]; taken {
			continue
		}
		delete(out, from)
		out[to] = v
	}

	marshaled, err := json.Marshal(out)
	if err != nil {
		return rename.jigName, input, false
	}
	return rename.jigName, marshaled, true
}

// translateTaskResult rewrites a translated task call's result output to
// jig's <task_result session_id="…"> format (task.wrapResult's format),
// per spec §5.4's "Subagent results". The child ID is metadataSessionID
// if set, else the id attribute of a leading <task id="…" tag in output.
// If no child ID is found, output is returned unchanged.
func translateTaskResult(output string, metadataSessionID string) string {
	childID := metadataSessionID
	if childID == "" {
		if m := taskTagIDPattern().FindStringSubmatch(output); m != nil {
			childID = m[1]
		}
	}
	if childID == "" {
		return output
	}

	body := output
	if m := taskResultBodyPattern().FindStringSubmatch(output); m != nil {
		body = m[1]
	}
	body = strings.TrimSpace(body)

	return "<task_result session_id=\"" + childID + "\">\n" + body + "\n</task_result>"
}

// taskTagIDPattern matches the id attribute of a leading <task id="…" tag.
func taskTagIDPattern() *regexp.Regexp {
	return regexp.MustCompile(`<task\s+id="([^"]*)"`)
}

// taskResultBodyPattern matches the body between <task_result> tags.
func taskResultBodyPattern() *regexp.Regexp {
	return regexp.MustCompile(`(?s)<task_result>(.*?)</task_result>`)
}
