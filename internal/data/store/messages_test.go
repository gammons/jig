package store

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

func seedSession(t *testing.T, s *Store, id core.SessionID) {
	t.Helper()
	sess := core.Session{
		ID:        id,
		Title:     "t",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_000),
		UpdatedAt: time.UnixMilli(1_700_000_000_000),
	}
	if err := s.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("seedSession CreateSession: %v", err)
	}
}

func TestMessages_SaveReplacesParts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "ses_1")

	msg := core.Message{
		ID:        "msg_1",
		SessionID: "ses_1",
		Role:      core.RoleAssistant,
		Agent:     "build",
		Model:     "claude",
		Parts: []core.Part{
			{Kind: core.PartText, Text: "one"},
			{Kind: core.PartText, Text: "two"},
		},
		Status:    core.StatusComplete,
		CreatedAt: time.UnixMilli(1_700_000_000_100),
	}
	if err := s.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msg.Parts = []core.Part{
		{Kind: core.PartText, Text: "a"},
		{Kind: core.PartText, Text: "b"},
		{Kind: core.PartText, Text: "c"},
	}
	if err := s.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage (replace): %v", err)
	}

	list, err := s.ListMessages(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListMessages returned %d messages, want 1", len(list))
	}
	if len(list[0].Parts) != 3 {
		t.Fatalf("ListMessages parts = %d, want 3", len(list[0].Parts))
	}
	want := []string{"a", "b", "c"}
	for i, p := range list[0].Parts {
		if p.Text != want[i] {
			t.Errorf("part %d = %q, want %q", i, p.Text, want[i])
		}
	}
}

func TestMessages_RoundTripAllPartKinds(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "ses_1")

	msg := core.Message{
		ID:        "msg_1",
		SessionID: "ses_1",
		Role:      core.RoleAssistant,
		Agent:     "build",
		Model:     "claude",
		Parts: []core.Part{
			{Kind: core.PartText, Text: "hello"},
			{Kind: core.PartReasoning, Text: "thinking..."},
			{Kind: core.PartToolCall, Call: &core.ToolCall{
				ID:    "call_1",
				Name:  "read",
				Input: json.RawMessage(`{"path":"a.txt"}`),
			}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{
				CallID:   "call_1",
				Name:     "read",
				Output:   "contents",
				IsError:  true,
				Metadata: map[string]string{"lines": "3"},
			}},
			{Kind: core.PartCompaction, Text: "summary of history"},
		},
		Usage: core.Usage{
			Input:      10,
			Output:     20,
			CacheRead:  5,
			CacheWrite: 2,
		},
		CostUSD:   0.0042,
		Status:    core.StatusComplete,
		CreatedAt: time.UnixMilli(1_700_000_000_200),
	}
	if err := s.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	list, err := s.ListMessages(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListMessages returned %d messages, want 1", len(list))
	}
	got := list[0]

	if !reflect.DeepEqual(got.Usage, msg.Usage) {
		t.Errorf("Usage = %+v, want %+v", got.Usage, msg.Usage)
	}
	if got.CostUSD != msg.CostUSD {
		t.Errorf("CostUSD = %v, want %v", got.CostUSD, msg.CostUSD)
	}
	if got.Status != msg.Status {
		t.Errorf("Status = %v, want %v", got.Status, msg.Status)
	}
	if !got.CreatedAt.Equal(msg.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, msg.CreatedAt)
	}
	if len(got.Parts) != len(msg.Parts) {
		t.Fatalf("Parts len = %d, want %d", len(got.Parts), len(msg.Parts))
	}
	for i, want := range msg.Parts {
		gp := got.Parts[i]
		if gp.Kind != want.Kind {
			t.Errorf("part %d Kind = %v, want %v", i, gp.Kind, want.Kind)
		}
		if gp.Text != want.Text {
			t.Errorf("part %d Text = %q, want %q", i, gp.Text, want.Text)
		}
		switch want.Kind {
		case core.PartToolCall:
			if gp.Call == nil || !reflect.DeepEqual(*gp.Call, *want.Call) {
				t.Errorf("part %d Call = %+v, want %+v", i, gp.Call, want.Call)
			}
		case core.PartToolResult:
			if gp.Result == nil || !reflect.DeepEqual(*gp.Result, *want.Result) {
				t.Errorf("part %d Result = %+v, want %+v", i, gp.Result, want.Result)
			}
		}
	}
}

func TestMessages_NilToolCallInputRoundTrips(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "ses_1")

	msg := core.Message{
		ID:        "msg_1",
		SessionID: "ses_1",
		Role:      core.RoleAssistant,
		Status:    core.StatusComplete,
		Parts: []core.Part{
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "call_nil", Name: "read", Input: nil}},
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "call_empty", Name: "read", Input: json.RawMessage(`{}`)}},
		},
		CreatedAt: time.UnixMilli(1_700_000_000_500),
	}
	if err := s.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	list, err := s.ListMessages(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(list) != 1 || len(list[0].Parts) != 2 {
		t.Fatalf("ListMessages = %+v, want 1 message with 2 parts", list)
	}

	nilCall := list[0].Parts[0].Call
	if nilCall == nil {
		t.Fatal("nil-input part: Call is nil")
	}
	if nilCall.Input != nil {
		t.Errorf("nil Input round-tripped as %q (%v), want nil", string(nilCall.Input), []byte(nilCall.Input))
	}

	emptyCall := list[0].Parts[1].Call
	if emptyCall == nil {
		t.Fatal("empty-input part: Call is nil")
	}
	if !reflect.DeepEqual(emptyCall.Input, json.RawMessage(`{}`)) {
		t.Errorf("{} Input round-tripped as %q, want %q", string(emptyCall.Input), "{}")
	}
}

func TestMessages_SaveNonexistentSessionErrors(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	msg := core.Message{
		ID:        "msg_orphan",
		SessionID: "no_such_session",
		Role:      core.RoleUser,
		Status:    core.StatusComplete,
		CreatedAt: time.UnixMilli(1_700_000_000_300),
	}
	if err := s.SaveMessage(ctx, msg); err == nil {
		t.Fatal("SaveMessage with unknown session_id: want error, got nil")
	}
}

func TestMessages_ListOrderByCreatedAtThenID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "ses_1")

	same := time.UnixMilli(1_700_000_000_400)
	msgs := []core.Message{
		{ID: "msg_b", SessionID: "ses_1", Role: core.RoleUser, Status: core.StatusComplete, CreatedAt: same},
		{ID: "msg_a", SessionID: "ses_1", Role: core.RoleUser, Status: core.StatusComplete, CreatedAt: same},
		{ID: "msg_z", SessionID: "ses_1", Role: core.RoleUser, Status: core.StatusComplete, CreatedAt: time.UnixMilli(1_700_000_000_100)},
	}
	for _, m := range msgs {
		if err := s.SaveMessage(ctx, m); err != nil {
			t.Fatalf("SaveMessage %s: %v", m.ID, err)
		}
	}

	list, err := s.ListMessages(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListMessages = %d, want 3", len(list))
	}
	wantOrder := []core.MessageID{"msg_z", "msg_a", "msg_b"}
	for i, want := range wantOrder {
		if list[i].ID != want {
			t.Errorf("list[%d].ID = %s, want %s", i, list[i].ID, want)
		}
	}
}

func TestMessages_ConcurrentSavesDifferentSessions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const n = 20
	for i := 0; i < n; i++ {
		id := core.SessionID(sessionIDFor(i))
		seedSession(t, s, id)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := core.SessionID(sessionIDFor(i))
			msg := core.Message{
				ID:        core.MessageID("msg_" + sessionIDFor(i)),
				SessionID: sid,
				Role:      core.RoleUser,
				Status:    core.StatusComplete,
				Parts:     []core.Part{{Kind: core.PartText, Text: "hi"}},
				CreatedAt: time.UnixMilli(1_700_000_000_000),
			}
			errs[i] = s.SaveMessage(ctx, msg)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("SaveMessage %d: %v", i, err)
		}
	}

	for i := 0; i < n; i++ {
		list, err := s.ListMessages(ctx, core.SessionID(sessionIDFor(i)))
		if err != nil {
			t.Fatalf("ListMessages %d: %v", i, err)
		}
		if len(list) != 1 {
			t.Errorf("ListMessages %d = %d messages, want 1", i, len(list))
		}
	}
}

func sessionIDFor(i int) string {
	return "ses_conc_" + string(rune('a'+i))
}
