package opencode

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/pathid"
)

func baseSessionRow() sessionRow {
	return sessionRow{
		ID:        "ses_root",
		ParentID:  "",
		Directory: "/work",
		Title:     "my title",
		Agent:     "build",
		ModelJSON: `{"id":"claude-opus-5-5","providerID":"anthropic","variant":"high"}`,
		Created:   1_700_000_000_000,
		Updated:   1_700_000_001_000,
	}
}

func msgRow(id, typ string, data any, created int64) messageRow {
	b, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return messageRow{ID: id, Type: typ, Data: string(b), Created: created}
}

func TestSession_Fields(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	sess, _, _ := tr.finish()

	if sess.Model != "anthropic/claude-opus-5-5" {
		t.Errorf("Model = %q, want anthropic/claude-opus-5-5", sess.Model)
	}
	if sess.Effort != core.EffortHigh {
		t.Errorf("Effort = %q, want high", sess.Effort)
	}
	if got := sess.CreatedAt; !got.Equal(time.UnixMilli(row.Created)) {
		t.Errorf("CreatedAt = %v, want %v", got, time.UnixMilli(row.Created))
	}
	if got := sess.UpdatedAt; !got.Equal(time.UnixMilli(row.Updated)) {
		t.Errorf("UpdatedAt = %v, want %v", got, time.UnixMilli(row.Updated))
	}

	row2 := baseSessionRow()
	row2.ModelJSON = `{"id":"m","providerID":"p","variant":"default"}`
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	sess2, _, _ := tr2.finish()
	if sess2.Effort != "" {
		t.Errorf("Effort for default variant = %q, want \"\"", sess2.Effort)
	}

	row3 := baseSessionRow()
	row3.ModelJSON = `{"id":"m","providerID":"p","variant":"weird"}`
	tr3 := newTranslator(row3, map[string]bool{"build": true})
	sess3, _, _ := tr3.finish()
	if sess3.Effort != "" {
		t.Errorf("Effort for weird variant = %q, want \"\"", sess3.Effort)
	}

	row4 := baseSessionRow()
	row4.Title = ""
	tr4 := newTranslator(row4, map[string]bool{"build": true})
	sess4, _, _ := tr4.finish()
	if sess4.Title != "(untitled)" {
		t.Errorf("Title for empty = %q, want (untitled)", sess4.Title)
	}
}

func TestSession_Agent(t *testing.T) {
	row := baseSessionRow()
	row.Agent = "build"
	tr := newTranslator(row, map[string]bool{"build": true})
	sess, _, _ := tr.finish()
	if sess.Agent != "build" {
		t.Errorf("Agent = %q, want build", sess.Agent)
	}

	row2 := baseSessionRow()
	row2.Agent = "sisyphus"
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	sess2, _, _ := tr2.finish()
	if sess2.Agent != "build" {
		t.Errorf("root unknown agent = %q, want build", sess2.Agent)
	}

	row3 := baseSessionRow()
	row3.Agent = "sisyphus"
	row3.ParentID = "ses_parent"
	tr3 := newTranslator(row3, map[string]bool{"build": true})
	sess3, _, _ := tr3.finish()
	if sess3.Agent != "general" {
		t.Errorf("child unknown agent = %q, want general", sess3.Agent)
	}
}

func TestSession_CwdFromLocationSwitch(t *testing.T) {
	row := baseSessionRow()
	row.Directory = "/a"
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "location-switched", map[string]any{
		"location": map[string]any{"directory": "/b"},
	}, 1))
	sess, _, stats := tr.finish()
	if sess.Cwd != pathid.Key("/b") {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, pathid.Key("/b"))
	}
	if stats.DroppedTypes["location-switched"] != 1 {
		t.Errorf("DroppedTypes[location-switched] = %d, want 1", stats.DroppedTypes["location-switched"])
	}
}

func TestUser_TextAndFiles(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})

	imgBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x01, 0x02, 0x03}
	txtBytes := []byte("hello text")

	tr.add(msgRow("msg_1", "user", map[string]any{
		"text": "hi",
		"files": []any{
			map[string]any{"name": "x.png", "mime": "image/png", "data": base64.StdEncoding.EncodeToString(imgBytes)},
			map[string]any{"name": "x.txt", "mime": "text/plain", "data": base64.StdEncoding.EncodeToString(txtBytes)},
			map[string]any{"name": "x.pdf", "mime": "application/pdf", "data": base64.StdEncoding.EncodeToString([]byte("pdfdata"))},
		},
	}, 42))

	_, msgs, stats := tr.finish()
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	m := msgs[0]
	if m.Role != core.RoleUser {
		t.Errorf("Role = %q, want user", m.Role)
	}
	if m.Status != core.StatusComplete {
		t.Errorf("Status = %q, want complete", m.Status)
	}
	if m.Agent != row.Agent {
		t.Errorf("Agent = %q, want %q (from session)", m.Agent, row.Agent)
	}
	if m.Model != "anthropic/claude-opus-5-5" {
		t.Errorf("Model = %q, want anthropic/claude-opus-5-5", m.Model)
	}
	if len(m.Parts) != 4 {
		t.Fatalf("len(Parts) = %d, want 4 (text + 3 files)", len(m.Parts))
	}
	if m.Parts[0].Kind != core.PartText || m.Parts[0].Text != "hi" {
		t.Errorf("Parts[0] = %+v, want text 'hi'", m.Parts[0])
	}

	img := m.Parts[1]
	if img.Kind != core.PartAttachment || img.Attachment == nil || img.Attachment.Media == nil {
		t.Fatalf("Parts[1] = %+v, want image attachment", img)
	}
	if img.Attachment.Media.MIME != "image/png" {
		t.Errorf("image MIME = %q, want image/png", img.Attachment.Media.MIME)
	}
	if string(img.Attachment.Media.Data) != string(imgBytes) {
		t.Errorf("image Data = %v, want %v", img.Attachment.Media.Data, imgBytes)
	}

	txt := m.Parts[2]
	if txt.Kind != core.PartAttachment || txt.Attachment == nil {
		t.Fatalf("Parts[2] = %+v, want text attachment", txt)
	}
	if txt.Attachment.Path != "x.txt" {
		t.Errorf("text attachment Path = %q, want x.txt", txt.Attachment.Path)
	}
	if txt.Attachment.Content != "hello text" {
		t.Errorf("text attachment Content = %q, want 'hello text'", txt.Attachment.Content)
	}

	pdf := m.Parts[3]
	if pdf.Kind != core.PartText || pdf.Text != "[attachment not imported: x.pdf (application/pdf)]" {
		t.Errorf("pdf part = %+v, want unimported text", pdf)
	}
	if stats.UnimportedAttachments != 1 {
		t.Errorf("UnimportedAttachments = %d, want 1", stats.UnimportedAttachments)
	}
}

func TestUser_FilesOnly(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	imgBytes := []byte{1, 2, 3}
	tr.add(msgRow("msg_1", "user", map[string]any{
		"text": "",
		"files": []any{
			map[string]any{"name": "x.png", "mime": "image/png", "data": base64.StdEncoding.EncodeToString(imgBytes)},
		},
	}, 1))
	_, msgs, _ := tr.finish()
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if len(msgs[0].Parts) != 1 {
		t.Fatalf("len(Parts) = %d, want 1", len(msgs[0].Parts))
	}

	row2 := baseSessionRow()
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	tr2.add(msgRow("msg_2", "user", map[string]any{"text": "", "files": []any{}}, 1))
	_, msgs2, stats2 := tr2.finish()
	if len(msgs2) != 0 {
		t.Fatalf("len(msgs2) = %d, want 0", len(msgs2))
	}
	if stats2.EmptyMessages != 1 {
		t.Errorf("EmptyMessages = %d, want 1", stats2.EmptyMessages)
	}
}

func TestSynthetic(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "user", map[string]any{"text": "a"}, 1))
	tr.add(msgRow("msg_2", "synthetic", map[string]any{"text": "ctx"}, 2))
	_, msgs, _ := tr.finish()
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if len(msgs[0].Parts) != 2 {
		t.Fatalf("len(Parts) = %d, want 2", len(msgs[0].Parts))
	}
	if msgs[0].Parts[0].Text != "a" || msgs[0].Parts[1].Text != "ctx" {
		t.Errorf("Parts = %+v, want [a, ctx]", msgs[0].Parts)
	}

	row2 := baseSessionRow()
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	tr2.add(msgRow("msg_1", "synthetic", map[string]any{"text": "ctx"}, 1))
	_, msgs2, _ := tr2.finish()
	if len(msgs2) != 1 {
		t.Fatalf("len(msgs2) = %d, want 1", len(msgs2))
	}
	if msgs2[0].Role != core.RoleUser {
		t.Errorf("Role = %q, want user", msgs2[0].Role)
	}
	if len(msgs2[0].Parts) != 1 || msgs2[0].Parts[0].Text != "ctx" {
		t.Errorf("Parts = %+v, want [ctx]", msgs2[0].Parts)
	}
}

func TestSynthetic_AfterAssistant(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", map[string]any{
		"time":    map[string]any{"created": 1, "completed": 2},
		"agent":   "build",
		"model":   map[string]any{"id": "m", "providerID": "p"},
		"content": []any{map[string]any{"type": "text", "text": "hi"}},
		"finish":  "stop",
	}, 1))
	tr.add(msgRow("msg_2", "synthetic", map[string]any{"text": "ctx"}, 2))
	_, msgs, _ := tr.finish()
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2 (assistant stays untouched, synthetic its own message)", len(msgs))
	}
	if msgs[0].Role != core.RoleAssistant {
		t.Errorf("msgs[0].Role = %q, want assistant", msgs[0].Role)
	}
	if len(msgs[0].Parts) != 1 {
		t.Errorf("msgs[0].Parts should be untouched, got %+v", msgs[0].Parts)
	}
	if msgs[1].Role != core.RoleUser || len(msgs[1].Parts) != 1 || msgs[1].Parts[0].Text != "ctx" {
		t.Errorf("msgs[1] = %+v, want its own user message with text ctx", msgs[1])
	}
}

func TestCompaction(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "compaction", map[string]any{"status": "completed", "summary": "S"}, 1))
	_, msgs, _ := tr.finish()
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	want := []core.Part{{Kind: core.PartCompaction, Text: "S"}}
	if len(msgs[0].Parts) != 1 || msgs[0].Parts[0] != want[0] {
		t.Errorf("Parts = %+v, want %+v", msgs[0].Parts, want)
	}
	if msgs[0].Role != core.RoleAssistant {
		t.Errorf("Role = %q, want assistant", msgs[0].Role)
	}
	if msgs[0].Agent != row.Agent || msgs[0].Model != "anthropic/claude-opus-5-5" {
		t.Errorf("Agent/Model = %q/%q, want from session", msgs[0].Agent, msgs[0].Model)
	}

	row2 := baseSessionRow()
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	tr2.add(msgRow("msg_1", "compaction", map[string]any{"status": "pending"}, 1))
	_, msgs2, stats2 := tr2.finish()
	if len(msgs2) != 0 {
		t.Fatalf("len(msgs2) = %d, want 0", len(msgs2))
	}
	if stats2.DroppedTypes["compaction"] != 1 {
		t.Errorf("DroppedTypes[compaction] = %d, want 1", stats2.DroppedTypes["compaction"])
	}
}

func assistantData_(overrides map[string]any) map[string]any {
	base := map[string]any{
		"time":    map[string]any{"created": 1, "completed": 2},
		"agent":   "build",
		"model":   map[string]any{"id": "m", "providerID": "p"},
		"content": []any{},
		"finish":  "stop",
		"cost":    0.0,
		"tokens":  map[string]any{"input": 0, "output": 0, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

func TestAssistant_UsageStatus(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"tokens": map[string]any{"input": 2, "output": 760, "reasoning": 84, "cache": map[string]any{"read": 98437, "write": 840}},
		"cost":   0.04,
	}), 1))
	_, msgs, _ := tr.finish()
	m := msgs[0]
	wantUsage := core.Usage{Input: 2, Output: 844, CacheRead: 98437, CacheWrite: 840}
	if m.Usage != wantUsage {
		t.Errorf("Usage = %+v, want %+v", m.Usage, wantUsage)
	}
	if m.CostUSD != 0.04 {
		t.Errorf("CostUSD = %v, want 0.04", m.CostUSD)
	}

	row2 := baseSessionRow()
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	tr2.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{"finish": "error"}), 1))
	_, msgs2, _ := tr2.finish()
	if msgs2[0].Status != core.StatusFailed {
		t.Errorf("Status = %q, want failed", msgs2[0].Status)
	}

	row3 := baseSessionRow()
	tr3 := newTranslator(row3, map[string]bool{"build": true})
	tr3.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"time": map[string]any{"created": 1},
	}), 1))
	_, msgs3, _ := tr3.finish()
	if msgs3[0].Status != core.StatusInterrupted {
		t.Errorf("Status = %q, want interrupted", msgs3[0].Status)
	}

	row4 := baseSessionRow()
	tr4 := newTranslator(row4, map[string]bool{"build": true})
	tr4.add(msgRow("msg_1", "assistant", assistantData_(nil), 1))
	_, msgs4, _ := tr4.finish()
	if msgs4[0].Status != core.StatusComplete {
		t.Errorf("Status = %q, want complete", msgs4[0].Status)
	}
}

func TestAssistant_Parts(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{"type": "reasoning", "text": "r"},
			map[string]any{"type": "reasoning", "text": ""},
			map[string]any{"type": "text", "text": "t"},
			map[string]any{
				"type": "tool",
				"id":   "toolu_1",
				"name": "edit",
				"state": map[string]any{
					"status": "completed",
					"input":  map[string]any{"filePath": "/a", "oldString": "x", "newString": "y"},
					"content": []any{
						map[string]any{"type": "text", "text": "ok"},
					},
				},
			},
		},
	}), 1))
	_, msgs, _ := tr.finish()
	m := msgs[0]
	if len(m.Parts) != 4 {
		t.Fatalf("len(Parts) = %d, want 4; got %+v", len(m.Parts), m.Parts)
	}
	if m.Parts[0].Kind != core.PartReasoning || m.Parts[0].Text != "r" {
		t.Errorf("Parts[0] = %+v, want reasoning r", m.Parts[0])
	}
	if m.Parts[1].Kind != core.PartText || m.Parts[1].Text != "t" {
		t.Errorf("Parts[1] = %+v, want text t", m.Parts[1])
	}
	call := m.Parts[2]
	if call.Kind != core.PartToolCall || call.Call == nil || call.Call.Name != "edit" {
		t.Fatalf("Parts[2] = %+v, want tool_call edit", call)
	}
	var input map[string]any
	if err := json.Unmarshal(call.Call.Input, &input); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if input["path"] != "/a" || input["old_string"] != "x" || input["new_string"] != "y" {
		t.Errorf("input = %+v, want translated keys", input)
	}
	result := m.Parts[3]
	if result.Kind != core.PartToolResult || result.Result == nil {
		t.Fatalf("Parts[3] = %+v, want tool_result", result)
	}
	if result.Result.CallID != "toolu_1" || result.Result.Name != "edit" {
		t.Errorf("Result = %+v, want CallID toolu_1 Name edit", result.Result)
	}
	if result.Result.Output != "ok" {
		t.Errorf("Result.Output = %q, want ok", result.Result.Output)
	}
}

func TestAssistant_ToolError(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "bash",
				"state": map[string]any{"status": "error", "error": map[string]any{"message": "boom"}},
			},
		},
	}), 1))
	_, msgs, _ := tr.finish()
	res := msgs[0].Parts[1].Result
	if !res.IsError || res.Output != "boom" {
		t.Errorf("Result = %+v, want IsError true Output boom", res)
	}

	row2 := baseSessionRow()
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	tr2.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "bash",
				"state": map[string]any{"status": "error"},
			},
		},
	}), 1))
	_, msgs2, _ := tr2.finish()
	res2 := msgs2[0].Parts[1].Result
	if !res2.IsError || res2.Output != "tool error" {
		t.Errorf("Result = %+v, want IsError true Output 'tool error'", res2)
	}
}

func TestAssistant_ToolRunning(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "bash",
				"state": map[string]any{"status": "running"},
			},
		},
	}), 1))
	_, msgs, _ := tr.finish()
	if len(msgs[0].Parts) != 1 {
		t.Fatalf("len(Parts) = %d, want 1 (call only)", len(msgs[0].Parts))
	}
	if msgs[0].Parts[0].Kind != core.PartToolCall {
		t.Errorf("Parts[0].Kind = %q, want tool_call", msgs[0].Parts[0].Kind)
	}
}

func TestAssistant_ToolFileContent(t *testing.T) {
	imgBytes := []byte{9, 9, 9}
	imgURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgBytes)

	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "bash",
				"state": map[string]any{
					"status":  "completed",
					"content": []any{map[string]any{"type": "file", "uri": imgURI}},
				},
			},
		},
	}), 1))
	_, msgs, _ := tr.finish()
	res := msgs[0].Parts[1].Result
	if len(res.Media) != 1 {
		t.Fatalf("len(Media) = %d, want 1", len(res.Media))
	}
	if string(res.Media[0].Data) != string(imgBytes) || res.Media[0].MIME != "image/png" {
		t.Errorf("Media[0] = %+v, want decoded png", res.Media[0])
	}

	pdfURI := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString([]byte("pdf"))
	row2 := baseSessionRow()
	tr2 := newTranslator(row2, map[string]bool{"build": true})
	tr2.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "bash",
				"state": map[string]any{
					"status":  "completed",
					"content": []any{map[string]any{"type": "file", "uri": pdfURI}},
				},
			},
		},
	}), 1))
	_, msgs2, _ := tr2.finish()
	res2 := msgs2[0].Parts[1].Result
	if len(res2.Media) != 0 {
		t.Errorf("len(Media) = %d, want 0", len(res2.Media))
	}
	if res2.Output != "\n[file not imported: application/pdf]" {
		t.Errorf("Output = %q, want file not imported note", res2.Output)
	}
}

func TestAssistant_TaskResultRewritten(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "task",
				"state": map[string]any{
					"status":   "completed",
					"input":    map[string]any{"subagent_type": "general", "task_id": "ses_c"},
					"content":  []any{map[string]any{"type": "text", "text": "result body"}},
					"metadata": map[string]any{"sessionId": "ses_c"},
				},
			},
		},
	}), 1))
	_, msgs, _ := tr.finish()
	res := msgs[0].Parts[1].Result
	want := `<task_result session_id="ses_c">`
	if len(res.Output) < len(want) || res.Output[:len(want)] != want {
		t.Errorf("Output = %q, want to start with %q", res.Output, want)
	}
}

func TestAssistant_UntranslatedCounted(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(msgRow("msg_1", "assistant", assistantData_(map[string]any{
		"content": []any{
			map[string]any{
				"type": "tool", "id": "t1", "name": "webfetch",
				"state": map[string]any{"status": "completed", "input": map[string]any{"url": "https://x"}},
			},
		},
	}), 1))
	_, _, stats := tr.finish()
	if stats.UntranslatedTools["webfetch"] != 1 {
		t.Errorf("UntranslatedTools[webfetch] = %d, want 1", stats.UntranslatedTools["webfetch"])
	}
}

func TestBadJSON(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	tr.add(messageRow{ID: "msg_bad", Type: "user", Data: "{", Created: 1})
	_, msgs, stats := tr.finish()
	if len(msgs) != 0 {
		t.Fatalf("len(msgs) = %d, want 0", len(msgs))
	}
	if len(stats.SkippedMessages) != 1 {
		t.Fatalf("len(SkippedMessages) = %d, want 1", len(stats.SkippedMessages))
	}
	if got, want := stats.SkippedMessages[0], "msg_bad"; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("SkippedMessages[0] = %q, want to start with %q", got, want)
	}
}

// TestAppendMessage_StrictlyIncreasingOnTies feeds three rows sharing the
// same Created, whose ids sort in reverse lexical order (msg_c, msg_b,
// msg_a), in that seq order (as Each would feed them). Finding 1 of the
// final review: opencode rows can tie (or invert) on created_at while
// disagreeing with seq order, so ListMessages' created_at-then-id sort
// could reorder them. The translator must keep CreatedAt strictly
// increasing in the order messages are appended, so ListMessages' sort
// reproduces seq order instead.
func TestAppendMessage_StrictlyIncreasingOnTies(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})

	const created = int64(5000)
	tr.add(msgRow("msg_c", "user", map[string]any{"text": "c"}, created))
	tr.add(msgRow("msg_b", "user", map[string]any{"text": "b"}, created))
	tr.add(msgRow("msg_a", "user", map[string]any{"text": "a"}, created))

	_, msgs, _ := tr.finish()
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}
	if !msgs[0].CreatedAt.Equal(time.UnixMilli(created)) {
		t.Errorf("msgs[0].CreatedAt = %v, want its original time %v (unchanged, first message)", msgs[0].CreatedAt, time.UnixMilli(created))
	}
	for i := 1; i < len(msgs); i++ {
		if !msgs[i].CreatedAt.After(msgs[i-1].CreatedAt) {
			t.Errorf("msgs[%d].CreatedAt = %v, want strictly after msgs[%d].CreatedAt = %v", i, msgs[i].CreatedAt, i-1, msgs[i-1].CreatedAt)
		}
	}
}

func TestDroppedTypes(t *testing.T) {
	row := baseSessionRow()
	tr := newTranslator(row, map[string]bool{"build": true})
	for _, typ := range []string{"system", "idle", "model-switched", "bogus"} {
		tr.add(msgRow("msg_"+typ, typ, map[string]any{}, 1))
	}
	_, msgs, stats := tr.finish()
	if len(msgs) != 0 {
		t.Fatalf("len(msgs) = %d, want 0", len(msgs))
	}
	for _, typ := range []string{"system", "idle", "model-switched", "bogus"} {
		if stats.DroppedTypes[typ] != 1 {
			t.Errorf("DroppedTypes[%q] = %d, want 1", typ, stats.DroppedTypes[typ])
		}
	}
}
