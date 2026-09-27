package session

import (
	"context"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func msgIDs(msgs []core.Message) []core.MessageID {
	out := make([]core.MessageID, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

func equalIDs(a, b []core.MessageID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHistory_StartsAtLastCompaction(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("one"))
	f.save(sess.ID, core.RoleAssistant, core.Part{Kind: core.PartCompaction, Text: "first summary"})
	f.save(sess.ID, core.RoleUser, text("two"))
	second := f.save(sess.ID, core.RoleAssistant, core.Part{Kind: core.PartCompaction, Text: "second summary"})
	three := f.save(sess.ID, core.RoleUser, text("three"))
	reply := f.save(sess.ID, core.RoleAssistant, text("reply"))

	got, err := f.svc.History(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.MessageID{second.ID, three.ID, reply.ID}
	if !equalIDs(msgIDs(got), want) {
		t.Errorf("History = %v, want %v", msgIDs(got), want)
	}

	all, err := f.svc.Messages(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Errorf("Messages = %d, want 6", len(all))
	}
}

func TestHistory_NoCompactionReturnsAll(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")
	a := f.save(sess.ID, core.RoleUser, text("one"))
	b := f.save(sess.ID, core.RoleAssistant, text("two"))

	got, err := f.svc.History(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []core.MessageID{a.ID, b.ID}; !equalIDs(msgIDs(got), want) {
		t.Errorf("History = %v, want %v", msgIDs(got), want)
	}
}
