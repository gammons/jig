package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestCompact_StoresSummaryUsingSmallModel(t *testing.T) {
	f := newFixture(t, defaultCfg())
	small := llmtest.New(llmtest.Text("the summary"))
	f.llms.clients[smallModel] = small
	f.llms.clients[mainModel] = llmtest.New()

	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("fix the bug"))
	last := f.save(sess.ID, core.RoleAssistant,
		text("looking"),
		core.Part{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "read", Input: []byte(`{}`)}},
		core.Part{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c1", Name: "read", Output: "SECRET FILE BODY"}},
	)

	if err := f.svc.Compact(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}

	if len(f.llms.asked) != 1 || f.llms.asked[0] != smallModel {
		t.Errorf("LLMs.For asked %v, want [%v]", f.llms.asked, smallModel)
	}
	if n := len(f.llms.clients[mainModel].Requests()); n != 0 {
		t.Errorf("main model got %d requests, want 0", n)
	}
	reqs := small.Requests()
	if len(reqs) != 1 {
		t.Fatalf("small model requests = %d, want 1", len(reqs))
	}
	compaction, _ := f.svc.d.Agents.Get("compaction")
	if len(reqs[0].System) != 1 || reqs[0].System[0] != compaction.Prompt {
		t.Errorf("System = %q, want the compaction prompt", reqs[0].System)
	}
	transcript := reqs[0].Messages[0].Parts[0].Text
	for _, want := range []string{"user: fix the bug", "assistant: looking", "[tool read]"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript %q missing %q", transcript, want)
		}
	}
	if strings.Contains(transcript, "SECRET FILE BODY") {
		t.Errorf("transcript %q includes tool output", transcript)
	}

	msgs, err := f.svc.Messages(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := msgs[len(msgs)-1]
	if got.Role != core.RoleAssistant || got.Agent != "compaction" || got.Model != smallModel.String() ||
		got.Status != core.StatusComplete {
		t.Errorf("compaction message = %+v", got)
	}
	if len(got.Parts) != 1 || got.Parts[0].Kind != core.PartCompaction || got.Parts[0].Text != "the summary" {
		t.Errorf("parts = %+v, want one compaction part", got.Parts)
	}
	if !got.CreatedAt.After(last.CreatedAt) {
		t.Errorf("CreatedAt %v not after last message %v", got.CreatedAt, last.CreatedAt)
	}

	hist, err := f.svc.History(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].ID != got.ID {
		t.Errorf("History = %v, want only the compaction message", msgIDs(hist))
	}
}

func TestCompact_IncludesPriorSummary(t *testing.T) {
	f := newFixture(t, defaultCfg())
	small := llmtest.New(llmtest.Text("second summary"))
	f.llms.clients[smallModel] = small

	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("old stuff"))
	f.save(sess.ID, core.RoleAssistant, core.Part{Kind: core.PartCompaction, Text: "first summary"})
	f.save(sess.ID, core.RoleUser, text("new stuff"))

	if err := f.svc.Compact(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	transcript := small.Requests()[0].Messages[0].Parts[0].Text
	if !strings.Contains(transcript, "first summary") || !strings.Contains(transcript, "user: new stuff") {
		t.Errorf("transcript %q missing prior summary or new message", transcript)
	}
	if strings.Contains(transcript, "old stuff") {
		t.Errorf("transcript %q includes pre-compaction messages", transcript)
	}
}

func TestCompact_UsesSessionModelWhenNoSmallModel(t *testing.T) {
	f := newFixture(t, core.Config{DefaultModel: mainModel.String()})
	other := core.ModelRef{Provider: "other", Model: "m"}
	f.llms.clients[other] = llmtest.New(llmtest.Text("sum"))

	sess := f.create("build")
	sess.Model = other.String()
	if err := f.svc.Update(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	f.save(sess.ID, core.RoleUser, text("hi"))

	if err := f.svc.Compact(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.llms.asked) != 1 || f.llms.asked[0] != other {
		t.Errorf("LLMs.For asked %v, want [%v]", f.llms.asked, other)
	}
}

func TestCompact_NothingToCompact(t *testing.T) {
	f := newFixture(t, defaultCfg())
	f.llms.clients[smallModel] = llmtest.New()
	sess := f.create("build")

	err := f.svc.Compact(context.Background(), sess.ID)
	if err == nil || err.Error() != "nothing to compact" {
		t.Errorf("empty session: err = %v, want nothing to compact", err)
	}

	f.save(sess.ID, core.RoleUser, text("hi"))
	f.save(sess.ID, core.RoleAssistant, core.Part{Kind: core.PartCompaction, Text: "summary"})
	err = f.svc.Compact(context.Background(), sess.ID)
	if err == nil || err.Error() != "nothing to compact" {
		t.Errorf("after compaction: err = %v, want nothing to compact", err)
	}
	if n := len(f.llms.clients[smallModel].Requests()); n != 0 {
		t.Errorf("model called %d times, want 0", n)
	}
}

func TestCompact_ResolveErrorReturned(t *testing.T) {
	f := newFixture(t, core.Config{})
	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("hi"))
	if err := f.svc.Compact(context.Background(), sess.ID); err == nil {
		t.Error("Compact with no model configured: want error")
	}
}

func TestCompact_LLMErrorSavesNothing(t *testing.T) {
	f := newFixture(t, defaultCfg())
	f.llms.clients[smallModel] = llmtest.New() // no scripted turn: errors
	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("hi"))
	f.clk.Advance(time.Second)

	if err := f.svc.Compact(context.Background(), sess.ID); err == nil {
		t.Fatal("want error")
	}
	msgs, err := f.svc.Messages(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("messages = %d, want 1", len(msgs))
	}
}

func TestCompact_SendsSmallModelsLowestEffort(t *testing.T) {
	f := newFixture(t, defaultCfg())
	small := llmtest.New(llmtest.Text("the summary"))
	f.llms.clients[smallModel] = small
	f.llms.clients[mainModel] = llmtest.New()
	f.llms.infos = map[core.ModelRef]core.ModelInfo{
		smallModel: {Efforts: []core.Effort{core.EffortMedium, core.EffortLow, core.EffortHigh}, DefaultEffort: core.EffortHigh},
	}
	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("fix the bug"))
	f.save(sess.ID, core.RoleAssistant, text("looking"))

	if err := f.svc.Compact(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if reqs := small.Requests(); len(reqs) != 1 || reqs[0].Effort != core.EffortLow {
		t.Errorf("small-model requests = %+v, want one with effort low", reqs)
	}
}
