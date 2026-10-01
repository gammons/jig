package store

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

// importRef is a 64-lowercase-hex sha256-shaped blob ref for tests.
const importRef = "b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a"

func TestImportSession_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	parent := core.Session{
		ID:        "ses_parent",
		Title:     "parent",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_000),
		UpdatedAt: time.UnixMilli(1_700_000_000_000),
	}
	if err := s.CreateSession(ctx, parent); err != nil {
		t.Fatalf("CreateSession parent: %v", err)
	}

	sess := core.Session{
		ID:        "ses_imported",
		ParentID:  "ses_parent",
		Title:     "imported",
		Agent:     "build",
		Model:     "claude",
		Effort:    "high",
		Cwd:       "/tmp/proj",
		CreatedAt: time.UnixMilli(1_700_000_001_000),
		UpdatedAt: time.UnixMilli(1_700_000_002_000),
	}

	msgs := []core.Message{
		{
			ID:        "msg_user",
			SessionID: "ses_imported",
			Role:      core.RoleUser,
			Status:    core.StatusComplete,
			Parts: []core.Part{
				{Kind: core.PartText, Text: "hello"},
			},
			CreatedAt: time.UnixMilli(1_700_000_001_100),
		},
		{
			ID:        "msg_assistant",
			SessionID: "ses_imported",
			Role:      core.RoleAssistant,
			Agent:     "build",
			Model:     "claude",
			Status:    core.StatusComplete,
			Parts: []core.Part{
				{Kind: core.PartReasoning, Text: "thinking..."},
				{Kind: core.PartToolCall, Call: &core.ToolCall{
					ID:    "call_1",
					Name:  "read",
					Input: json.RawMessage(`{"path":"a.txt"}`),
				}},
				{Kind: core.PartToolResult, Result: &core.ToolResult{
					CallID: "call_1",
					Name:   "read",
					Output: "image 10x10 (123)",
					Media:  []core.Media{{MIME: "image/png", Ref: importRef}},
				}},
			},
			CreatedAt: time.UnixMilli(1_700_000_001_200),
		},
	}

	todos := []core.Todo{
		{Content: "write tests", Status: "in_progress"},
		{Content: "implement", Status: "pending"},
	}

	if err := s.ImportSession(ctx, sess, msgs, todos); err != nil {
		t.Fatalf("ImportSession: %v", err)
	}

	gotSess, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if gotSess != sess {
		t.Errorf("GetSession = %+v, want %+v", gotSess, sess)
	}

	gotMsgs, err := s.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if !reflect.DeepEqual(gotMsgs, msgs) {
		t.Errorf("ListMessages = %+v, want %+v", gotMsgs, msgs)
	}

	gotTodos, err := s.ListTodos(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if !reflect.DeepEqual(gotTodos, todos) {
		t.Errorf("ListTodos = %+v, want %+v", gotTodos, todos)
	}
}

func TestImportSession_ExistingIDFails(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	sess := core.Session{
		ID:        "ses_dup",
		Title:     "first",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_000),
		UpdatedAt: time.UnixMilli(1_700_000_000_000),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	err := s.ImportSession(ctx, sess, []core.Message{
		{
			ID:        "msg_1",
			SessionID: "ses_dup",
			Role:      core.RoleUser,
			Status:    core.StatusComplete,
			CreatedAt: time.UnixMilli(1_700_000_000_100),
		},
	}, nil)
	if err == nil {
		t.Fatal("ImportSession with existing ID: want error, got nil")
	}

	list, lerr := s.ListMessages(ctx, "ses_dup")
	if lerr != nil {
		t.Fatalf("ListMessages: %v", lerr)
	}
	if len(list) != 0 {
		t.Errorf("ListMessages after failed import = %+v, want empty", list)
	}
}

func TestImportSession_RollsBackOnFailure(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	sess := core.Session{
		ID:        "ses_rollback",
		Title:     "t",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_000),
		UpdatedAt: time.UnixMilli(1_700_000_000_000),
	}

	msgs := []core.Message{
		{
			ID:        "msg_ok",
			SessionID: "ses_rollback",
			Role:      core.RoleUser,
			Status:    core.StatusComplete,
			CreatedAt: time.UnixMilli(1_700_000_000_100),
		},
		{
			ID:        "msg_bad",
			SessionID: "ses_no_such_session",
			Role:      core.RoleUser,
			Status:    core.StatusComplete,
			CreatedAt: time.UnixMilli(1_700_000_000_200),
		},
	}

	todos := []core.Todo{
		{Content: "x", Status: "pending"},
	}

	err := s.ImportSession(ctx, sess, msgs, todos)
	if err == nil {
		t.Fatal("ImportSession with FK violation: want error, got nil")
	}

	exists, eerr := s.SessionExists(ctx, "ses_rollback")
	if eerr != nil {
		t.Fatalf("SessionExists: %v", eerr)
	}
	if exists {
		t.Error("SessionExists after rollback = true, want false")
	}

	list, lerr := s.ListTodos(ctx, "ses_rollback")
	if lerr != nil {
		t.Fatalf("ListTodos: %v", lerr)
	}
	if len(list) != 0 {
		t.Errorf("ListTodos after rollback = %+v, want empty", list)
	}
}

func TestSessionExists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	exists, err := s.SessionExists(ctx, "ses_missing")
	if err != nil {
		t.Fatalf("SessionExists: %v", err)
	}
	if exists {
		t.Error("SessionExists before CreateSession = true, want false")
	}

	sess := core.Session{
		ID:        "ses_present",
		Title:     "t",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_000),
		UpdatedAt: time.UnixMilli(1_700_000_000_000),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	exists, err = s.SessionExists(ctx, "ses_present")
	if err != nil {
		t.Fatalf("SessionExists: %v", err)
	}
	if !exists {
		t.Error("SessionExists after CreateSession = false, want true")
	}
}
