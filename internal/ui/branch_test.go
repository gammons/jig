package ui

import (
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core/event"
)

func withBranch(p *listProject) testOpt {
	return func(c *testConfig) { c.project = p }
}

func TestApp_StatusShowsBranchAtStartup(t *testing.T) {
	t.Parallel()
	p := &listProject{branch: "feat/x"}
	ta := newTestApp(t, withBranch(p))
	if got := ta.app.statusState().Branch; got != "feat/x" {
		t.Errorf("status branch = %q, want feat/x", got)
	}
}

func TestApp_BranchRefreshedAfterRun(t *testing.T) {
	t.Parallel()
	p := &listProject{branch: "main"}
	ta := newTestApp(t, withBranch(p))
	calls := p.branches

	ta.sendAndAdopt("go")
	p.branch = "feat/y" // the agent switched branches during the run
	ta.event(event.RunFinished{Base: rootBase(), MessageID: "m1"})
	ta.returnSend() // the run settles once both Send and RunFinished land

	if p.branches <= calls {
		t.Fatalf("Branch called %d times, want a refresh after the run", p.branches)
	}
	if got := ta.app.statusState().Branch; got != "feat/y" {
		t.Errorf("status branch = %q, want feat/y after the run", got)
	}
}

func TestApp_BranchNotQueriedPerKeystroke(t *testing.T) {
	t.Parallel()
	p := &listProject{branch: "main"}
	ta := newTestApp(t, withBranch(p))
	calls := p.branches
	ta.typeText("hello")
	if p.branches != calls {
		t.Errorf("Branch called %d more times while typing, want 0", p.branches-calls)
	}
}

func TestApp_BranchSanitized(t *testing.T) {
	t.Parallel()
	p := &listProject{branch: "evil\x1b]0;t\x07"}
	ta := newTestApp(t, withBranch(p))
	if got := ta.app.statusState().Branch; strings.ContainsRune(got, '\x1b') {
		t.Errorf("unsanitized branch %q", got)
	}
}
