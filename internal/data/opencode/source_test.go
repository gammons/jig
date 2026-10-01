package opencode

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/opencode/opencodetest"
	"github.com/gammons/jig/internal/data/store"
)

var errStop = errors.New("stop")

func fixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "opencode.db")
}

// writeNonOpencodeDB creates a SQLite file at path holding only a
// "sessions" table, so Open must reject it as not an opencode database.
func writeNonOpencodeDB(path string) error {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY)`)
	return err
}

func TestEach_ParentsBeforeChildren(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{ID: "ses_b", Directory: "/work", Created: 2, Updated: 2},
		{ID: "ses_a", Directory: "/work", Created: 1, Updated: 1},
		{ID: "ses_c", ParentID: "ses_b", Directory: "/work", Created: 3, Updated: 3},
		{ID: "ses_d", ParentID: "ses_c", Directory: "/work", Created: 4, Updated: 4},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	var got []string
	err = src.Each(context.Background(), func(item Item) error {
		got = append(got, string(item.Session.ID))
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}

	want := []string{"ses_a", "ses_b", "ses_c", "ses_d"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestEach_OrphanAndCycleBecomeRoots(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{ID: "ses_o", ParentID: "ses_missing", Directory: "/work", Created: 1, Updated: 1},
		{ID: "ses_x", ParentID: "ses_y", Directory: "/work", Created: 2, Updated: 2},
		{ID: "ses_y", ParentID: "ses_x", Directory: "/work", Created: 3, Updated: 3},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	seen := map[string]int{}
	var order []string
	err = src.Each(context.Background(), func(item Item) error {
		seen[string(item.Session.ID)]++
		order = append(order, string(item.Session.ID))
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}

	want := []string{"ses_o", "ses_x", "ses_y"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want each of %v exactly once", order, want)
	}
	for _, id := range want {
		if seen[id] != 1 {
			t.Errorf("session %s seen %d times, want 1 (order: %v)", id, seen[id], order)
		}
	}
}

func TestEach_MessagesBySeqTodosByPosition(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{
			ID: "ses_a", Directory: "/work", Created: 1, Updated: 1,
			Messages: []opencodetest.Message{
				{ID: "msg_2", Type: "user", Data: `{"text":"second"}`, Seq: 2, Created: 20},
				{ID: "msg_1", Type: "user", Data: `{"text":"first"}`, Seq: 1, Created: 10},
			},
			Todos: []opencodetest.Todo{
				{Content: "second todo", Status: "pending", Position: 1},
				{Content: "first todo", Status: "completed", Position: 0},
			},
		},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	var item Item
	err = src.Each(context.Background(), func(i Item) error {
		item = i
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}

	if len(item.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(item.Messages))
	}
	if string(item.Messages[0].ID) != "msg_1" || string(item.Messages[1].ID) != "msg_2" {
		t.Errorf("messages not in seq order: %q, %q", item.Messages[0].ID, item.Messages[1].ID)
	}

	if len(item.Todos) != 2 {
		t.Fatalf("got %d todos, want 2", len(item.Todos))
	}
	if item.Todos[0].Content != "first todo" || item.Todos[0].Status != "completed" {
		t.Errorf("todo[0] = %+v, want first todo/completed", item.Todos[0])
	}
	if item.Todos[1].Content != "second todo" || item.Todos[1].Status != "pending" {
		t.Errorf("todo[1] = %+v, want second todo/pending", item.Todos[1])
	}
}

func TestEach_StopsOnFnError(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{ID: "ses_a", Directory: "/work", Created: 1, Updated: 1},
		{ID: "ses_b", Directory: "/work", Created: 2, Updated: 2},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	calls := 0
	err = src.Each(context.Background(), func(item Item) error {
		calls++
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Errorf("Each returned %v, want errStop", err)
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1", calls)
	}
}

func TestEach_Cancelled(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{ID: "ses_a", Directory: "/work", Created: 1, Updated: 1},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = src.Each(ctx, func(item Item) error {
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Each returned %v, want context.Canceled", err)
	}
}

func TestOpen_ReadOnly(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{ID: "ses_a", Directory: "/work", Created: 1, Updated: 1},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture before: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	if _, err := src.db.Exec(`INSERT INTO session_v2 (id, parent_id, project_id, directory, title, slug, version, agent, model, time_created, time_updated)
		VALUES ('ses_write', NULL, 'p', '/d', NULL, 's', 'v', NULL, NULL, 1, 1)`); err == nil {
		t.Fatal("INSERT through a read-only connection succeeded, want error")
	}

	err = src.Each(context.Background(), func(item Item) error { return nil })
	if err != nil {
		t.Fatalf("Each: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture after: %v", err)
	}
	if string(before) != string(after) {
		t.Error("fixture bytes changed after Open+Each, want unchanged")
	}
}

func TestOpen_Missing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")

	_, err := Open(context.Background(), path, Options{})
	if err == nil {
		t.Fatal("Open on a missing path succeeded, want error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open error = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
}

func TestOpen_PathWithSpecialChars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weird?name#1 test.db")
	sessions := []opencodetest.Session{
		{ID: "ses_a", Directory: "/work", Created: 1, Updated: 1},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("os.Stat(%q): %v, want the fixture file to exist at its exact literal path", path, err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	var got []string
	err = src.Each(context.Background(), func(item Item) error {
		got = append(got, string(item.Session.ID))
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(got) != 1 || got[0] != "ses_a" {
		t.Errorf("got %v, want [ses_a]", got)
	}
}

func TestOpen_NotOpencode(t *testing.T) {
	path := fixturePath(t)

	if err := writeNonOpencodeDB(path); err != nil {
		t.Fatalf("writeNonOpencodeDB: %v", err)
	}

	_, err := Open(context.Background(), path, Options{})
	if err == nil {
		t.Fatal("Open on a non-opencode DB succeeded, want error")
	}
	var ne *NotOpencodeError
	if !errors.As(err, &ne) {
		t.Errorf("Open error = %v (%T), want *NotOpencodeError", err, err)
	}
}

// TestEach_SameMillisecondTieKeepsSeqOrderThroughStore is a reader-level
// regression test for finding 1 of the final review: two messages at the
// same Created where the later-seq message's id sorts lexically smaller
// than the earlier one's. ListMessages orders by (created_at, id), so
// without the translator's CreatedAt bump this would come back in id
// order (msg_a before msg_b) rather than seq order (msg_b then msg_a). It
// round-trips through a real store.Store, exactly as `jig import opencode`
// does, to prove the fix holds end to end.
func TestEach_SameMillisecondTieKeepsSeqOrderThroughStore(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{
			ID: "ses_tie", Directory: "/work", Created: 1, Updated: 1,
			Messages: []opencodetest.Message{
				{ID: "msg_b", Type: "user", Data: `{"text":"first in seq"}`, Seq: 1, Created: 100},
				{ID: "msg_a", Type: "user", Data: `{"text":"second in seq"}`, Seq: 2, Created: 100},
			},
		},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	var item Item
	err = src.Each(context.Background(), func(i Item) error {
		item = i
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(item.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(item.Messages))
	}

	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "jig.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	if err := st.ImportSession(context.Background(), item.Session, item.Messages, nil); err != nil {
		t.Fatalf("ImportSession: %v", err)
	}

	got, err := st.ListMessages(context.Background(), item.Session.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListMessages returned %d messages, want 2", len(got))
	}
	if string(got[0].ID) != "msg_b" || string(got[1].ID) != "msg_a" {
		t.Errorf("ListMessages order = [%s, %s], want [msg_b, msg_a] (seq order, despite msg_a < msg_b lexically)", got[0].ID, got[1].ID)
	}
	if !got[0].CreatedAt.Before(got[1].CreatedAt) {
		t.Errorf("got[0].CreatedAt = %v, want strictly before got[1].CreatedAt = %v", got[0].CreatedAt, got[1].CreatedAt)
	}
}

// TestEach_FeedOrderSortsStableByCreatedAtThenID is a reader-level
// (translator-driven) check, independent of internal/data/store, that
// sorting Each's yielded messages by (CreatedAt, ID) — exactly what
// ListMessages' ORDER BY does — reproduces the feed (seq) order Each
// produced them in. It covers the same same-millisecond, reverse-id tie
// as the store round-trip test above, so the invariant is checked even if
// a future change makes data/opencode unable to import internal/data/store.
func TestEach_FeedOrderSortsStableByCreatedAtThenID(t *testing.T) {
	path := fixturePath(t)
	sessions := []opencodetest.Session{
		{
			ID: "ses_tie", Directory: "/work", Created: 1, Updated: 1,
			Messages: []opencodetest.Message{
				{ID: "msg_b", Type: "user", Data: `{"text":"first in seq"}`, Seq: 1, Created: 100},
				{ID: "msg_a", Type: "user", Data: `{"text":"second in seq"}`, Seq: 2, Created: 100},
			},
		},
	}
	if err := opencodetest.Write(path, sessions); err != nil {
		t.Fatalf("Write: %v", err)
	}

	src, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	var item Item
	err = src.Each(context.Background(), func(i Item) error {
		item = i
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}

	feedOrder := make([]string, len(item.Messages))
	for i, m := range item.Messages {
		feedOrder[i] = string(m.ID)
	}

	sorted := append([]string(nil), feedOrder...)
	msgs := item.Messages
	sort.SliceStable(sorted, func(i, j int) bool {
		mi, mj := indexByID(msgs, sorted[i]), indexByID(msgs, sorted[j])
		if !msgs[mi].CreatedAt.Equal(msgs[mj].CreatedAt) {
			return msgs[mi].CreatedAt.Before(msgs[mj].CreatedAt)
		}
		return sorted[i] < sorted[j]
	})

	if len(sorted) != len(feedOrder) {
		t.Fatalf("len(sorted) = %d, want %d", len(sorted), len(feedOrder))
	}
	for i := range feedOrder {
		if sorted[i] != feedOrder[i] {
			t.Errorf("(created_at, id) sort = %v, want feed order %v", sorted, feedOrder)
			break
		}
	}
}

// indexByID returns the index of the message with the given id in msgs.
func indexByID(msgs []core.Message, id string) int {
	for i, m := range msgs {
		if string(m.ID) == id {
			return i
		}
	}
	return -1
}
