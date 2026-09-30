# Tool-Call Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fold runs of two or more consecutive `read`/`grep`/`glob` calls in the TUI transcript into one collapsible group line that expands inline with `o`.

**Architecture:** The transcript `Projection` is unchanged. A pure `transcript.Groups` finds the runs; a `foldState` in `internal/ui` lays blocks out as plain items, group headers (`g/<first call ID>`), and nested members; `sessionState`'s list bookkeeping moves into a new `itemTrack` that maps block changes onto items; a `foldCtl` rebuilds the blocklist when the layout changes other than by growing at its end.

**Tech Stack:** Go, bubbletea v2 (`charm.land/bubbletea/v2`), jig's `internal/bubbles/blocklist`, `internal/golden`.

**Spec:** `docs/superpowers/specs/2026-09-30-tool-call-groups-design.md` (read it before starting; section numbers below refer to it).

## Precondition

Another session had uncommitted work in this tree when this plan was written (streamed reasoning: `internal/ui/reasoning.go`, `internal/ui/prefs.go`, and changes to `render.go`, `app.go`, `actions/actions.go`, `actions/actions_test.go`, `app_bench_test.go`, `app_test.go`, `render_test.go`, `core/prefs.go`, `AGENTS.md`). Several tasks here edit those files, and `git add` of a file commits every hunk in it.

- [ ] Before Task 1, run `git status --short`. If any file other than this plan and the spec is modified or untracked, **stop and ask** your human partner to land that work first. Do not commit someone else's hunks.
- [ ] If that work changed since this plan was written, adapt the snippets that touch it (they are marked "as of writing") and keep every behavior this plan's tests pin.

## Global Constraints

- Every commit passes `make check` (build, `go test ./... -race`, the jigtest e2e, `golangci-lint` with and without `--build-tags jigtest`, `gofmt -l .` empty).
- archtest limits: a struct has ≤ 15 fields and ≤ 20 methods (across the package); a non-test source file has ≤ 500 lines. `sessionState` (15 fields, 20 methods), `App` (15 fields), `viewState` (15), and `widgets` (15) are at their field limits; `app.go` is at 495 lines.
- No package-level mutable `var`s; no `time.Now`/`time.Sleep` in `_test.go` files.
- `internal/ui/transcript` imports only the standard library and `internal/core/...`.
- `internal/ui` does no I/O; every string from a call input or result passes `ansi.SanitizeLine` (one-line) or `ansi.Sanitize` before it is rendered or copied.
- Every `internal/ui` test calls `t.Parallel()`.
- Golden files regenerate with `JIG_UPDATE_GOLDEN=1`; inspect every regenerated file before committing it.
- Budgets (spec §7), never raised: `BenchmarkApp_FoldToggle2000` < 50 ms/op; `BenchmarkApp_ToolStart2000` < 5 ms/op; existing budgets in AGENTS.md unchanged.
- Header text (spec §4.1), verbatim: live `<spinner> exploring · <tally>[ · <current call>]`; settled `▸ explored · <tally>` (`▾` when expanded) then ` · N failed ✗`, ` · N denied ⊘`, ` · N cancelled ⊘`. Tally order reads, greps, globs; `1 read`/`2 reads`, `1 grep`/`2 greps`, `1 glob`/`2 globs`, joined with `, `.
- Action: ID `transcript.fold`, title `Toggle group`, picker group `Transcript`, default key NORMAL `o`.

## Review Focus

The inputs most likely to bite a user that the spec implies but does not spell out. Each has a test in the task that owns the code.

1. **Cancelling a run mid-exploration.** The header must stop spinning and settle to `▸ explored · 2 reads · 2 cancelled ⊘`, and the group must leave the live set. Test: Task 5, `TestFold_CancelSettlesLiveGroup`.
2. **Details open on a member when its group collapses.** The details split must follow the selection to the header, not keep showing a hidden block. Test: Task 7, `TestFold_DetailsFollowCollapse`.
3. **Switching theme with groups on screen.** Headers must re-render in the new palette (their versions must bump like every other item's). Test: Task 5, `TestFold_ThemeChangeRerendersHeader`.
4. **Resuming a session with interrupted calls** (stored calls with no result load as `pending`). They must not add a problem suffix. Test: Task 4, case "interrupted (pending) calls add nothing".
5. **A parallel batch in flight.** Several members run at once; the header shows the last running one. Test: Task 4, case "parallel batch shows the last running call".

## File Structure

| File | Responsibility |
|---|---|
| `internal/ui/transcript/groups.go` (new) | `Group`, `Groups(blocks)`: the pure grouping rule (spec §3). |
| `internal/ui/items.go` (new) | `itemTrack`: item versions, dirty and live sets, last-issued nesting, the `foldState`; turns layout entries and changed block IDs into blocklist items. |
| `internal/ui/fold.go` (new) | `foldState`, `foldEntry`: groups, user-expanded set, forced expansion (search, awaiting permission), the visible layout, restructure detection. |
| `internal/ui/groups.go` (new) | `groupData`, header rendering (`renderGroup`, tally, live, current call, problem suffixes), group details and yank text. |
| `internal/ui/foldctl.go` (new) | `foldCtl{a}`: apply an event result (relist or upsert), toggle, search hook, group lookup. |
| `internal/ui/session.go` | `sessionState` uses `itemTrack`; `apply` regroups on tool/run/permission events; `items`/`allItems`/`dropUser` go through the layout. |
| `internal/ui/render.go` | `blockData` gains `Group`, `Nested`; `render` dispatches headers and indents nested members. |
| `internal/ui/app.go` | `onEvent` goes through `foldCtl.apply`; `dispatchAction` handles `transcript.fold`. |
| `internal/ui/mode_normal.go` | Details, yank, and search hooks for groups. |
| `internal/ui/themestate.go`, `internal/ui/images_test.go` | Follow the move of `versions` into `itemTrack`. |
| `internal/ui/actions/actions.go`, `keymap.go` (+ tests) | The `transcript.fold` action and its `normal.o` binding. |
| `internal/ui/fold_test.go`, `groups_test.go`, `fold_app_test.go`, `fold_bench_test.go` (new) | Unit, render, App, and benchmark tests. |
| `e2e/tui_groups_test.go` (new) | The pty end-to-end check. |
| `AGENTS.md` | Architecture notes, shared-code rows, budgets. |

---

### Task 1: Move the list bookkeeping into `itemTrack`

A pure refactor: `sessionState`'s `versions`, `dirty`, and `live` fields become one `track itemTrack` field, freeing two field slots for later tasks. No behavior changes, so the existing suite is the test.

**Files:**
- Create: `internal/ui/items.go`
- Modify: `internal/ui/session.go` (struct, `newSessionState`, `resetBlocks`, every `s.versions`/`s.dirty`/`s.live`)
- Modify: `internal/ui/themestate.go:61-63`
- Modify: `internal/ui/images_test.go:262,276`

**Interfaces:**
- Produces: `type itemTrack struct { versions map[transcript.BlockID]int; dirty idSet; live map[transcript.BlockID]bool }`, `newItemTrack() itemTrack`, `(*itemTrack).reset()`. `sessionState.track itemTrack`.

- [ ] **Step 1: Confirm the suite is green before the refactor**

Run: `go test ./internal/ui/... ./internal/archtest/ -count=1`
Expected: `ok` for every package.

- [ ] **Step 2: Create `internal/ui/items.go`**

```go
package ui

import "github.com/gammons/jig/internal/ui/transcript"

// itemTrack is the transcript list's bookkeeping, kept apart from
// sessionState (archtest's struct limits): the item version issued per
// ID (versions only ever grow, so an ID shown again never matches a stale
// cached render), the dirty streaming blocks (rendered on the next
// streamTick), and the live items, whose spinner each tick advances.
type itemTrack struct {
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
}

// newItemTrack returns an empty itemTrack.
func newItemTrack() itemTrack {
	return itemTrack{
		versions: map[transcript.BlockID]int{},
		live:     map[transcript.BlockID]bool{},
	}
}

// reset clears what a new projection invalidates; versions survive.
func (t *itemTrack) reset() {
	t.dirty = idSet{}
	t.live = map[transcript.BlockID]bool{}
}
```

- [ ] **Step 3: Use it in `internal/ui/session.go`**

In the `sessionState` doc comment, replace

```go
// the summed cost, todos, tool durations, the blocklist item versions it
// has issued, dirty streaming blocks (rendered on the next streamTick),
// and live blocks (running tools/subagents, whose spinner each tick
// advances). model is the model the last root step reported. attach
```

with

```go
// the summed cost, todos, tool durations, and the transcript list's
// bookkeeping (track). model is the model the last root step reported. attach
```

In the struct, replace

```go
	times    blockTimes
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
	cat      catalog
```

with

```go
	times    blockTimes
	track    itemTrack
	cat      catalog
```

In `newSessionState`, replace `versions: map[transcript.BlockID]int{}, cat: cat,` with `track: newItemTrack(), cat: cat,`.

In `resetBlocks`, replace

```go
	s.dirty = idSet{}
	s.live = map[transcript.BlockID]bool{}
```

with

```go
	s.track.reset()
```

Then, in `session.go` only, replace every remaining `s.dirty` with `s.track.dirty`, `s.live` with `s.track.live`, and `s.versions` with `s.track.versions` (in `apply`, `withDirty`, `endSend`, `dropUser`, `tick`, `item`, and the comment on `item`).

- [ ] **Step 4: Follow the move elsewhere**

In `internal/ui/themestate.go` (`pushTheme`), replace

```go
	for id := range a.sess.versions {
		a.sess.versions[id]++
	}
```

with

```go
	for id := range a.sess.track.versions {
		a.sess.track.versions[id]++
	}
```

In `internal/ui/images_test.go`, replace `ta.app.sess.versions` with `ta.app.sess.track.versions` on both lines (262 and 276).

- [ ] **Step 5: Run the suite and archtest**

Run: `gofmt -l internal/ui && go vet ./internal/ui/... && go test ./internal/ui/... ./internal/archtest/ -count=1`
Expected: no gofmt output; `ok` everywhere.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/items.go internal/ui/session.go internal/ui/themestate.go internal/ui/images_test.go
git commit -m "refactor(ui): move transcript list bookkeeping into itemTrack"
```

### Task 2: `transcript.Groups`

**Files:**
- Create: `internal/ui/transcript/groups.go`
- Test: `internal/ui/transcript/groups_test.go`

**Interfaces:**
- Produces: `type Group struct { ID BlockID; Members []BlockID }`; `func Groups(blocks []Block) []Group` (nil when there are none; `ID` is `"g/" + first member call ID`; `Members` in display order, calls and absorbed reasoning).

- [ ] **Step 1: Write the failing test**

Create `internal/ui/transcript/groups_test.go`:

```go
package transcript

import (
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func gTool(id, name string, st ToolState) Block {
	return Block{ID: toolBlockID(id), Kind: KindTool, State: st, Call: &core.ToolCall{ID: id, Name: name}}
}

func gBlock(id string, kind Kind) Block { return Block{ID: BlockID(id), Kind: kind} }

func TestGroups(t *testing.T) {
	ok := StateOK
	tests := []struct {
		name   string
		blocks []Block
		want   []Group
	}{
		{"empty", nil, nil},
		{"no tools", []Block{gBlock("u/u1", KindUser), gBlock("m/a/0", KindText)}, nil},
		{"a lone call is not grouped",
			[]Block{gBlock("m/a/0", KindText), gTool("c1", "read", ok), gBlock("m/a/1", KindText)}, nil},
		{"two exploration calls group",
			[]Block{gTool("c1", "read", ok), gTool("c2", "grep", ok)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "t/c2"}}}},
		{"reasoning between calls joins; leading and trailing reasoning does not",
			[]Block{gBlock("m/a/0", KindReasoning), gTool("c1", "read", ok), gBlock("m/b/0", KindReasoning),
				gTool("c2", "glob", ok), gBlock("m/c/0", KindReasoning), gBlock("m/c/1", KindText)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "m/b/0", "t/c2"}}}},
		{"the spec's example",
			[]Block{gBlock("m/a/0", KindText), gTool("c1", "read", ok), gBlock("m/b/0", KindReasoning),
				gTool("c2", "grep", ok), gBlock("m/c/0", KindReasoning), gTool("c3", "read", ok),
				gTool("c4", "edit", ok), gTool("c5", "read", ok), gBlock("m/d/0", KindText)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "m/b/0", "t/c2", "m/c/0", "t/c3"}}}},
		{"failed, denied, cancelled, awaiting, running, and pending calls are members",
			[]Block{gTool("c1", "read", StateError), gTool("c2", "grep", StateDenied), gTool("c3", "glob", StateCancelled),
				gTool("c4", "read", StateAwaiting), gTool("c5", "read", StateRunning), gTool("c6", "read", StatePending)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "t/c2", "t/c3", "t/c4", "t/c5", "t/c6"}}}},
		{"a breaker splits two groups",
			[]Block{gTool("c1", "read", ok), gTool("c2", "read", ok), gTool("c3", "bash", ok),
				gTool("c4", "grep", ok), gTool("c5", "glob", ok)},
			[]Group{{ID: "g/c1", Members: []BlockID{"t/c1", "t/c2"}}, {ID: "g/c4", Members: []BlockID{"t/c4", "t/c5"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Groups(tt.blocks); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Groups =\n %+v\nwant\n %+v", got, tt.want)
			}
		})
	}
}

func TestGroups_EveryBreakerEndsARun(t *testing.T) {
	ok := StateOK
	breakers := []Block{
		gBlock("m/x/0", KindText), gBlock("u/x", KindUser), gBlock("n/0", KindNotice),
		{ID: "t/s1", Kind: KindSubagent, Call: &core.ToolCall{ID: "s1", Name: "task"}},
		gTool("b1", "bash", ok), gTool("b2", "edit", ok), gTool("b3", "write", ok),
		gTool("b4", "todo", ok), gTool("b5", "skill", ok), gTool("b6", "mcp_search", ok),
	}
	for _, br := range breakers {
		blocks := []Block{gTool("c1", "read", ok), br, gTool("c2", "read", ok)}
		if got := Groups(blocks); got != nil {
			t.Errorf("read, %s (%v), read: Groups = %+v, want none", br.ID, br.Kind, got)
		}
	}
}

func TestGroups_IDStaysWithTheFirstCall(t *testing.T) {
	ok := StateOK
	blocks := []Block{gTool("c1", "read", ok), gTool("c2", "read", ok), gTool("c3", "grep", ok)}
	two, three := Groups(blocks[:2]), Groups(blocks)
	if len(two) != 1 || len(three) != 1 || two[0].ID != "g/c1" || three[0].ID != "g/c1" {
		t.Errorf("IDs as the group grows: %+v then %+v, want g/c1 both times", two, three)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/transcript/ -run TestGroups -count=1`
Expected: FAIL to compile with `undefined: Groups` and `undefined: Group`.

- [ ] **Step 3: Implement `internal/ui/transcript/groups.go`**

```go
package transcript

// groupPrefix starts every Group ID. It cannot collide with a block ID
// ("u/", "m/", "t/", "n/").
const groupPrefix = "g/"

// Group is a run of at least two consecutive exploration calls (read,
// grep, glob) with the reasoning blocks between them, shown as one line
// in the TUI (tool-call groups spec §3). ID is "g/" plus its first call's
// ID, so it stays the same as the group grows and across reloads.
type Group struct {
	ID      BlockID
	Members []BlockID // display order: calls and absorbed reasoning
}

// isExploration reports whether b is a read, grep, or glob call, in any
// state.
func isExploration(b *Block) bool {
	if b.Kind != KindTool || b.Call == nil {
		return false
	}
	switch b.Call.Name {
	case "read", "grep", "glob":
		return true
	}
	return false
}

// Groups returns blocks' groups in display order, or nil. A reasoning
// block joins a run only between two of its calls; reasoning before the
// first call or after the last is not a member. Any other block (text,
// user, notice, subagent, or another tool) ends the run.
func Groups(blocks []Block) []Group {
	var out []Group
	var members, pending []BlockID // pending: reasoning after the run's last call
	calls, first := 0, ""
	flush := func() {
		if calls >= 2 {
			out = append(out, Group{ID: BlockID(groupPrefix + first), Members: members})
		}
		members, pending, calls, first = nil, nil, 0, ""
	}
	for i := range blocks {
		b := &blocks[i]
		switch {
		case isExploration(b):
			if calls == 0 {
				first = b.Call.ID
			}
			members = append(append(members, pending...), b.ID)
			pending = nil
			calls++
		case b.Kind == KindReasoning:
			if calls > 0 {
				pending = append(pending, b.ID)
			}
		default:
			flush()
		}
	}
	flush()
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui/transcript/ -count=1 && go test ./internal/archtest/ -count=1`
Expected: `ok` for both (archtest confirms `transcript` still imports only stdlib and `core`).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/transcript/groups.go internal/ui/transcript/groups_test.go
git commit -m "feat(transcript): Groups finds runs of exploration calls"
```

---

### Task 3: `foldState` and the layout

**Files:**
- Create: `internal/ui/fold.go`
- Modify: `internal/ui/items.go` (add the `fold` field; `reset` clears it)
- Test: `internal/ui/fold_test.go`

**Interfaces:**
- Consumes: `transcript.Groups`, `transcript.Group` (Task 2).
- Produces:
  - `type entryKind int` with `entryPlain`, `entryHeader`, `entryNested`.
  - `type foldEntry struct { kind entryKind; id transcript.BlockID; group int }` (`group` indexes `foldState.groups` for a header or nested member, `-1` for a plain block; comparable).
  - `type foldState struct { groups []transcript.Group; owner, byID map[transcript.BlockID]int; open map[transcript.BlockID]bool; search bool; order []foldEntry }`.
  - `(*foldState).regroup(blocks []transcript.Block) ([]foldEntry, bool)`: recomputes groups and the layout, stores it in `order`, and reports whether it changed other than by new entries at its end.
  - `(*foldState).layout(blocks []transcript.Block) []foldEntry`, `(*foldState).expanded(gi int) bool`, `(*foldState).toggle(id transcript.BlockID) (transcript.BlockID, bool)`, `(*foldState).groupOf(id transcript.BlockID) (transcript.BlockID, bool)`, `(*foldState).members(id transcript.BlockID) ([]transcript.BlockID, bool)`.
  - `itemTrack.fold foldState`.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/fold_test.go`:

```go
package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

func foldTool(id, name string, st transcript.ToolState) transcript.Block {
	return transcript.Block{ID: transcript.BlockID("t/" + id), Kind: transcript.KindTool, State: st, Call: &core.ToolCall{ID: id, Name: name}}
}

// foldBlocks: a user block, a read, reasoning, a grep, and a reply. The
// read, reasoning, and grep form group g/c1.
func foldBlocks() []transcript.Block {
	return []transcript.Block{
		{ID: "u/u1", Kind: transcript.KindUser},
		foldTool("c1", "read", transcript.StateOK),
		{ID: "m/a2/0", Kind: transcript.KindReasoning},
		foldTool("c2", "grep", transcript.StateOK),
		{ID: "m/a3/0", Kind: transcript.KindText},
	}
}

// entryNames renders entries as "<kind> <id>" for readable comparisons.
func entryNames(es []foldEntry) []string {
	kinds := map[entryKind]string{entryPlain: "plain", entryHeader: "header", entryNested: "nested"}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = fmt.Sprintf("%s %s", kinds[e.kind], e.id)
	}
	return out
}

func wantEntries(t *testing.T, got []foldEntry, want ...string) {
	t.Helper()
	if names := entryNames(got); !slices.Equal(names, want) {
		t.Errorf("layout = %q\nwant     %q", names, want)
	}
}

func TestFold_CollapsedByDefault(t *testing.T) {
	t.Parallel()
	var f foldState
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_ToggleHeaderExpandsAndCollapses(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	if g, ok := f.toggle("g/c1"); !ok || g != "g/c1" {
		t.Fatalf("toggle(g/c1) = %q, %v, want g/c1, true", g, ok)
	}
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "nested t/c1", "nested m/a2/0", "nested t/c2", "plain m/a3/0")
	f.toggle("g/c1")
	got, _ = f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_ToggleOnAMemberCollapsesItsGroup(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	f.toggle("g/c1")
	f.regroup(foldBlocks())
	if g, ok := f.toggle("t/c2"); !ok || g != "g/c1" {
		t.Fatalf("toggle(t/c2) = %q, %v, want g/c1, true", g, ok)
	}
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_ToggleOutsideAGroupDoesNothing(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	for _, id := range []transcript.BlockID{"m/a3/0", "u/u1", "t/nope"} {
		if g, ok := f.toggle(id); ok {
			t.Errorf("toggle(%s) = %q, true, want false", id, g)
		}
	}
}

func TestFold_SearchForcesOpenAndRestores(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	f.search = true
	got, _ := f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "nested t/c1", "nested m/a2/0", "nested t/c2", "plain m/a3/0")
	f.search = false
	got, _ = f.regroup(foldBlocks())
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
}

func TestFold_RegroupReportsRestructure(t *testing.T) {
	t.Parallel()
	all := foldBlocks()
	var f foldState
	steps := []struct {
		n    int
		want bool
		why  string
	}{
		{2, false, "first layout"},
		{3, false, "reasoning appended after a lone read"},
		{4, true, "the grep forms a group: t/c1 and the reasoning leave the list"},
		{5, false, "a reply appended after the group"},
	}
	for _, s := range steps {
		if _, got := f.regroup(all[:s.n]); got != s.want {
			t.Errorf("%s: regroup = %v, want %v", s.why, got, s.want)
		}
	}
}

func TestFold_AbsorbedReasoningInAnOpenGroupRestructures(t *testing.T) {
	t.Parallel()
	blocks := []transcript.Block{
		foldTool("c1", "read", transcript.StateOK),
		foldTool("c2", "read", transcript.StateOK),
		{ID: "m/x/0", Kind: transcript.KindReasoning},
		foldTool("c3", "read", transcript.StateOK),
	}
	var f foldState
	f.regroup(blocks[:2])
	f.toggle("g/c1")
	if _, got := f.regroup(blocks[:3]); got {
		t.Error("reasoning after an open group: regroup = true, want false (it is appended as a plain item)")
	}
	if _, got := f.regroup(blocks); !got {
		t.Error("the next call absorbs the reasoning (plain → nested): regroup = false, want true")
	}
}

func TestFold_GroupOfAndMembers(t *testing.T) {
	t.Parallel()
	var f foldState
	f.regroup(foldBlocks())
	if g, ok := f.groupOf("m/a2/0"); !ok || g != "g/c1" {
		t.Errorf("groupOf(m/a2/0) = %q, %v, want g/c1", g, ok)
	}
	if _, ok := f.groupOf("g/c1"); ok {
		t.Error("groupOf(a header) = true, want false")
	}
	if m, ok := f.members("g/c1"); !ok || !slices.Equal(m, []transcript.BlockID{"t/c1", "m/a2/0", "t/c2"}) {
		t.Errorf("members(g/c1) = %v, %v", m, ok)
	}
	if _, ok := f.members("t/c1"); ok {
		t.Error("members(a member) = true, want false")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestFold_ -count=1`
Expected: FAIL to compile (`undefined: foldState`, `foldEntry`, `entryKind`, ...).

- [ ] **Step 3: Implement `internal/ui/fold.go`**

```go
package ui

import (
	"slices"

	"github.com/gammons/jig/internal/ui/transcript"
)

// entryKind says how the transcript list shows a block or group.
type entryKind int

const (
	entryPlain  entryKind = iota // a block outside any group
	entryHeader                  // a group's one-line header
	entryNested                  // a member of an expanded group, indented
)

// foldEntry is one item of the transcript list's layout: a plain block,
// a group header (id is the group's ID), or a nested member. group
// indexes foldState.groups for a header or member, and is -1 otherwise.
type foldEntry struct {
	kind  entryKind
	id    transcript.BlockID
	group int
}

// foldState is the root transcript's tool-call groups (tool-call groups
// spec §6.1): the groups, which group each member is in (owner) and each
// group's index (byID), the groups the user expanded (open), whether a
// search forces every group open, and the layout last computed (order),
// so regroup can tell an append from a restructure.
type foldState struct {
	groups []transcript.Group
	owner  map[transcript.BlockID]int
	byID   map[transcript.BlockID]int
	open   map[transcript.BlockID]bool
	search bool
	order  []foldEntry
}

// regroup recomputes the groups and the layout of blocks, keeps the
// layout as order, and reports whether it changed other than by new
// entries at its end (the list must then be rebuilt, not upserted).
func (f *foldState) regroup(blocks []transcript.Block) ([]foldEntry, bool) {
	f.groups = transcript.Groups(blocks)
	f.owner = make(map[transcript.BlockID]int)
	f.byID = make(map[transcript.BlockID]int, len(f.groups))
	for gi, g := range f.groups {
		f.byID[g.ID] = gi
		for _, id := range g.Members {
			f.owner[id] = gi
		}
	}
	entries := f.layout(blocks)
	appended := len(entries) >= len(f.order) && slices.Equal(entries[:len(f.order)], f.order)
	f.order = entries
	return entries, !appended
}

// layout lays blocks out: a member's group shows as its header (at its
// first call) and, while expanded, the members nested under it.
func (f *foldState) layout(blocks []transcript.Block) []foldEntry {
	out := make([]foldEntry, 0, len(blocks))
	for _, b := range blocks {
		gi, member := f.owner[b.ID]
		if !member {
			out = append(out, foldEntry{kind: entryPlain, id: b.ID, group: -1})
			continue
		}
		g := f.groups[gi]
		if b.ID == g.Members[0] {
			out = append(out, foldEntry{kind: entryHeader, id: g.ID, group: gi})
		}
		if f.expanded(gi) {
			out = append(out, foldEntry{kind: entryNested, id: b.ID, group: gi})
		}
	}
	return out
}

// expanded reports whether group gi shows its members: the user opened
// it, or a search is applied.
func (f *foldState) expanded(gi int) bool {
	return f.open[f.groups[gi].ID] || f.search
}

// toggle flips the user's state of the group id heads, or collapses the
// group id is a member of, and returns that group's ID. It reports false
// for any other block. A group held open (a search) keeps showing until
// the hold ends; the flipped state applies then.
func (f *foldState) toggle(id transcript.BlockID) (transcript.BlockID, bool) {
	if f.open == nil {
		f.open = map[transcript.BlockID]bool{}
	}
	if _, ok := f.byID[id]; ok {
		f.open[id] = !f.open[id]
		return id, true
	}
	gi, ok := f.owner[id]
	if !ok {
		return "", false
	}
	g := f.groups[gi].ID
	f.open[g] = false
	return g, true
}

// groupOf returns the ID of the group member id belongs to.
func (f *foldState) groupOf(id transcript.BlockID) (transcript.BlockID, bool) {
	gi, ok := f.owner[id]
	if !ok {
		return "", false
	}
	return f.groups[gi].ID, true
}

// members returns group id's member IDs, in display order.
func (f *foldState) members(id transcript.BlockID) ([]transcript.BlockID, bool) {
	gi, ok := f.byID[id]
	if !ok {
		return nil, false
	}
	return f.groups[gi].Members, true
}
```

- [ ] **Step 4: Add the fold to `itemTrack`**

In `internal/ui/items.go`, add the field (and update the doc comment's list to end with "…whose spinner each tick advances, and the tool-call groups (fold).") :

```go
type itemTrack struct {
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
	fold     foldState
}
```

and make `reset` clear it:

```go
func (t *itemTrack) reset() {
	t.dirty = idSet{}
	t.live = map[transcript.BlockID]bool{}
	t.fold = foldState{}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run TestFold_ -count=1 && go vet ./internal/ui/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/fold.go internal/ui/fold_test.go internal/ui/items.go
git commit -m "feat(ui): foldState lays tool-call groups out as headers and members"
```

### Task 4: Render the header and nested members

**Files:**
- Create: `internal/ui/groups.go`
- Modify: `internal/ui/render.go` (`blockData`, `render`; as of writing, `render` dispatches reasoning to `renderReasoningBlock(data, width)`)
- Test: `internal/ui/groups_test.go`
- Golden: `internal/ui/testdata/golden/render_groups.ansi` (generated)

**Interfaces:**
- Produces:
  - `type groupData struct { Members []transcript.Block; Durs []time.Duration; Open bool }`.
  - `blockData.Group *groupData` (set only on header items), `blockData.Nested bool`.
  - `const nestIndent = 2`.
  - `(*renderer).renderGroup(g *groupData, frame int) string`, `(*renderer).groupProblems(members []transcript.Block) string`.
  - `groupTally(members []transcript.Block) string`, `groupLive(members []transcript.Block) bool`, `currentCall(members []transcript.Block) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/groups_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/transcript"
)

// gm is group member call id of tool name with input in state st; an ok
// call gets a one-line result (a read's "1: package a", a search's one
// match).
func gm(id, name, input string, st transcript.ToolState) transcript.Block {
	b := transcript.Block{ID: transcript.BlockID("t/" + id), Kind: transcript.KindTool, State: st, Call: toolCall(id, name, input)}
	if st == transcript.StateOK {
		out := "a.go:1: match"
		if name == "read" {
			out = "1: package a"
		}
		b.Result = toolResult(id, name, out, false)
	}
	return b
}

// headerOf is the blockData of group g1's header showing g.
func headerOf(g groupData, frame int) blockData {
	return blockData{Block: transcript.Block{ID: "g/c1"}, Group: &g, Frame: frame}
}

func TestRender_GroupHeader(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok, run := transcript.StateOK, transcript.StateRunning
	readA := gm("c1", "read", `{"path":"a.go"}`, ok)
	readB := gm("c2", "read", `{"path":"b.go"}`, ok)
	thinking := transcript.Block{ID: "m/x/0", Kind: transcript.KindReasoning, Thinking: true}
	tests := []struct {
		name string
		g    groupData
		want string
	}{
		{"live, collapsed, shows the running call",
			groupData{Members: []transcript.Block{readA, gm("c3", "grep", `{"pattern":"TODO"}`, run)}},
			`⠋ exploring · 1 read, 1 grep · grep "TODO"`},
		{"live, expanded: no current call",
			groupData{Members: []transcript.Block{readA, gm("c3", "grep", `{"pattern":"TODO"}`, run)}, Open: true},
			`⠋ exploring · 1 read, 1 grep`},
		{"live with nothing running: no current call",
			groupData{Members: []transcript.Block{readA, readB, thinking}},
			`⠋ exploring · 2 reads`},
		{"parallel batch shows the last running call",
			groupData{Members: []transcript.Block{gm("c1", "read", `{"path":"a.go"}`, run), gm("c2", "read", `{"path":"b.go"}`, run)}},
			`⠋ exploring · 2 reads · read b.go`},
		{"awaiting permission counts as live",
			groupData{Members: []transcript.Block{readA, gm("c2", "read", `{"path":"/etc/hosts"}`, transcript.StateAwaiting)}, Open: true},
			`⠋ exploring · 2 reads`},
		{"settled, collapsed",
			groupData{Members: []transcript.Block{readA, readB, gm("c3", "grep", `{"pattern":"x"}`, ok), gm("c4", "glob", `{"pattern":"*.go"}`, ok)}},
			`▸ explored · 2 reads, 1 grep, 1 glob`},
		{"settled, expanded",
			groupData{Members: []transcript.Block{readA, readB}, Open: true},
			`▾ explored · 2 reads`},
		{"problems follow the tally",
			groupData{Members: []transcript.Block{
				gm("c1", "read", `{"path":"a.go"}`, transcript.StateError), gm("c2", "grep", `{"pattern":"x"}`, transcript.StateDenied),
				gm("c3", "glob", `{"pattern":"*"}`, transcript.StateCancelled), readB}},
			`▸ explored · 2 reads, 1 grep, 1 glob · 1 failed ✗ · 1 denied ⊘ · 1 cancelled ⊘`},
		{"interrupted (pending) calls add nothing",
			groupData{Members: []transcript.Block{gm("c1", "read", `{"path":"a.go"}`, transcript.StatePending), readB}},
			`▸ explored · 2 reads`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := xansi.Strip(renderOne(t, r, headerOf(tt.g, 0), 120)); got != tt.want {
				t.Errorf("header = %q\nwant     %q", got, tt.want)
			}
		})
	}
}

func TestRender_GroupHeaderStyles(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	rs := set.Render
	ok := transcript.StateOK
	readA, readB := gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "read", `{"path":"b.go"}`, ok)

	if got, want := renderOne(t, r, headerOf(groupData{Members: []transcript.Block{readA, readB}}, 0), 120),
		rs.OK.Render("▸ explored · 2 reads"); got != want {
		t.Errorf("settled header = %q, want the OK style %q", got, want)
	}
	failed := gm("c1", "read", `{"path":"a.go"}`, transcript.StateError)
	if got, want := renderOne(t, r, headerOf(groupData{Members: []transcript.Block{failed, readB}}, 0), 120),
		rs.OK.Render("▸ explored · 2 reads")+rs.Error.Render(" · 1 failed ✗"); got != want {
		t.Errorf("header with a failure = %q, want %q", got, want)
	}
	running := gm("c2", "read", `{"path":"b.go"}`, transcript.StateRunning)
	if got, want := renderOne(t, r, headerOf(groupData{Members: []transcript.Block{readA, running}}, 0), 120),
		rs.Tool.Render("⠋ exploring · 2 reads · read b.go"); got != want {
		t.Errorf("live header = %q, want the Tool style %q", got, want)
	}
}

func TestRender_GroupHeaderFitsAndSanitizes(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok := transcript.StateOK
	wide := headerOf(groupData{Members: []transcript.Block{gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "grep", `{"pattern":"x"}`, ok)}}, 0)
	out := renderOne(t, r, wide, 24)
	if w := ansi.Width(out); w > 24-rightPad || !strings.HasSuffix(xansi.Strip(out), "…") {
		t.Errorf("header at width 24 = %q (width %d), want ≤ %d cells ending in …", xansi.Strip(out), w, 24-rightPad)
	}
	hostile := headerOf(groupData{Members: []transcript.Block{
		gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "read", `{"path":"a\u001b[2Jb.go"}`, transcript.StateRunning)}}, 0)
	out = renderOne(t, r, hostile, 120)
	if strings.Contains(out, "\x1b[2J") || !strings.Contains(xansi.Strip(out), "read ab.go") {
		t.Errorf("header = %q, want the escape removed and \"read ab.go\" kept", out)
	}
}

func TestRender_NestedMemberIsIndented(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok := transcript.StateOK
	out := renderOne(t, r, blockData{Block: gm("c1", "read", `{"path":"a.go"}`, ok), Nested: true}, 80)
	if got := xansi.Strip(out); got != "  ▸ read  a.go · 1 lines" {
		t.Errorf("nested member = %q, want %q", got, "  ▸ read  a.go · 1 lines")
	}
	long := blockData{Block: gm("c2", "read", `{"path":"`+strings.Repeat("x", 200)+`"}`, ok), Nested: true}
	for _, line := range strings.Split(renderOne(t, r, long, 40), "\n") {
		if ansi.Width(line) > 40-rightPad || !strings.HasPrefix(xansi.Strip(line), "  ") {
			t.Errorf("nested line %q: want indented and ≤ %d cells", xansi.Strip(line), 40-rightPad)
		}
	}
	carded := blockData{Block: gm("c3", "read", `{"path":"a.go"}`, transcript.StateAwaiting), Nested: true, Card: "CARD"}
	lines := strings.Split(renderOne(t, r, carded, 80), "\n")
	if lines[len(lines)-1] != "CARD" {
		t.Errorf("nested member's card line = %q, want it unindented", lines[len(lines)-1])
	}
}

func TestRender_GoldenGroups(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	ok := transcript.StateOK
	readA, readB := gm("c1", "read", `{"path":"a.go"}`, ok), gm("c2", "read", `{"path":"b.go"}`, ok)
	items := []blockData{
		headerOf(groupData{Members: []transcript.Block{readA, gm("c3", "grep", `{"pattern":"TODO"}`, transcript.StateRunning)}}, 3),
		headerOf(groupData{Members: []transcript.Block{readA, readB, gm("c3", "grep", `{"pattern":"TODO"}`, ok)}}, 0),
		headerOf(groupData{Members: []transcript.Block{
			gm("c1", "read", `{"path":"a.go"}`, transcript.StateError), gm("c2", "grep", `{"pattern":"x"}`, transcript.StateDenied),
			gm("c3", "glob", `{"pattern":"*"}`, transcript.StateCancelled), readB}}, 0),
		headerOf(groupData{Members: []transcript.Block{readA, readB}, Open: true}, 0),
		{Block: readA, Nested: true},
		{Block: transcript.Block{ID: "m/x/0", Kind: transcript.KindReasoning}, Nested: true},
		{Block: readB, Nested: true},
	}
	lines := make([]string, 0, len(items))
	for _, it := range items {
		lines = append(lines, renderOne(t, r, it, 80))
	}
	golden.Assert(t, "render_groups", strings.Join(lines, "\n"))
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/ -run 'TestRender_(Group|Nested|GoldenGroups)' -count=1`
Expected: FAIL to compile (`undefined: groupData`, unknown field `Group`/`Nested` in `blockData`).

- [ ] **Step 3: Implement `internal/ui/groups.go`**

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/ui/transcript"
)

// nestIndent is the blank columns before each line of a member of an
// expanded group, setting it off under its header.
const nestIndent = 2

// groupData is a group header's render input (tool-call groups spec §4):
// its members (calls and absorbed reasoning, display order), their
// durations, and whether the group is shown expanded.
type groupData struct {
	Members []transcript.Block
	Durs    []time.Duration
	Open    bool
}

// renderGroup renders a group header: "<spinner> exploring · <tally>"
// while live, followed by " · <current call>" when collapsed; otherwise
// "▸ explored · <tally>" (▾ when expanded) followed by its failed,
// denied, and cancelled counts, each in its state's style.
func (r *renderer) renderGroup(g *groupData, frame int) string {
	rs := r.set.Render
	tally := groupTally(g.Members)
	if groupLive(g.Members) {
		line := string(spinnerGlyph(frame)) + " exploring · " + tally
		if cur := currentCall(g.Members); cur != "" && !g.Open {
			line += " · " + cur
		}
		return rs.Tool.Render(line)
	}
	icon := "▸"
	if g.Open {
		icon = "▾"
	}
	return rs.OK.Render(icon+" explored · "+tally) + r.groupProblems(g.Members)
}

// groupProblems renders the settled header's " · N failed ✗",
// " · N denied ⊘", and " · N cancelled ⊘" suffixes, each only when N > 0.
// A pending call (loaded, unanswered) adds nothing.
func (r *renderer) groupProblems(members []transcript.Block) string {
	var failed, denied, cancelled int
	for _, m := range members {
		switch m.State {
		case transcript.StateError:
			failed++
		case transcript.StateDenied:
			denied++
		case transcript.StateCancelled:
			cancelled++
		}
	}
	rs := r.set.Render
	var out string
	if failed > 0 {
		out += rs.Error.Render(fmt.Sprintf(" · %d failed ✗", failed))
	}
	if denied > 0 {
		out += rs.Denied.Render(fmt.Sprintf(" · %d denied ⊘", denied))
	}
	if cancelled > 0 {
		out += rs.Dim.Render(fmt.Sprintf(" · %d cancelled ⊘", cancelled))
	}
	return out
}

// groupTally counts members' calls as "N reads, N greps, N globs", in
// that order, singular for one, leaving out a tool with no calls.
func groupTally(members []transcript.Block) string {
	counts := map[string]int{}
	for _, m := range members {
		if m.Kind == transcript.KindTool && m.Call != nil {
			counts[m.Call.Name]++
		}
	}
	kinds := [...][3]string{{"read", "read", "reads"}, {"grep", "grep", "greps"}, {"glob", "glob", "globs"}}
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		switch n := counts[k[0]]; {
		case n == 1:
			parts = append(parts, "1 "+k[1])
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, k[2]))
		}
	}
	return strings.Join(parts, ", ")
}

// groupLive reports whether a group is still working: a member call is
// running or awaiting permission, or a member is still thinking.
func groupLive(members []transcript.Block) bool {
	for _, m := range members {
		if m.Thinking || m.State == transcript.StateRunning || m.State == transcript.StateAwaiting {
			return true
		}
	}
	return false
}

// currentCall is "<tool> <summary>" for the last running member call in
// display order (several run at once in a parallel batch), or "".
func currentCall(members []transcript.Block) string {
	for i := len(members) - 1; i >= 0; i-- {
		m := members[i]
		if m.Kind == transcript.KindTool && m.Call != nil && m.State == transcript.StateRunning {
			_, name, summary, _ := toolLine(m, 0)
			return ansi.SanitizeLine(name + " " + summary)
		}
	}
	return ""
}
```

- [ ] **Step 4: Wire it into `internal/ui/render.go`**

Add two fields to `blockData`, after `Thinking`:

```go
	Thinking bool       // a reasoning block the model is still thinking in (Projection.Thinking)
	Group    *groupData // set only on a group header's item
	Nested   bool       // a member of an expanded group, indented under its header
```

In `render`, right after `width = max(width-rightPad, 1)`, insert:

```go
	if data.Group != nil {
		return []string{r.fitLine(r.renderGroup(data.Group, data.Frame), width)}
	}
	if data.Nested {
		width = max(width-nestIndent, 1)
	}
```

and right before `if data.Card != "" {`, insert:

```go
	if data.Nested {
		pad := strings.Repeat(" ", nestIndent)
		for i := range lines {
			lines[i] = pad + lines[i]
		}
	}
```

Update `render`'s doc comment: add "A group header renders as its one line (renderGroup); a nested member is laid out nestIndent columns narrower and indented, its permission card (if any) not."

- [ ] **Step 5: Generate the golden and run the tests**

Run: `JIG_UPDATE_GOLDEN=1 go test ./internal/ui/ -run TestRender_GoldenGroups -count=1`
Then inspect it: `cat internal/ui/testdata/golden/render_groups.ansi` in a terminal. Expect seven lines: a live header (spinner frame 3, `exploring · 1 read, 1 grep · grep "TODO"`), `▸ explored · 2 reads, 1 grep`, the problems header in OK/red/red/dim segments, `▾ explored · 2 reads`, then three lines indented two columns (`▸ read  a.go · 1 lines`, `∴ thinking`, `▸ read  b.go · 1 lines`).

Run: `go test ./internal/ui/ -count=1`
Expected: `ok` (all existing render and App tests too).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/groups.go internal/ui/groups_test.go internal/ui/render.go internal/ui/testdata/golden/render_groups.ansi
git commit -m "feat(ui): render tool-call group headers and nested members"
```

### Task 5: Groups in the App's transcript list

`sessionState` now builds items through the layout, `apply` regroups on tool and run events, and the App rebuilds the list when the layout changes other than by growing at its end.

**Files:**
- Modify: `internal/ui/items.go` (full content below)
- Modify: `internal/ui/session.go` (`applyResult`, `apply`, `items`, `allItems`, `dropUser`; delete `item`; add `regroupsOn`)
- Create: `internal/ui/foldctl.go`
- Modify: `internal/ui/app.go` (`onEvent`; as of writing, lines 239–244)
- Test: `internal/ui/fold_app_test.go`

**Interfaces:**
- Consumes: `foldState`, `foldEntry` (Task 3); `groupData`, `groupLive` (Task 4); `sessionState.data(b)`, `isLive(b)` (existing).
- Produces:
  - `itemTrack.nested map[transcript.BlockID]bool`; methods `regroup(p *transcript.Projection) bool`, `visible(ids []transcript.BlockID) []foldEntry`, `layoutItems(s *sessionState, entries []foldEntry, bump func(foldEntry) bool) []blocklist.Item`, `relistItems(s *sessionState, changed []transcript.BlockID) []blocklist.Item`, `build(...)`, `entryItem(...)`, `entryData(...)`; funcs `bumpAll`, `bumpNone`, `groupMembers(s *sessionState, ids []transcript.BlockID) ([]transcript.Block, []time.Duration)`.
  - `applyResult.relist bool`; `regroupsOn(ev event.Event) bool`.
  - `type foldCtl struct{ a *App }` with `apply(res applyResult) tea.Cmd` and `relist(changed []transcript.BlockID) tea.Cmd`.
  - Test helpers (in `fold_app_test.go`): `groupMessages() []core.Message`, `(*testApp).listIDs() []string`, `(*testApp).startTool(msg core.MessageID, id, name, input string)`, `(*testApp).finishTool(msg core.MessageID, id, name, out string, isErr bool)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/fold_app_test.go`:

```go
package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// groupMessages is a stored session whose exploration folds into one
// group: a read, then (next step) reasoning and a grep, then the reply.
// Its blocks are u/u1, t/c1, m/a2/0, t/c2, m/a3/0; the group is g/c1.
func groupMessages() []core.Message {
	msg := func(id string, role core.Role, parts ...core.Part) core.Message {
		return core.Message{ID: core.MessageID(id), SessionID: "ses_1", Role: role, Status: core.StatusComplete, Parts: parts}
	}
	call := func(id, name, input string) core.Part {
		return core.Part{Kind: core.PartToolCall, Call: &core.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}}
	}
	result := func(id, name, out string) core.Part {
		return core.Part{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: id, Name: name, Output: out}}
	}
	return []core.Message{
		msg("u1", core.RoleUser, core.Part{Kind: core.PartText, Text: "look around"}),
		msg("a1", core.RoleAssistant, call("c1", "read", `{"path":"a.go"}`), result("c1", "read", "1: package a")),
		msg("a2", core.RoleAssistant, core.Part{Kind: core.PartReasoning, Text: "now search"},
			call("c2", "grep", `{"pattern":"TODO"}`), result("c2", "grep", "b.go:3: // TODO")),
		msg("a3", core.RoleAssistant, core.Part{Kind: core.PartText, Text: "Found one TODO in b.go."}),
	}
}

// resumeGroups resumes ses_1 with groupMessages.
func resumeGroups() testOpt {
	return withResume(core.Session{ID: "ses_1", Agent: "build"}, groupMessages(), nil)
}

// listIDs returns the transcript list's item IDs in order, walking a copy
// of the list, so the App's own selection is untouched.
func (ta *testApp) listIDs() []string {
	l := ta.app.w.list
	l.Top()
	ids := make([]string, 0, l.Len())
	for range l.Len() {
		it, _ := l.Selected()
		ids = append(ids, it.ID)
		l, _ = l.Update(keyPress("j"))
	}
	return ids
}

// startTool delivers a root ToolCallStarted for call id of tool name.
func (ta *testApp) startTool(msg core.MessageID, id, name, input string) {
	ta.t.Helper()
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: msg,
		Call: core.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}})
}

// finishTool delivers a root ToolCallFinished for call id.
func (ta *testApp) finishTool(msg core.MessageID, id, name, out string, isErr bool) {
	ta.t.Helper()
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: msg,
		Result: core.ToolResult{CallID: id, Name: name, Output: out, IsError: isErr}})
}

func TestFold_SecondExplorationCallFormsGroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"t/c1"}) {
		t.Fatalf("after one read, list = %v, want [<user> t/c1]", got)
	}
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		t.Fatalf("after two reads, list = %v, want [<user> g/c1]", got)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the following view on g/c1", got)
	}
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "exploring · 2 reads · read b.go") {
		t.Errorf("no live header in the view:\n%s", view)
	}
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.finishTool("m1", "c2", "read", "1: package b", false)
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "▸ explored · 2 reads") {
		t.Errorf("no settled header in the view:\n%s", view)
	}
}

func TestFold_HiddenMembersAreNotLive(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	live := ta.app.sess.track.live
	if !live["g/c1"] || live["t/c1"] || live["t/c2"] {
		t.Errorf("live = %v, want the header g/c1 live and its hidden members not", live)
	}
}

func TestFold_LoneCallStaysPlain(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "done"})
	ta.fire()
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"t/c1", "m/m1/0"}) {
		t.Errorf("list = %v, want [<user> t/c1 m/m1/0]", got)
	}
}

func TestFold_StoredSessionLoadsCollapsed(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "▸ explored · 1 read, 1 grep") {
		t.Errorf("no collapsed header in the view:\n%s", view)
	}
}

func TestFold_CancelSettlesLiveGroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	ta.event(event.RunFailed{Base: rootBase(), Err: "cancelled"})
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "▸ explored · 2 reads · 2 cancelled ⊘") {
		t.Errorf("no cancelled header in the view:\n%s", view)
	}
	if ta.app.sess.track.live["g/c1"] {
		t.Error("the cancelled group is still live (its spinner would keep ticking)")
	}
}

func TestFold_ThemeChangeRerendersHeader(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	before := ta.app.sess.track.versions["g/c1"]
	if before == 0 {
		t.Fatal("the header g/c1 was never issued")
	}
	pushTheme(ta.app)
	if got := ta.app.sess.track.versions["g/c1"]; got <= before {
		t.Errorf("header version after a theme change = %d, want > %d", got, before)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/ -run TestFold_ -count=1`
Expected: FAIL. `TestFold_SecondExplorationCallFormsGroup` reports a list of `[<user> t/c1 t/c2]`, `TestFold_StoredSessionLoadsCollapsed` a list with every block, and `TestFold_ThemeChangeRerendersHeader` "the header g/c1 was never issued". The Task 3 tests still pass.

- [ ] **Step 3: Replace `internal/ui/items.go`**

```go
package ui

import (
	"time"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/ui/transcript"
)

// itemTrack is the transcript list's bookkeeping, kept apart from
// sessionState (archtest's struct limits): the item version issued per
// ID (versions only ever grow, so an ID shown again never matches a stale
// cached render), the dirty streaming blocks (rendered on the next
// streamTick), the live items, whose spinner each tick advances, each
// block's nesting as last issued, and the tool-call groups (fold).
type itemTrack struct {
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
	nested   map[transcript.BlockID]bool
	fold     foldState
}

// newItemTrack returns an empty itemTrack.
func newItemTrack() itemTrack {
	return itemTrack{
		versions: map[transcript.BlockID]int{},
		live:     map[transcript.BlockID]bool{},
		nested:   map[transcript.BlockID]bool{},
	}
}

// reset clears what a new projection invalidates; versions survive.
func (t *itemTrack) reset() {
	t.dirty = idSet{}
	t.live = map[transcript.BlockID]bool{}
	t.fold = foldState{}
}

// bumpAll and bumpNone are build's bump rules: re-render every item, or
// only the ones build must (never issued, or re-nested).
func bumpAll(foldEntry) bool  { return true }
func bumpNone(foldEntry) bool { return false }

// regroup re-lays out p's blocks and reports whether the list must be
// rebuilt: its layout changed other than by new items at its end.
func (t *itemTrack) regroup(p *transcript.Projection) bool {
	_, changed := t.fold.regroup(p.Blocks())
	return changed
}

// visible maps changed block IDs to the entries of the items that show
// them, each once, in first-seen order: a block outside any group is
// itself; a member of an expanded group is itself and its group's header
// (the tally may have changed); a member of a collapsed group is only its
// header, and stops being live itself (the header animates for it); a
// group ID (a live header, from the tick) is its header.
func (t *itemTrack) visible(ids []transcript.BlockID) []foldEntry {
	f := &t.fold
	seen := make(map[transcript.BlockID]bool, len(ids))
	out := make([]foldEntry, 0, len(ids))
	add := func(e foldEntry) {
		if !seen[e.id] {
			seen[e.id] = true
			out = append(out, e)
		}
	}
	for _, id := range ids {
		if gi, ok := f.byID[id]; ok {
			add(foldEntry{kind: entryHeader, id: id, group: gi})
			continue
		}
		gi, ok := f.owner[id]
		if !ok {
			add(foldEntry{kind: entryPlain, id: id, group: -1})
			continue
		}
		if f.expanded(gi) {
			add(foldEntry{kind: entryNested, id: id, group: gi})
		} else {
			delete(t.live, id)
		}
		add(foldEntry{kind: entryHeader, id: f.groups[gi].ID, group: gi})
	}
	return out
}

// layoutItems builds the whole list from entries (a full layout), so the
// live set is rebuilt from scratch: a member hidden since it was last
// built is no longer live.
func (t *itemTrack) layoutItems(s *sessionState, entries []foldEntry, bump func(foldEntry) bool) []blocklist.Item {
	t.live = map[transcript.BlockID]bool{}
	return t.build(s, entries, bump)
}

// relistItems builds every item of the current layout (fold.order),
// re-rendering the group headers (a header's open state or tally may
// have changed) and the items showing changed; every other item keeps
// its version, so the list re-renders only those.
func (t *itemTrack) relistItems(s *sessionState, changed []transcript.BlockID) []blocklist.Item {
	bumped := map[transcript.BlockID]bool{}
	for _, e := range t.visible(changed) {
		bumped[e.id] = true
	}
	return t.layoutItems(s, t.fold.order, func(e foldEntry) bool {
		return e.kind == entryHeader || bumped[e.id]
	})
}

// build returns an item for each entry whose block still exists.
func (t *itemTrack) build(s *sessionState, entries []foldEntry, bump func(foldEntry) bool) []blocklist.Item {
	out := make([]blocklist.Item, 0, len(entries))
	for _, e := range entries {
		if it, ok := t.entryItem(s, e, bump(e)); ok {
			out = append(out, it)
		}
	}
	return out
}

// entryItem builds e's item. Its version goes up when bump is set, when
// it was never issued, or when its nesting changed since it was last
// issued (the cached render would keep the old indent). It records
// whether the item is live.
func (t *itemTrack) entryItem(s *sessionState, e foldEntry, bump bool) (blocklist.Item, bool) {
	data, live, ok := t.entryData(s, e)
	if !ok {
		return blocklist.Item{}, false
	}
	nested := e.kind == entryNested
	if bump || t.versions[e.id] == 0 || t.nested[e.id] != nested {
		t.versions[e.id]++
	}
	t.nested[e.id] = nested
	if live {
		t.live[e.id] = true
	} else {
		delete(t.live, e.id)
	}
	return blocklist.Item{ID: string(e.id), Version: t.versions[e.id], Data: data}, true
}

// entryData is e's blockData and whether it animates: a header's members
// and open state, or a block's data (nested when e is a member).
func (t *itemTrack) entryData(s *sessionState, e foldEntry) (blockData, bool, bool) {
	if e.kind == entryHeader {
		g := t.fold.groups[e.group]
		members, durs := groupMembers(s, g.Members)
		if len(members) == 0 {
			return blockData{}, false, false
		}
		data := blockData{
			Block: transcript.Block{ID: g.ID},
			Frame: s.run.frame,
			Group: &groupData{Members: members, Durs: durs, Open: t.fold.expanded(e.group)},
		}
		return data, groupLive(members), true
	}
	b, ok := s.proj.Block(e.id)
	if !ok {
		return blockData{}, false, false
	}
	data := s.data(b)
	data.Nested = e.kind == entryNested
	return data, isLive(b) || data.Thinking, true
}

// groupMembers returns the blocks ids name that are still in s's
// projection, with their measured durations (0 for none).
func groupMembers(s *sessionState, ids []transcript.BlockID) ([]transcript.Block, []time.Duration) {
	blocks := make([]transcript.Block, 0, len(ids))
	durs := make([]time.Duration, 0, len(ids))
	for _, id := range ids {
		b, ok := s.proj.Block(id)
		if !ok {
			continue
		}
		blocks = append(blocks, b)
		durs = append(durs, s.times.durs[id])
	}
	return blocks, durs
}
```

- [ ] **Step 4: Route `sessionState` through the layout (`internal/ui/session.go`)**

Replace `applyResult` and its comment with:

```go
// applyResult is what the App must do after sessionState.apply: re-render
// upsert now (with relist, by rebuilding the list from the new layout),
// rebuild the whole list (reload), or send the queue now the previous
// send has settled (settled).
type applyResult struct {
	upsert  []transcript.BlockID
	reload  bool
	relist  bool
	settled bool
}
```

In `apply`, replace everything from `s.times.thinking(s.proj, ev, ids, s.clk.Now())` to the end of the function with:

```go
	s.times.thinking(s.proj, ev, ids, s.clk.Now())
	res := applyResult{relist: regroupsOn(ev) && s.track.regroup(s.proj)}
	switch e := ev.(type) {
	case event.TextDelta, event.ReasoningDelta:
		for _, id := range ids {
			s.track.dirty.add(id)
		}
		return applyResult{}
	case event.MessageStarted:
		s.run.started = s.run.started || s.run.inFlight
		if e.Model != "" {
			s.model = e.Model
		}
	case event.ToolCallStarted, event.ToolCallFinished:
		s.timeTools(ev, ids)
	case event.StepFinished:
		s.usage = e.Usage
		s.cost += e.CostUSD
	case event.RunFinished, event.RunFailed:
		_, finished := ev.(event.RunFinished)
		res.settled = s.run.end(finished)
		res.upsert = s.withDirty(ids)
		return res
	case event.SessionUpdated:
		s.info = withID(e.Info, s.info.ID)
	case event.TodosUpdated:
		s.todos = slices.Clone(e.Todos)
	}
	if len(ids) == 0 && !res.relist {
		return applyResult{}
	}
	res.upsert = s.withDirty(ids)
	return res
}

// regroupsOn reports whether ev can change how the root's blocks group:
// a tool call starting or finishing, or a run settling its calls.
func regroupsOn(ev event.Event) bool {
	switch ev.(type) {
	case event.ToolCallStarted, event.ToolCallFinished, event.RunFinished, event.RunFailed:
		return true
	}
	return false
}
```

(Keep every other case exactly as it is in the file when you edit it; the list above matches the file as of writing.)

Replace `dropUser` with:

```go
// dropUser removes the block of a send that never ran and returns every
// remaining item at its current version (nothing re-renders).
func (s *sessionState) dropUser(id transcript.BlockID) []blocklist.Item {
	s.proj.DropUser(id)
	delete(s.track.versions, id)
	entries, _ := s.track.fold.regroup(s.proj.Blocks())
	return s.track.layoutItems(s, entries, bumpNone)
}
```

Replace `items`, `allItems`, and `item` (delete `item` and its comment entirely) with:

```go
// items builds, each at a new version, the items that show the blocks
// ids (a member of a collapsed group shows as its header), and tracks
// which of them are live.
func (s *sessionState) items(ids []transcript.BlockID) []blocklist.Item {
	return s.track.build(s, s.track.visible(ids), bumpAll)
}

// allItems regroups the blocks and builds every item of the layout, in
// display order, each at a new version.
func (s *sessionState) allItems() []blocklist.Item {
	entries, _ := s.track.fold.regroup(s.proj.Blocks())
	return s.track.layoutItems(s, entries, bumpAll)
}
```

- [ ] **Step 5: Create `internal/ui/foldctl.go`**

```go
package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/ui/transcript"
)

// foldCtl runs the transcript's tool-call groups from the App (tool-call
// groups spec §5–6): it renders an event's changes, rebuilding the list
// when the layout changed other than at its end, and keeps the selection
// on a block that is still listed.
type foldCtl struct{ a *App }

// apply renders an event's changed blocks: a rebuild when res.relist,
// else an upsert.
func (c foldCtl) apply(res applyResult) tea.Cmd {
	if res.relist {
		return c.relist(res.upsert)
	}
	c.a.flush(res.upsert)
	return nil
}

// relist rebuilds the list from the current layout, re-rendering only
// the headers and the items showing changed (itemTrack.relistItems). When
// the selected item is no longer listed (its group collapsed, or the
// first call of a new group turned into its header), the selection moves
// to its group's header, and an open details split follows it.
func (c foldCtl) relist(changed []transcript.BlockID) tea.Cmd {
	a := c.a
	before, had := a.w.list.Selected()
	items := a.sess.track.relistItems(a.sess, changed)
	a.w.setItems(items)
	if !had || slices.ContainsFunc(items, func(it blocklist.Item) bool { return it.ID == before.ID }) {
		return nil
	}
	if g, ok := a.sess.track.fold.groupOf(transcript.BlockID(before.ID)); ok {
		a.w.list.Select(string(g))
	}
	return normalKeys{a}.syncDetails(before)
}
```

- [ ] **Step 6: Use it in `App.onEvent` (`internal/ui/app.go`)**

Replace

```go
	res := a.sess.apply(ev)
	if res.reload {
		a.w.setItems(a.sess.allItems())
	}
	a.flush(res.upsert)
	cmds := []tea.Cmd{waitEvent(a.sub)}
```

with

```go
	res := a.sess.apply(ev)
	if res.reload {
		a.w.setItems(a.sess.allItems())
	}
	cmds := []tea.Cmd{waitEvent(a.sub), foldCtl{a}.apply(res)}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/ui/... -count=1 && go test ./internal/archtest/ -count=1`
Expected: `ok`. `TestFold_*` pass; every existing test (streaming, permissions, images, mouse, benchmarks' setup) still passes. If `TestSize_Structs` fails, count fields/methods again: `sessionState` should now have 13 fields and 19 methods.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/items.go internal/ui/session.go internal/ui/foldctl.go internal/ui/app.go internal/ui/fold_app_test.go
git commit -m "feat(ui): fold exploration calls into group headers in the transcript"
```

### Task 6: The `transcript.fold` action on `o`

**Files:**
- Modify: `internal/ui/actions/actions.go` (the ID constant and the built-in entry, both right after `TranscriptDetails`)
- Modify: `internal/ui/actions/keymap.go` (`DefaultBindings`)
- Modify: `internal/ui/app.go` (`dispatchAction`)
- Modify: `internal/ui/foldctl.go` (`toggle`, `relayout`)
- Test: `internal/ui/actions/actions_test.go`, `internal/ui/actions/keymap_test.go`, `internal/ui/fold_app_test.go`

**Interfaces:**
- Consumes: `foldState.toggle`, `foldState.regroup` (Task 3); `foldCtl.relist` (Task 5).
- Produces: `actions.TranscriptFold ID = "transcript.fold"`; `foldCtl.toggle() tea.Cmd`; `foldCtl.relayout() tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

In `internal/ui/actions/actions_test.go` (`TestCatalogue_BuiltinsAndExt`), add this row right after the `TranscriptDetails` row:

```go
		{TranscriptFold, "Toggle group", "Transcript", false},
```

and raise both numbers in the length assertion by one (as of writing: `len(all) != 21` and `"All() = %d actions, want 21 (20 builtins + 1 ext)"`).

In `internal/ui/actions/keymap_test.go` (`TestResolve_DefaultsAndOverride`), add after the `y` row:

```go
		{"normal", "o", TranscriptFold},
```

Append to `internal/ui/fold_app_test.go`:

```go
func TestFold_OTogglesAGroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	want := []string{"u/u1", "g/c1", "t/c1", "m/a2/0", "t/c2", "m/a3/0"}
	if got := ta.listIDs(); !slices.Equal(got, want) {
		t.Fatalf("after o, list = %v, want %v", got, want)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the header g/c1", got)
	}
	view := xansi.Strip(ta.view())
	for _, s := range []string{"▾ explored · 1 read, 1 grep", "  ▸ read  a.go · 1 lines", `  ▸ grep  "TODO" · 1 matches`} {
		if !strings.Contains(view, s) {
			t.Errorf("view has no %q:\n%s", s, view)
		}
	}
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("after o again, list = %v, want %v", got, want)
	}
}

func TestFold_OOnAMemberCollapsesToTheHeader(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	ta.app.w.list.Select("t/c2")
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the header g/c1", got)
	}
}

func TestFold_OElsewhereDoesNothing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc") // the selection is on the reply, m/a3/0
	before := ta.app.sess.track.versions["g/c1"]
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v unchanged", got, want)
	}
	if got := ta.app.sess.track.versions["g/c1"]; got != before {
		t.Errorf("header version %d → %d, want nothing re-rendered", before, got)
	}
}

func TestFold_KeyCanBeRemapped(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups(), withKeybinds(map[string]string{"normal.z": "transcript.fold"}))
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("z")
	if got := ta.listIDs(); len(got) != 6 {
		t.Errorf("after the remapped z, list = %v, want the group expanded", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/... -run 'TestCatalogue|TestResolve|TestFold_(O|Key)' -count=1`
Expected: FAIL to compile (`undefined: TranscriptFold`) in `actions`. In `ui`, `TestFold_OTogglesAGroup`, `TestFold_OOnAMemberCollapsesToTheHeader`, and `TestFold_KeyCanBeRemapped` fail with an unchanged list; `TestFold_OElsewhereDoesNothing` passes already (it guards the no-op branch against a toggle that always rebuilds).

- [ ] **Step 3: Add the action and its key**

In `internal/ui/actions/actions.go`, add the constant right after `TranscriptDetails`:

```go
	TranscriptFold    ID = "transcript.fold"
```

and the entry right after the `TranscriptDetails` entry in `builtinActions`:

```go
		{ID: TranscriptFold, Title: "Toggle group", Group: "Transcript"},
```

In `internal/ui/actions/keymap.go`, in `DefaultBindings`, add after the `y` binding:

```go
		{Mode: "normal", Key: "o", Command: string(TranscriptFold)},
```

(`o` is not in `isFixed`, so `[keybinds]` can remap it.)

- [ ] **Step 4: Toggle through `foldCtl`**

Append to `internal/ui/foldctl.go`:

```go
// toggle expands or collapses the selected group header, or collapses
// the group of the selected member (the selection then moves to its
// header). It does nothing on any other block.
func (c foldCtl) toggle() tea.Cmd {
	a := c.a
	it, ok := a.w.list.Selected()
	if !ok {
		return nil
	}
	if _, ok := a.sess.track.fold.toggle(transcript.BlockID(it.ID)); !ok {
		return nil
	}
	return c.relayout()
}

// relayout re-lays out the groups after a fold or search change and
// rebuilds the list.
func (c foldCtl) relayout() tea.Cmd {
	c.a.sess.track.fold.regroup(c.a.sess.proj.Blocks())
	return c.relist(nil)
}
```

In `internal/ui/app.go`, `dispatchAction`, add after the `TranscriptDetails` case:

```go
	case actions.TranscriptFold:
		return foldCtl{a}.toggle()
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/ui/... ./internal/app/ ./internal/archtest/ -count=1`
Expected: `ok` (`internal/app`'s `registry_test` compares the registered binds with `DefaultBindings()`, so it follows automatically). `wc -l internal/ui/app.go` must be ≤ 500.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/actions/actions.go internal/ui/actions/keymap.go internal/ui/actions/actions_test.go internal/ui/actions/keymap_test.go internal/ui/app.go internal/ui/foldctl.go internal/ui/fold_app_test.go
git commit -m "feat(ui): o expands and collapses a tool-call group (transcript.fold)"
```

---

### Task 7: Group details and yank

**Files:**
- Modify: `internal/ui/groups.go` (`groupDetails`, `reasoningLabel`, `groupYank`)
- Modify: `internal/ui/foldctl.go` (`group`)
- Modify: `internal/ui/mode_normal.go` (`openDetailsFor`, `yank`)
- Test: `internal/ui/groups_test.go`, `internal/ui/fold_app_test.go`

**Interfaces:**
- Consumes: `foldState.members` (Task 3), `groupMembers` (Task 5), `oneLiner`, `header`, `thoughtFor` (existing).
- Produces: `groupDetails(members []transcript.Block, durs []time.Duration) details.Content`; `groupYank(members []transcript.Block) string`; `foldCtl.group(id transcript.BlockID) ([]transcript.Block, []time.Duration, bool)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/groups_test.go` (add `"reflect"` and `"time"` to its imports):

```go
func TestGroupDetails(t *testing.T) {
	t.Parallel()
	ok := transcript.StateOK
	members := []transcript.Block{
		gm("c1", "read", `{"path":"a.go"}`, ok),
		{ID: "m/a2/0", Kind: transcript.KindReasoning},
		gm("c2", "grep", `{"pattern":"TODO"}`, ok),
		{ID: "m/a3/0", Kind: transcript.KindReasoning},
	}
	got := groupDetails(members, []time.Duration{0, 2 * time.Second, 0, 0})
	if got.Header != "group · 1 read, 1 grep" {
		t.Errorf("header = %q, want %q", got.Header, "group · 1 read, 1 grep")
	}
	want := []string{"▸ read  a.go · 1 lines", "∴ thought for 2.0s", `▸ grep  "TODO" · 1 matches`, "∴ thinking"}
	if !reflect.DeepEqual(got.Lines, want) {
		t.Errorf("lines = %q\nwant    %q", got.Lines, want)
	}
}

func TestGroupYank(t *testing.T) {
	t.Parallel()
	ok := transcript.StateOK
	members := []transcript.Block{
		gm("c1", "read", `{"path":"a.go"}`, ok),
		{ID: "m/a2/0", Kind: transcript.KindReasoning, Text: "skipped"},
		gm("c2", "grep", `{"pattern":"TODO"}`, ok),
		gm("c3", "glob", `{"pattern":"*.go"}`, ok),
		gm("c4", "read", `{"path":"x\u001b[2Jy.go"}`, ok),
	}
	if got, want := groupYank(members), "a.go\nTODO\n*.go\nxy.go"; got != want {
		t.Errorf("groupYank = %q, want %q", got, want)
	}
}
```

Append to `internal/ui/fold_app_test.go` (add `"fmt"` to its imports):

```go
func TestFold_EnterOnAHeaderShowsItsMembers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("enter")
	if ta.app.view.detailsFor != "g/c1" {
		t.Fatalf("detailsFor = %q, want g/c1", ta.app.view.detailsFor)
	}
	body := xansi.Strip(ta.app.w.details.View())
	for _, s := range []string{"group · 1 read, 1 grep", "▸ read  a.go · 1 lines", "∴ thinking", `▸ grep  "TODO" · 1 matches`} {
		if !strings.Contains(body, s) {
			t.Errorf("details have no %q:\n%s", s, body)
		}
	}
}

func TestFold_YankOnAHeaderCopiesSubjects(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	cmd := normalKeys{ta.app}.yank()
	if cmd == nil {
		t.Fatal("yank on a header: want a Cmd")
	}
	if got := fmt.Sprint(cmd()); got != "a.go\nTODO" {
		t.Errorf("clipboard = %q, want %q", got, "a.go\nTODO")
	}
	if ta.app.view.hint != "yanked" {
		t.Errorf("hint = %q, want yanked", ta.app.view.hint)
	}
}

func TestFold_DetailsFollowCollapse(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	ta.app.w.list.Select("t/c1")
	ta.key("enter")
	if ta.app.view.detailsFor != "t/c1" {
		t.Fatalf("detailsFor = %q, want t/c1", ta.app.view.detailsFor)
	}
	ta.key("o")
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want g/c1", got)
	}
	if ta.app.view.detailsFor != "g/c1" {
		t.Errorf("detailsFor = %q, want the details to follow to g/c1", ta.app.view.detailsFor)
	}
	if body := xansi.Strip(ta.app.w.details.View()); !strings.Contains(body, "group · 1 read, 1 grep") {
		t.Errorf("details still show the hidden member:\n%s", body)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/ -run 'TestGroup(Details|Yank)|TestFold_(Enter|Yank|DetailsFollow)' -count=1`
Expected: FAIL to compile (`undefined: groupDetails`, `groupYank`); after Step 3 alone, the three App tests still fail (`detailsFor` stays empty or on t/c1, and `yank` returns nil for a header).

- [ ] **Step 3: Add details and yank text to `internal/ui/groups.go`**

Add `"encoding/json"` and `"github.com/gammons/jig/internal/bubbles/details"` to its imports, and append:

```go
// groupDetails is a group's details content (tool-call groups spec
// §5.2): the header "group · <tally>", then one line per member in order,
// a call's plain one-liner or a reasoning block's label. It needs no port.
func groupDetails(members []transcript.Block, durs []time.Duration) details.Content {
	lines := make([]string, 0, len(members))
	for i, m := range members {
		if m.Kind == transcript.KindReasoning {
			lines = append(lines, reasoningLabel(durs[i]))
			continue
		}
		lines = append(lines, ansi.SanitizeLine(oneLiner(m)))
	}
	return details.Content{Header: header("group", groupTally(members)), Lines: lines}
}

// reasoningLabel is a finished reasoning block's line: "∴ thought for
// <dur>", or "∴ thinking" with no measured duration.
func reasoningLabel(d time.Duration) string {
	if d <= 0 {
		return "∴ thinking"
	}
	return "∴ thought for " + thoughtFor(d)
}

// groupYank is what y copies for a group: each member call's subject, one
// per line in order (a read's path, a grep's or glob's pattern),
// sanitized. Reasoning is skipped.
func groupYank(members []transcript.Block) string {
	subjects := make([]string, 0, len(members))
	for _, m := range members {
		if m.Kind != transcript.KindTool || m.Call == nil {
			continue
		}
		var in struct {
			Path    string `json:"path"`
			Pattern string `json:"pattern"`
		}
		_ = json.Unmarshal(m.Call.Input, &in)
		subject := in.Pattern
		if m.Call.Name == "read" {
			subject = in.Path
		}
		subjects = append(subjects, ansi.SanitizeLine(subject))
	}
	return strings.Join(subjects, "\n")
}
```

- [ ] **Step 4: Look groups up through `foldCtl`**

Add `"time"` to `internal/ui/foldctl.go`'s imports and append:

```go
// group returns group id's member blocks and their durations; false when
// id is not a group.
func (c foldCtl) group(id transcript.BlockID) ([]transcript.Block, []time.Duration, bool) {
	ids, ok := c.a.sess.track.fold.members(id)
	if !ok {
		return nil, nil, false
	}
	members, durs := groupMembers(c.a.sess, ids)
	return members, durs, len(members) > 0
}
```

- [ ] **Step 5: Route details and yank (`internal/ui/mode_normal.go`)**

Add `"github.com/gammons/jig/internal/bubbles/details"` to its imports. Replace `openDetailsFor` with:

```go
// openDetailsFor builds and shows id's details: a group's member list, or
// a block's own details. A block's build is sized for the details split
// at the App's current width/height directly, rather than a.lay (not yet
// recomputed for detailsOpen when opening for the first time in this same
// key press), so the content is never built at a stale width.
func (h normalKeys) openDetailsFor(id transcript.BlockID) tea.Cmd {
	a := h.a
	members, durs, isGroup := foldCtl{a}.group(id)
	b, isBlock := a.sess.proj.Block(id)
	if !isGroup && !isBlock {
		return nil
	}
	a.view.detailsFor = id
	a.img.shown = nil
	var content details.Content
	var cmd tea.Cmd
	if isGroup {
		content = groupDetails(members, durs)
	} else {
		// Only the side slot's size is needed: the prompt beside it wraps
		// the same at either width to within its height, and relayout sizes
		// the prompt for real.
		lay := computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, true)
		content, cmd = buildDetails(a.ctx, b, lay.Side.W, lay.Side.H, a.w.render, a.ports, a.img)
	}
	a.w.details.SetContent(content)
	if a.view.mouse.pane == paneDetails {
		a.view.mouse.sel = selection.Range{}
	}
	return cmd
}
```

In `yank`, insert right after the `item, ok := a.w.list.Selected()` check:

```go
	if members, _, ok := foldCtl{a}.group(transcript.BlockID(item.ID)); ok {
		a.view.hint = "yanked"
		return tea.SetClipboard(groupYank(members))
	}
```

and update `yank`'s doc comment to mention it: "…copies the selected block's text (a group header's member subjects, groupYank) to the clipboard…".

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/ui/... -count=1`
Expected: `ok`, including the existing details goldens (`app_details_open`, `narrow_details`) unchanged.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/groups.go internal/ui/groups_test.go internal/ui/foldctl.go internal/ui/mode_normal.go internal/ui/fold_app_test.go
git commit -m "feat(ui): details and yank for tool-call group headers"
```

### Task 8: A search holds every group open

**Files:**
- Modify: `internal/ui/foldctl.go` (`setSearch`)
- Modify: `internal/ui/mode_normal.go` (`handleSearch`'s enter case, `closeOrClear`)
- Test: `internal/ui/fold_app_test.go`

**Interfaces:**
- Consumes: `foldState.search` (Task 3), `foldCtl.relayout` (Task 6).
- Produces: `foldCtl.setSearch(on bool) tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/fold_app_test.go`:

```go
func TestFold_SearchExpandsGroupsAndClearingRestores(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.key("/")
	ta.typeText("a.go")
	ta.key("enter")
	want := []string{"u/u1", "g/c1", "t/c1", "m/a2/0", "t/c2", "m/a3/0"}
	if got := ta.listIDs(); !slices.Equal(got, want) {
		t.Fatalf("with a search applied, list = %v, want %v", got, want)
	}
	ta.key("n")
	if got := ta.selectedID(); got != "t/c1" {
		t.Fatalf("n selected %q, want the match inside the group, t/c1", got)
	}
	ta.key("esc") // no split open: clears the search
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("after clearing the search, list = %v, want %v", got, want)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the hidden match's header g/c1", got)
	}
}

func TestFold_ClearingSearchKeepsAUserExpandedGroupOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	ta.key("/")
	ta.typeText("nothing matches this")
	ta.key("enter")
	ta.key("esc")
	if got := ta.listIDs(); len(got) != 6 {
		t.Errorf("after clearing the search, list = %v, want g/c1 still expanded", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/ -run 'TestFold_(Search|ClearingSearch)' -count=1`
Expected: `TestFold_SearchExpandsGroupsAndClearingRestores` FAILs (the list stays collapsed, and `n` finds no match). `TestFold_ClearingSearchKeepsAUserExpandedGroupOpen` passes already; it guards the restore once Step 3 lands.

- [ ] **Step 3: Implement**

Append to `internal/ui/foldctl.go`:

```go
// setSearch holds every group open while a search is applied (on), so
// a match inside a collapsed group can be found, and lets each group
// return to its own state when the search is cleared.
func (c foldCtl) setSearch(on bool) tea.Cmd {
	f := &c.a.sess.track.fold
	if f.search == on {
		return nil
	}
	f.search = on
	return c.relayout()
}
```

In `internal/ui/mode_normal.go`, `handleSearch`, replace the `"enter"` case with:

```go
	case "enter":
		a.view.searching = false
		query := a.w.search.Value()
		cmd := foldCtl{a}.setSearch(query != "")
		a.w.list.SetSearch(query)
		a.w.search.Blur()
		return cmd
```

(the list must hold the members before `SetSearch` counts matches). In `closeOrClear`, replace the last two lines

```go
	a.w.list.SetSearch("")
	return nil
```

with

```go
	a.w.list.SetSearch("")
	return foldCtl{a}.setSearch(false)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/ui/... -count=1`
Expected: `ok`, including `TestNormal_SearchInputAndClear` and `TestNormal_EscClosesDetailsFirst`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/foldctl.go internal/ui/mode_normal.go internal/ui/fold_app_test.go
git commit -m "feat(ui): a transcript search shows every tool-call group expanded"
```

---

### Task 9: A pending permission holds its group open

**Files:**
- Modify: `internal/ui/fold.go` (`awaiting` field, `regroup`, `expanded`, the struct's doc)
- Modify: `internal/ui/session.go` (`regroupsOn`)
- Test: `internal/ui/fold_test.go`, `internal/ui/fold_app_test.go`

**Interfaces:**
- Consumes: `transcript.StateAwaiting`; `permCtl` (existing, unchanged).
- Produces: `foldState.awaiting map[transcript.BlockID]bool` (group IDs with a member awaiting permission).

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/fold_test.go`:

```go
func TestFold_AwaitingPermissionForcesOpen(t *testing.T) {
	t.Parallel()
	blocks := foldBlocks()
	blocks[3].State = transcript.StateAwaiting // t/c2
	var f foldState
	got, _ := f.regroup(blocks)
	wantEntries(t, got, "plain u/u1", "header g/c1", "nested t/c1", "nested m/a2/0", "nested t/c2", "plain m/a3/0")
	blocks[3].State = transcript.StateRunning
	got, changed := f.regroup(blocks)
	wantEntries(t, got, "plain u/u1", "header g/c1", "plain m/a3/0")
	if !changed {
		t.Error("releasing the hold: regroup = false, want true (members leave the list)")
	}
}
```

Append to `internal/ui/fold_app_test.go`:

```go
func TestFold_PermissionForcesGroupOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.startTool("m1", "c2", "read", `{"path":"/etc/hosts"}`)
	ta.event(event.PermissionRequested{Base: rootBase(), RequestID: "p1", Tool: "read", Subject: "/etc/hosts",
		Call: core.ToolCall{ID: "c2", Name: "read"}})

	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1", "t/c1", "t/c2"}) {
		t.Fatalf("with a pending permission, list = %v, want [<user> g/c1 t/c1 t/c2]", got)
	}
	if got := ta.selectedID(); got != "t/c2" {
		t.Fatalf("selected = %q, want the requesting member t/c2 (the focus rule)", got)
	}
	ta.app.w.list.Top()
	ta.key("g")
	ta.key("p")
	if got := ta.selectedID(); got != "t/c2" {
		t.Fatalf("gp selected %q, want t/c2", got)
	}
	if req := ta.app.w.card.Request(); req == nil || req.ID != "p1" || !(permCtl{ta.app}).onCard() {
		t.Fatalf("card = %+v on %q, want p1 on the selected member", req, ta.app.w.cardAt.block)
	}
	ta.arm()
	ta.key("a")
	if len(ta.perms.replies) != 1 || ta.perms.replies[0].ID != "p1" {
		t.Fatalf("replies = %+v, want one for p1", ta.perms.replies)
	}

	ta.event(event.PermissionResolved{Base: rootBase(), RequestID: "p1", Reply: core.PermissionReply{Kind: core.ReplyOnce}})
	ta.finishTool("m1", "c2", "read", "1: 127.0.0.1 localhost", false)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		t.Errorf("after the reply, list = %v, want the group collapsed again", got)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the header g/c1", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/ -run 'TestFold_(AwaitingPermission|PermissionForces)' -count=1`
Expected: FAIL. The unit test sees a collapsed layout; the App test's list is `[<user> g/c1]` with the member (and its card) hidden.

- [ ] **Step 3: Implement**

In `internal/ui/fold.go`, add the field after `search` and extend the doc comment ("…whether a search forces every group open, the groups holding a member awaiting permission (awaiting), and the layout…"):

```go
	search   bool
	awaiting map[transcript.BlockID]bool
	order    []foldEntry
```

In `regroup`, right after the loop that fills `f.owner` and `f.byID`, add:

```go
	f.awaiting = map[transcript.BlockID]bool{}
	for _, b := range blocks {
		if gi, ok := f.owner[b.ID]; ok && b.State == transcript.StateAwaiting {
			f.awaiting[f.groups[gi].ID] = true
		}
	}
```

Replace `expanded` with:

```go
// expanded reports whether group gi shows its members: the user opened
// it, a search is applied, or a member awaits permission (its card must
// never be hidden).
func (f *foldState) expanded(gi int) bool {
	id := f.groups[gi].ID
	return f.open[id] || f.search || f.awaiting[id]
}
```

Update `toggle`'s doc comment: "A group held open (a search, a pending permission) keeps showing until the hold ends; the flipped state applies then."

In `internal/ui/session.go`, `regroupsOn`, add the permission events:

```go
	case event.ToolCallStarted, event.ToolCallFinished, event.RunFinished, event.RunFailed,
		event.PermissionRequested, event.PermissionResolved:
		return true
```

and extend its doc comment: "…or a run settling its calls, or a permission request holding a group open or releasing it."

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/ui/... -count=1`
Expected: `ok`, including every `TestApp_Permission*`/card test in `permissions_test.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/fold.go internal/ui/session.go internal/ui/fold_test.go internal/ui/fold_app_test.go
git commit -m "feat(ui): a pending permission keeps its tool-call group open"
```

### Task 10: Benchmarks and budgets

**Files:**
- Create: `internal/ui/fold_bench_test.go`

**Interfaces:**
- Consumes: `benchHistory` (existing, `app_bench_test.go`), `newTestApp`, `withResume`, `withSize`, `rootBase`.
- Produces: `benchHistoryGroups(n int) []core.Message`; `BenchmarkApp_FoldToggle2000`; `BenchmarkApp_ToolStart2000`.

- [ ] **Step 1: Write the benchmarks**

Create `internal/ui/fold_bench_test.go`:

```go
package ui

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// benchHistoryGroups is benchHistory with a group in every fourth message
// (3, 7, 11, …, all assistant messages): reasoning, then a read and a
// grep with their results, which group as g/r<index>.
func benchHistoryGroups(n int) []core.Message {
	msgs := benchHistory(n)
	for i := 3; i < n; i += 4 {
		r, q := fmt.Sprintf("r%05d", i), fmt.Sprintf("q%05d", i)
		msgs[i].Parts = []core.Part{
			{Kind: core.PartReasoning, Text: "look around first"},
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: r, Name: "read", Input: json.RawMessage(`{"path":"internal/ui/render.go"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: r, Name: "read", Output: "1: package ui"}},
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: q, Name: "grep", Input: json.RawMessage(`{"pattern":"renderGroup"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: q, Name: "grep", Output: "internal/ui/groups.go:40: func"}},
		}
	}
	return msgs
}

// BenchmarkApp_FoldToggle2000 measures o on a group header (the group
// expands or collapses: a list rebuild) and the next View, on a resumed
// session of 2,000 messages holding 500 groups.
func BenchmarkApp_FoldToggle2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistoryGroups(2000), nil))
	_ = ta.view()
	ta.key("esc")
	if !ta.app.w.list.Select("g/r01003") {
		b.Fatal("no group g/r01003 in the list")
	}
	_ = ta.view()
	b.ReportAllocs()
	for b.Loop() {
		ta.key("o")
		_ = ta.view()
	}
}

// BenchmarkApp_ToolStart2000 measures one ToolCallStarted that joins the
// run's trailing group and absorbs the reasoning before it (a structural
// rebuild of the list), and the next View, on the same resumed session.
// Each iteration's setup (a new step with its reasoning, and every 20
// steps a reply that ends the group so it stays small) is untimed.
func BenchmarkApp_ToolStart2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistoryGroups(2000), nil))
	_ = ta.view()
	ta.typeText("go")
	ta.key("enter")
	n := 0
	step := func() core.MessageID {
		n++
		msg := core.MessageID(fmt.Sprintf("live%d", n))
		ta.event(event.MessageStarted{Base: rootBase(), MessageID: msg})
		ta.event(event.ReasoningDelta{Base: rootBase(), MessageID: msg, Text: "next"})
		ta.fire()
		return msg
	}
	start := func(msg core.MessageID) {
		ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: msg,
			Call: core.ToolCall{ID: fmt.Sprintf("b%d", n), Name: "read", Input: json.RawMessage(`{"path":"a.go"}`)}})
	}
	finish := func(msg core.MessageID) {
		ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: msg,
			Result: core.ToolResult{CallID: fmt.Sprintf("b%d", n), Name: "read", Output: "1: package a"}})
	}
	first := step()
	start(first)
	finish(first)
	_ = ta.view()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		if i%20 == 19 {
			reply := step()
			ta.event(event.TextDelta{Base: rootBase(), MessageID: reply, Text: "ok"})
			lone := step()
			start(lone)
			finish(lone)
		}
		msg := step()
		_ = ta.view()
		b.StartTimer()
		start(msg)
		_ = ta.view()
		b.StopTimer()
		finish(msg)
		b.StartTimer()
	}
}
```

- [ ] **Step 2: Check the setup is right, then run them**

Run: `go test ./internal/ui/ -run XXX -bench 'BenchmarkApp_(FoldToggle|ToolStart)2000' -benchtime 1x -count=1`
Expected: both run without `b.Fatal` (the group `g/r01003` exists).

Run: `go test ./internal/ui/ -run XXX -bench 'BenchmarkApp_(FoldToggle|ToolStart)2000' -benchmem -count=3`
Expected: `FoldToggle2000` < 50 ms/op and `ToolStart2000` < 5 ms/op in every run. Write down the median of each and the CPU model (`lscpu | grep 'Model name'`, or the `cpu:` line of the benchmark output) for Task 12.

**If `ToolStart2000` misses its budget, fix the algorithm; never raise the budget.** Profile first (`-cpuprofile cpu.out`, then `go tool pprof -top cpu.out`). The expected cost is `Projection.Blocks()` cloning every block plus `Groups` and `layout` over all of them on each tool event. The fix the spec names: blocks are only ever appended, so only the trailing group can change. Keep the last full layout (`order`) and, on a tool/permission/run event, re-run `Groups` and `layout` only over the blocks from the first block of the last group (or the last plain tool block) onward, splicing the result onto the unchanged prefix of `order`. A `load`, `dropUser`, fold toggle, or search change still does the full regroup. Add a unit test in `fold_test.go` showing an incremental regroup gives the same `order` as a full one for the `TestFold_RegroupReportsRestructure` sequence, then re-measure.

- [ ] **Step 3: Check the existing budgets still hold**

Run: `go test ./internal/ui/ -run XXX -bench 'BenchmarkApp_' -benchmem -count=1`
Expected: every existing benchmark is within its AGENTS.md budget (StreamTick2000 < 5 ms, ReasoningTick2000 < 5 ms, Resize2000 < 1.5 s, Keystroke2000 < 1.5 ms, Wheel2000 < 1.5 ms, DetailsToggle2000 < 50 ms), and `go test -run XXX -bench . -benchmem ./internal/bubbles/blocklist/` is within its three budgets.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/fold_bench_test.go
git commit -m "test(ui): benchmarks for tool-call group toggles and rebuilds"
```

---

### Task 11: End to end under a pty

**Files:**
- Create: `e2e/tui_groups_test.go`

**Interfaces:**
- Consumes: e2e helpers `newEnv`, `writeFile`, `writeScript`, `writeConfig`, `jigtestConfig`, `command`, `call`, `exitCode`, `newScreen`, `(*screen).read/.waitFor/.mark/.snapshot`, `tuiTimeout` (all existing, build tag `jigtest`).

- [ ] **Step 1: Write the test**

Create `e2e/tui_groups_test.go`:

```go
//go:build jigtest

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/creack/pty"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// TestE2E_TUIToolCallGroup drives the built binary under a pty: three
// reads across two steps fold into one group, and o expands it. The pty
// stream is redrawn cell by cell (a header changing in place from
// "exploring" to "explored · 3 reads" never arrives as one string), so
// the test waits only for text drawn fresh: the reply, the NORMAL badge,
// and the ▾ that expanding draws. The header's exact text is pinned by
// the App-level tests in internal/ui.
func TestE2E_TUIToolCallGroup(t *testing.T) {
	env := newEnv(t)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeFile(t, filepath.Join(env.work, name), "package x\n")
	}
	script := writeScript(t, env, "tui.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{Calls: []jigtest.Call{call("r1", "read", `{"path":"a.go"}`), call("r2", "read", `{"path":"b.go"}`)}},
			{Calls: []jigtest.Call{call("r3", "read", `{"path":"c.go"}`)}},
			{Text: "read all three"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	ctx, cancel := context.WithTimeout(context.Background(), tuiTimeout)
	defer cancel()
	cmd := command(ctx, env, "--cwd", env.work)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "JIG_IMAGES=off")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	scr := newScreen()
	go scr.read(tty)
	write := func(s string) {
		t.Helper()
		if _, err := tty.WriteString(s); err != nil {
			t.Fatalf("writing %q: %v", s, err)
		}
	}

	// No project config, so there is no trust dialog; reads default to allow.
	scr.waitFor(ctx, t, 0, "Message build")
	write("go\r")
	scr.waitFor(ctx, t, 0, "read all three")
	from := scr.mark()
	write("\x1b") // esc: NORMAL, the selection on the reply
	scr.waitFor(ctx, t, from, "NORMAL")
	write("k") // up to the group header
	from = scr.mark()
	write("o")
	scr.waitFor(ctx, t, from, "▾")
	write("\x04")

	err = cmd.Wait()
	if code := exitCode(t, err, scr.snapshot()); code != 0 {
		t.Fatalf("jig exited %d\nscreen:\n%s", code, scr.snapshot())
	}
}
```

- [ ] **Step 2: Run it**

Run: `make build && go test -tags jigtest -race -count=1 -run TestE2E_TUIToolCallGroup ./e2e/`
Expected: `ok`. If it times out waiting for `▾`, read the screen dump in the failure: if the reads show separate lines with no header, grouping did not happen in the binary (check Task 5 wiring); if the header shows but `o` did nothing, the selection was not on it (check that `k` from the reply lands on the header).

To confirm the test can fail: temporarily change `DefaultBindings`' `"o"` to `"O"`, re-run (expect a timeout waiting for `▾`), then revert.

- [ ] **Step 3: Commit**

```bash
git add e2e/tui_groups_test.go
git commit -m "test(e2e): a pty run folds reads into a group that o expands"
```

---

### Task 12: Document it and run the full check

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Update `AGENTS.md`**

In the layout block, change the `internal/ui/transcript/` line to:

```
internal/ui/transcript/                 (Plan 2) transcript projection, core-only; Groups finds runs of read/grep/glob calls
```

In "The TUI", after the three mode bullets (before "**Actions.**"), add:

```markdown
**Tool-call groups.** Two or more consecutive `read`/`grep`/`glob`
calls, with the reasoning between them, show as one header line
(`transcript.Groups`, pure; ID `g/<first call ID>`): `⠋ exploring · 3
reads, 1 grep · read a.go` while live, `▸ explored · 4 reads, 2 greps`
once settled. The projection is unchanged. `foldState`
(`internal/ui/fold.go`, in `sessionState.track`, an `itemTrack` in
`internal/ui/items.go`) lays blocks out as plain items, headers, and
nested members; `foldCtl` (`internal/ui/foldctl.go`) rebuilds the list
(`relist`) when that layout changes other than by growing at its end,
and content changes still upsert, a hidden member mapping to its
header. A group shows expanded when the user opened it (NORMAL `o`, the
remappable `transcript.fold`), while a search is applied, or while a
member awaits permission, so a card is never hidden. An item's version
goes up whenever its nesting changes, so a cached render never keeps a
stale indent. Headers get their own details (the member list) and yank
(the members' paths and patterns).
```

In the shared-code table, add two rows at the end:

```markdown
| Group consecutive exploration calls (read/grep/glob, with the reasoning between them) | `transcript.Groups(blocks)` in `internal/ui/transcript/groups.go` |
| Rebuild the transcript list after a layout change, keeping the selection on a listed block | `foldCtl{a}.relist(changed)` in `internal/ui/foldctl.go` |
```

In the `internal/ui` budget table, add two rows after `BenchmarkApp_ReasoningTick2000`, with the medians and CPU recorded in Task 10:

```markdown
| `BenchmarkApp_FoldToggle2000` — `o` on a group header (a list rebuild), then `View`, over 2,000 messages holding 500 groups | < 50 ms/op | <median> ms/op (<CPU>) |
| `BenchmarkApp_ToolStart2000` — one `ToolCallStarted` that joins a group and absorbs the reasoning before it (a list rebuild), then `View` | < 5 ms/op | <median> ms/op (<CPU>) |
```

(Replace `<median>` and `<CPU>` with the measured values; the other rows' format is `0.79 ms/op (QEMU VM, 4 vCPU)` or a CPU name.)

- [ ] **Step 2: Run the full check**

Run: `make check`
Expected: build, `go test ./... -race`, the jigtest e2e and jigtest tests, both lint runs (`0 issues.`), and `fmt-check` all pass.

- [ ] **Step 3: Try it by hand**

Run `make build`, then `./bin/jig` in this repo and ask something that makes the model read several files (for example "Read internal/ui/fold.go, internal/ui/items.go and internal/ui/foldctl.go and summarize them"). Check: the reads fold into one live header with a spinner and the current file; it settles to `▸ explored · 3 reads`; `esc`, `k` onto it, and `o` shows the three lines indented; `enter` shows the member list; `y` copies the paths; `/` with one of the file names expands it and `esc` folds it back.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md
git commit -m "docs: tool-call groups in AGENTS.md"
```

