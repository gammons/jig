package importer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gammons/jig/internal/core"
)

// fakeSource is a slice-backed Source: it calls fn for each item in order
// and stops (returning fn's error) as soon as fn returns one.
type fakeSource struct {
	items []Item
}

func (s *fakeSource) Each(_ context.Context, fn func(Item) error) error {
	for _, it := range s.items {
		if err := fn(it); err != nil {
			return err
		}
	}
	return nil
}

// fakeStore is a map-backed Store. pre holds IDs that exist before the run
// starts; failIDs names IDs whose ImportSession call should fail.
type fakeStore struct {
	pre          map[core.SessionID]bool
	failIDs      map[core.SessionID]bool
	imported     map[core.SessionID]core.Session
	importedMsgs map[core.SessionID][]core.Message
	importCalls  []core.SessionID
	existsCalls  int
	onImport     func(id core.SessionID)
}

func newFakeStore(pre ...core.SessionID) *fakeStore {
	preSet := map[core.SessionID]bool{}
	for _, id := range pre {
		preSet[id] = true
	}
	return &fakeStore{
		pre:          preSet,
		failIDs:      map[core.SessionID]bool{},
		imported:     map[core.SessionID]core.Session{},
		importedMsgs: map[core.SessionID][]core.Message{},
	}
}

func (s *fakeStore) SessionExists(_ context.Context, id core.SessionID) (bool, error) {
	s.existsCalls++
	if s.pre[id] {
		return true, nil
	}
	_, ok := s.imported[id]
	return ok, nil
}

func (s *fakeStore) ImportSession(_ context.Context, sess core.Session, msgs []core.Message, _ []core.Todo) error {
	s.importCalls = append(s.importCalls, sess.ID)
	if s.onImport != nil {
		s.onImport(sess.ID)
	}
	if s.failIDs[sess.ID] {
		return fmt.Errorf("import failed: %s", sess.ID)
	}
	s.imported[sess.ID] = sess
	s.importedMsgs[sess.ID] = msgs
	return nil
}

// fakeMedia returns a deterministic Media for any data except "bad", which
// errors.
type fakeMedia struct {
	calls [][]byte
}

func (m *fakeMedia) Process(data []byte) (core.Media, core.ImageInfo, error) {
	m.calls = append(m.calls, data)
	if string(data) == "bad" {
		return core.Media{}, core.ImageInfo{}, errors.New("bad image")
	}
	return core.Media{MIME: "image/png", Ref: fmt.Sprintf("ref-%d", len(data))}, core.ImageInfo{}, nil
}

func TestRun_ImportsAndSkips(t *testing.T) {
	src := &fakeSource{items: []Item{
		{Session: core.Session{ID: "ses_a"}},
		{Session: core.Session{ID: "ses_b"}},
	}}
	store := newFakeStore("ses_a")
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Imported != 1 {
		t.Errorf("Imported = %d, want 1", summary.Imported)
	}
	if summary.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", summary.Skipped)
	}
	if _, ok := store.imported["ses_b"]; !ok {
		t.Error("ses_b was not imported")
	}
	if _, ok := store.imported["ses_a"]; ok {
		t.Error("ses_a (already present) should not have been imported")
	}
}

func TestRun_FailureContinues(t *testing.T) {
	src := &fakeSource{items: []Item{
		{Session: core.Session{ID: "ses_a"}},
		{Session: core.Session{ID: "ses_b"}},
	}}
	store := newFakeStore()
	store.failIDs["ses_a"] = true
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.Failed) != 1 || summary.Failed[0].ID != "ses_a" || summary.Failed[0].Err == nil {
		t.Errorf("Failed = %+v, want one failure for ses_a", summary.Failed)
	}
	if _, ok := store.imported["ses_b"]; !ok {
		t.Error("ses_b should still have been imported")
	}
}

func TestRun_FailedParentChildBecomesRoot(t *testing.T) {
	t.Run("failed parent", func(t *testing.T) {
		src := &fakeSource{items: []Item{
			{Session: core.Session{ID: "ses_p"}},
			{Session: core.Session{ID: "ses_c", ParentID: "ses_p"}},
		}}
		store := newFakeStore()
		store.failIDs["ses_p"] = true
		med := &fakeMedia{}

		summary, err := Run(context.Background(), src, store, med, Options{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if summary.OrphansAsRoots != 1 {
			t.Errorf("OrphansAsRoots = %d, want 1", summary.OrphansAsRoots)
		}
		got, ok := store.imported["ses_c"]
		if !ok {
			t.Fatal("ses_c was not imported")
		}
		if got.ParentID != "" {
			t.Errorf("ses_c.ParentID = %q, want empty", got.ParentID)
		}
	})

	t.Run("skipped parent keeps ParentID", func(t *testing.T) {
		src := &fakeSource{items: []Item{
			{Session: core.Session{ID: "ses_p"}},
			{Session: core.Session{ID: "ses_c", ParentID: "ses_p"}},
		}}
		store := newFakeStore("ses_p") // already present -> skipped, not failed
		med := &fakeMedia{}

		summary, err := Run(context.Background(), src, store, med, Options{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if summary.OrphansAsRoots != 0 {
			t.Errorf("OrphansAsRoots = %d, want 0", summary.OrphansAsRoots)
		}
		got, ok := store.imported["ses_c"]
		if !ok {
			t.Fatal("ses_c was not imported")
		}
		if got.ParentID != "ses_p" {
			t.Errorf("ses_c.ParentID = %q, want ses_p", got.ParentID)
		}
	})
}

func TestRun_MediaToBlobs(t *testing.T) {
	origAtt := &core.Attachment{Media: &core.Media{Data: []byte("img")}}
	origResult := &core.ToolResult{
		Output: "out",
		Media:  []core.Media{{Data: []byte("img2")}},
	}
	src := &fakeSource{items: []Item{
		{
			Session: core.Session{ID: "ses_m"},
			Messages: []core.Message{
				{
					ID:   "msg_1",
					Role: core.RoleUser,
					Parts: []core.Part{
						{Kind: core.PartAttachment, Attachment: origAtt},
					},
				},
				{
					ID:   "msg_2",
					Role: core.RoleAssistant,
					Parts: []core.Part{
						{Kind: core.PartToolResult, Result: origResult},
					},
				},
			},
		},
	}}
	store := newFakeStore()
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Imported != 1 {
		t.Fatalf("Imported = %d, want 1", summary.Imported)
	}

	msgs := store.importedMsgs["ses_m"]
	if len(msgs) != 2 {
		t.Fatalf("imported %d messages, want 2", len(msgs))
	}

	att := msgs[0].Parts[0].Attachment
	if att == nil || att.Media == nil {
		t.Fatalf("attachment/media missing: %+v", msgs[0].Parts[0])
	}
	if att.Media.Ref != "ref-3" || att.Media.Data != nil {
		t.Errorf("attachment media = %+v, want Ref ref-3, Data nil", att.Media)
	}

	res := msgs[1].Parts[0].Result
	if res == nil || len(res.Media) != 1 {
		t.Fatalf("result media missing: %+v", msgs[1].Parts[0])
	}
	if res.Media[0].Ref != "ref-4" || res.Media[0].Data != nil {
		t.Errorf("result media = %+v, want Ref ref-4, Data nil", res.Media[0])
	}

	// The source's own objects must not have been mutated.
	if origAtt.Media.Data == nil || string(origAtt.Media.Data) != "img" {
		t.Error("original attachment was mutated")
	}
	if origResult.Media[0].Data == nil || string(origResult.Media[0].Data) != "img2" {
		t.Error("original result media was mutated")
	}
}

func TestRun_MediaFailure(t *testing.T) {
	src := &fakeSource{items: []Item{
		{
			Session: core.Session{ID: "ses_m"},
			Messages: []core.Message{
				{
					ID:   "msg_1",
					Role: core.RoleUser,
					Parts: []core.Part{
						{Kind: core.PartAttachment, Attachment: &core.Attachment{Media: &core.Media{Data: []byte("bad")}}},
					},
				},
				{
					ID:   "msg_2",
					Role: core.RoleAssistant,
					Parts: []core.Part{
						{Kind: core.PartToolResult, Result: &core.ToolResult{
							Output: "out",
							Media:  []core.Media{{Data: []byte("bad")}},
						}},
					},
				},
			},
		},
	}}
	store := newFakeStore()
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Imported != 1 {
		t.Fatalf("Imported = %d, want 1", summary.Imported)
	}

	msgs := store.importedMsgs["ses_m"]
	part := msgs[0].Parts[0]
	if part.Kind != core.PartText || part.Text != "[image not imported: bad image]" {
		t.Errorf("attachment part = %+v, want PartText placeholder", part)
	}

	res := msgs[1].Parts[0].Result
	if res == nil || len(res.Media) != 0 {
		t.Fatalf("result media = %+v, want dropped", res)
	}
	wantOutput := "out\n[image not imported: bad image]"
	if res.Output != wantOutput {
		t.Errorf("Output = %q, want %q", res.Output, wantOutput)
	}
}

func TestRun_DryRun(t *testing.T) {
	src := &fakeSource{items: []Item{
		{Session: core.Session{ID: "ses_a"}},
		{Session: core.Session{ID: "ses_b"}},
	}}
	store := newFakeStore()
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Imported != 2 {
		t.Errorf("Imported = %d, want 2", summary.Imported)
	}
	if len(med.calls) != 0 {
		t.Errorf("Process called %d times, want 0", len(med.calls))
	}
	if len(store.importCalls) != 0 {
		t.Errorf("ImportSession called %d times, want 0", len(store.importCalls))
	}
	if store.existsCalls == 0 {
		t.Error("SessionExists was never consulted")
	}
}

func TestRun_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &fakeSource{items: []Item{
		{Session: core.Session{ID: "ses_a"}},
		{Session: core.Session{ID: "ses_b"}},
	}}
	store := newFakeStore()
	store.onImport = func(id core.SessionID) {
		if id == "ses_b" {
			cancel()
		}
	}
	// The second ImportSession call still returns an (unrelated) error;
	// the cancellation, not that error, should be what Run reports.
	store.failIDs["ses_b"] = true
	med := &fakeMedia{}

	summary, err := Run(ctx, src, store, med, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if summary.Imported != 1 {
		t.Errorf("Imported = %d, want 1", summary.Imported)
	}
	if len(summary.Failed) != 0 {
		t.Errorf("Failed = %+v, want none (cancellation isn't a session failure)", summary.Failed)
	}
}

func TestRun_StatsSummed(t *testing.T) {
	src := &fakeSource{items: []Item{
		{
			Session: core.Session{ID: "ses_a"},
			Stats: Stats{
				SkippedMessages:       []string{"msg_1: bad json"},
				DroppedTypes:          map[string]int{"system": 1},
				UntranslatedTools:     map[string]int{"weird": 2},
				UnimportedAttachments: 1,
				EmptyMessages:         1,
			},
		},
		{
			Session: core.Session{ID: "ses_b"},
			Stats: Stats{
				SkippedMessages:   []string{"msg_2: bad json"},
				DroppedTypes:      map[string]int{"system": 3, "idle": 1},
				UntranslatedTools: map[string]int{"weird": 1, "other": 1},
				EmptyMessages:     2,
			},
		},
	}}
	store := newFakeStore()
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := Stats{
		SkippedMessages:       []string{"msg_1: bad json", "msg_2: bad json"},
		DroppedTypes:          map[string]int{"system": 4, "idle": 1},
		UntranslatedTools:     map[string]int{"weird": 3, "other": 1},
		UnimportedAttachments: 1,
		EmptyMessages:         3,
	}
	if got := summary.Stats; !statsEqual(got, want) {
		t.Errorf("Stats = %+v, want %+v", got, want)
	}
}

func statsEqual(a, b Stats) bool {
	if len(a.SkippedMessages) != len(b.SkippedMessages) {
		return false
	}
	for i := range a.SkippedMessages {
		if a.SkippedMessages[i] != b.SkippedMessages[i] {
			return false
		}
	}
	if a.UnimportedAttachments != b.UnimportedAttachments || a.EmptyMessages != b.EmptyMessages {
		return false
	}
	if len(a.DroppedTypes) != len(b.DroppedTypes) {
		return false
	}
	for k, v := range a.DroppedTypes {
		if b.DroppedTypes[k] != v {
			return false
		}
	}
	if len(a.UntranslatedTools) != len(b.UntranslatedTools) {
		return false
	}
	for k, v := range a.UntranslatedTools {
		if b.UntranslatedTools[k] != v {
			return false
		}
	}
	return true
}

func TestRun_StatsMapsNonNilWhenEmpty(t *testing.T) {
	src := &fakeSource{}
	store := newFakeStore()
	med := &fakeMedia{}

	summary, err := Run(context.Background(), src, store, med, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Stats.DroppedTypes == nil {
		t.Error("Stats.DroppedTypes is nil, want non-nil")
	}
	if summary.Stats.UntranslatedTools == nil {
		t.Error("Stats.UntranslatedTools is nil, want non-nil")
	}
}

func TestRun_Progress(t *testing.T) {
	items := make([]Item, 250)
	for i := range items {
		items[i] = Item{Session: core.Session{ID: core.SessionID(fmt.Sprintf("ses_%d", i))}}
	}
	src := &fakeSource{items: items}
	store := newFakeStore()
	med := &fakeMedia{}

	var progress []int
	opts := Options{Progress: func(done int) { progress = append(progress, done) }}

	summary, err := Run(context.Background(), src, store, med, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Imported != 250 {
		t.Errorf("Imported = %d, want 250", summary.Imported)
	}
	want := []int{100, 200}
	if len(progress) != len(want) {
		t.Fatalf("progress = %v, want %v", progress, want)
	}
	for i := range want {
		if progress[i] != want[i] {
			t.Fatalf("progress = %v, want %v", progress, want)
		}
	}
}
