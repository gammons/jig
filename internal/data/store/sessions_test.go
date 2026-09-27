package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jig.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSessions_CreateGetList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	created := time.UnixMilli(1_700_000_000_000)
	updated := time.UnixMilli(1_700_000_001_000)
	sess := core.Session{
		ID:        "ses_1",
		Title:     "first",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp/proj",
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.GetSession(ctx, "ses_1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got != sess {
		t.Errorf("GetSession = %+v, want %+v", got, sess)
	}

	updatedSess := sess
	updatedSess.Title = "renamed"
	updatedSess.Agent = "review"
	updatedSess.Model = "gpt"
	updatedSess.UpdatedAt = time.UnixMilli(1_700_000_002_000)
	if err := s.UpdateSession(ctx, updatedSess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got2, err := s.GetSession(ctx, "ses_1")
	if err != nil {
		t.Fatalf("GetSession after update: %v", err)
	}
	if got2 != updatedSess {
		t.Errorf("GetSession after update = %+v, want %+v", got2, updatedSess)
	}

	list, err := s.ListSessions(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 || list[0] != updatedSess {
		t.Errorf("ListSessions = %+v, want [%+v]", list, updatedSess)
	}
}

func TestSessions_ListRootsExcludesChildren(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	root := core.Session{
		ID:        "ses_root",
		Title:     "root",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_000),
		UpdatedAt: time.UnixMilli(1_700_000_000_000),
	}
	if err := s.CreateSession(ctx, root); err != nil {
		t.Fatalf("CreateSession root: %v", err)
	}

	child := core.Session{
		ID:        "ses_child",
		ParentID:  "ses_root",
		Title:     "child",
		Agent:     "build",
		Model:     "claude",
		Cwd:       "/tmp",
		CreatedAt: time.UnixMilli(1_700_000_000_500),
		UpdatedAt: time.UnixMilli(1_700_000_000_500),
	}
	if err := s.CreateSession(ctx, child); err != nil {
		t.Fatalf("CreateSession child: %v", err)
	}

	roots, err := s.ListSessions(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListSessions roots: %v", err)
	}
	if len(roots) != 1 || roots[0].ID != "ses_root" {
		t.Errorf("ListSessions(root) = %+v, want only ses_root", roots)
	}

	children, err := s.ListSessions(ctx, "ses_root", 0)
	if err != nil {
		t.Fatalf("ListSessions children: %v", err)
	}
	if len(children) != 1 || children[0].ID != "ses_child" {
		t.Errorf("ListSessions(ses_root) = %+v, want only ses_child", children)
	}
}

func TestSessions_ListOrderAndLimit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.UnixMilli(1_700_000_000_000)
	for i, id := range []string{"ses_a", "ses_b", "ses_c"} {
		sess := core.Session{
			ID:        core.SessionID(id),
			Title:     id,
			Agent:     "build",
			Model:     "claude",
			Cwd:       "/tmp",
			CreatedAt: base,
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession %s: %v", id, err)
		}
	}

	list, err := s.ListSessions(ctx, "", 2)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListSessions with limit 2 = %d items, want 2", len(list))
	}
	if list[0].ID != "ses_c" || list[1].ID != "ses_b" {
		t.Errorf("ListSessions order = [%s, %s], want [ses_c, ses_b]", list[0].ID, list[1].ID)
	}
}

func TestSessions_GetMissingIsErrNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.GetSession(ctx, "does_not_exist")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession missing: err = %v, want ErrNotFound", err)
	}
	if !s.IsNotFound(err) || s.IsNotFound(errors.New("other")) {
		t.Error("IsNotFound misclassifies errors")
	}
}

func TestSessions_UpdateMissingIsErrNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	err := s.UpdateSession(ctx, core.Session{ID: "does_not_exist"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSession missing: err = %v, want ErrNotFound", err)
	}
}
