# Subagent View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `enter` on a subagent block opens a live, fully rendered transcript of the child session in a right-hand column with a breadcrumb. The column sits beside main when the terminal is wide and replaces it when narrow; `tab`, `h`, and `l` move focus between the two.

**Architecture:**
- `transcript.NewChild` projects a single descendant session.
- A new `ui.pane` holds everything one transcript view needs: its projection, blocklist, item versions, dirty/live sets, and timings. The root view becomes `sess.main`.
- `App.kids` keeps a live pane for every spawned subagent.
- `App.col` is a stack of panes: subagent transcripts and details entries. The old details split becomes an entry in that stack.
- A new `bubbles/breadcrumb` widget draws the column's path.

**Tech Stack:** Go, bubbletea v2 (`charm.land/...`), the existing `blocklist`, `details`, and `mdrender` widgets, and golden-frame tests.

**Spec:** `docs/superpowers/specs/2026-09-29-subagent-view-design.md`

## Global Constraints

- `make check` (build, test with `-race`, lint with and without `jigtest`, `gofmt`) must pass before every commit.
- `ui/...` does no I/O. Every port call goes in a `tea.Cmd` in `cmds.go`. `internal/ui/transcript` imports only stdlib and `internal/core/...`.
- `bubbles/breadcrumb` imports only stdlib, `charm.land/...`, and `internal/bubbles/ansi`. It has no package-level vars, no `func(tea.Msg)` fields, and a `View()` with no parameters.
- Every string that comes from a model, a tool, or the store passes `ansi.SanitizeLine` or `ansi.Sanitize` before it is rendered. That includes breadcrumb segments and pane titles.
- No `time.Now` or `time.Sleep` in `_test.go` files; use the App's `clock`.
- Non-test source files are at most 500 lines (`internal/archtest`), so split files rather than grow the allowlist.
- Performance budgets are binding. The existing ones must hold, and the new `BenchmarkApp_SubagentStream` must run in < 3 ms/op.
- Wide means ≥ 120 columns (`sidebarMinTerm`). The column takes `detailsPercent` (50%) of the width when wide and the whole transcript region when narrow.
- The breadcrumb separator is ` › `, the root segment is `main`, and the hint is `esc back`. When narrow with main focused, main's own breadcrumb hint is `tab → <top title>`.
- A subagent pane's title is `↳ <agent>: <description>`. A details pane's title is its build's header, e.g. `edit · foo.go · 2 hunks`.
- A child load error becomes a notice: `could not load subagent: <err>`.

## Review Focus

1. **A theme change while the column is open.** Every child pane's list, and any details pane, must restyle, not just main. The test goes in Task 7.
2. **Resizing the terminal while the column is open.** The column's top pane must get the new rect, debounced by its own `resizeGen`. A stale `resizeMsg` for a pane that has been popped must be dropped. The test goes in Task 7.
3. **A subagent finishing while you watch it.** The child's `RunFinished` must stop every spinner in the pane, so it has no leftover `live` entries. The next tick must not keep rescheduling once the root is idle. The test goes in Task 7.
4. **Opening another session from the picker while the column is open.** The column must close, `kids` must clear, and focus must go back to main, with no stale child blocks in the new session. The test goes in Task 6.
5. **`y`, `/`, `n`, and `N` in a focused child pane.** They must act on the child's list and blocks, not on main's. The test goes in Task 7.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/ui/transcript/projection.go` (modify) | the `self` field, `NewChild`, `inSubtree` |
| `internal/ui/transcript/apply.go`, `subagent.go` (modify) | `root` → `self` for "own session" checks; the subtree filter |
| `internal/ui/transcript/child_test.go` (create) | `NewChild` tests |
| `internal/bubbles/breadcrumb/{breadcrumb.go,view.go,*_test.go,testdata/}` (create) | the breadcrumb widget |
| `internal/bubbles/details/details.go` (modify) | `WithoutHeader()` |
| `internal/ui/theme/widgets.go` (modify) | `Set.Breadcrumb`, `breadcrumbStyles` |
| `internal/ui/pane.go` (create) | the `pane` type, with the item/version/tick methods moved out of `session.go` |
| `internal/ui/session.go` (modify) | only root state is left, plus `main *pane` |
| `internal/ui/kids.go` (create) | live child panes: `kids`, routing, `childLoadedMsg`, load plus replay |
| `internal/ui/column.go` (create) | the `col` stack, push/pop/replace, focus, breadcrumb segments, the column view |
| `internal/ui/layout.go` (modify) | `columnOpen`, the narrow focus takeover, the breadcrumb rows |
| `internal/ui/mode_normal.go` (modify) | keys routed to the focused pane; `tab`/`h`/`l`/`enter`/`q`/`esc` |
| `internal/ui/permissions.go` (modify) | `cardAt.pane`, the focused-pane target |
| `internal/ui/details.go`, `detailsctl.go`, `stream.go` (modify) | remove the subagent summary and the `subStale` refresh |
| `internal/ui/actions/keymap.go` (modify) | `isFixed` gains `h` and `l` |
| `internal/ui/app_bench_test.go` (modify) | `BenchmarkApp_SubagentStream` |
| `e2e/tui_test.go` (modify) | `TestE2E_TUISubagentView` |
| `AGENTS.md` (modify) | keys, the column, shared-code rows, the invariant, the budget row |

---

### Task 1: `transcript.NewChild`

**Files:**
- Modify: `internal/ui/transcript/projection.go`, `apply.go`, `subagent.go`
- Create: `internal/ui/transcript/child_test.go`

**Interfaces:**
- Produces: `func NewChild(root, child core.SessionID) *Projection`, and `func (p *Projection) Self() core.SessionID`. `New(root)` is unchanged, with `Self() == root`.

- [ ] **Step 1: Write the failing tests in `child_test.go`**

Use `root := core.SessionID("ses_r")`, `kid := "ses_k"`, `grand := "ses_g"`, and `sib := "ses_s"`.

```go
func TestNewChild_OwnEventsBuildBlocks(t *testing.T)
// Apply to NewChild(root,kid):
//   TextDelta{kid, msg "k1", "hello"},
//   ReasoningDelta{kid, "k1", "hm"},
//   ToolCallStarted{kid, "k1", bash call "c1"}, then ToolCallFinished for "c1".
// Assert Blocks() kinds are [KindText, KindReasoning, KindTool], the tool
// block's State is StateOK, and Self() == kid.

func TestNewChild_IgnoresRootAndSiblings(t *testing.T)
// Apply to NewChild(root,kid):
//   TextDelta{root}, ToolCallStarted{sib},
//   PermissionRequested{sib, RequestID "p1"}, PermissionRequested{root, "p2"}.
// Assert Blocks() is empty, Pending() is empty, and every Apply returns nil.

func TestNewChild_GrandchildBecomesSubagentBlock(t *testing.T)
// ToolCallStarted{kid, task call "c2", input {"agent":"general","description":"deeper"}},
// SubagentSpawned{Base kid, Child grand, CallID "c2"},
// ToolCallStarted{grand, bash "c3"}.
// Assert block "t/c2" is KindSubagent with Sub.Child == grand, Sub.Tools == 1, Sub.Current == "bash".

func TestNewChild_PermissionOnOwnToolBlock(t *testing.T)
// ToolCallStarted{kid, bash "c1"}, PermissionRequested{kid, "p1", Call{ID:"c1"}}.
// Assert Block("t/c1").Permission.RequestID == "p1", State == StateAwaiting,
// Pending()[0].Block == "t/c1", and Pending()[0].Subagent == "".
// Then PermissionResolved{kid,"p1"}: Permission == nil and Pending() is empty.

func TestNewChild_RootIDMustMatch(t *testing.T)
// An event with SessionID kid but RootID "other" is ignored.
```

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/ui/transcript -run NewChild`
Expected: FAIL, `undefined: NewChild`.

- [ ] **Step 3: Implement**

- Add a `self core.SessionID` field to `Projection`. `New` sets `self = root`. `NewChild(root, child)` sets `root` and `self = child`.
- In `Apply`, after the `Root()` check, add: `if !p.inSubtree(ev.Session()) { return nil }`, where `inSubtree(s)` is `s == p.self || p.tree.owner[s] != ""`.
  - This runs before the spawn and permission switch.
  - A `SubagentSpawned` whose `Session()` is `self` or a known descendant still passes.
- Pass `p.self` wherever `Apply` passes `p.root` today: `tree.spawn`, `perms.request`, and the `ev.Session() != p.root` check.
- `Load` does nothing new.
- For the root projection (`self == root`), `inSubtree` always returns true. That keeps today's behaviour exactly: `descendant` already drops events from sessions it doesn't know, and a `PermissionRequested` from such a session is still listed with an empty `Block`. Only child projections filter.

- [ ] **Step 4: Run the tests and check they pass**

Run: `go test ./internal/ui/transcript/...`
Expected: PASS, including every existing test, unchanged.

- [ ] **Step 5: Commit**

`git commit -m "feat(transcript): NewChild projects one descendant session"`

---

### Task 2: `bubbles/breadcrumb` widget and theme

**Files:**
- Create: `internal/bubbles/breadcrumb/breadcrumb.go`, `view.go`, `breadcrumb_test.go`, `styles_test.go`, `testdata/golden/*`
- Modify: `internal/ui/theme/widgets.go` (plus its test), `internal/ui/themestate.go` (add the `SetStyles` line in Task 6, once the widget is owned)

**Interfaces:**
- Produces:
  ```go
  type Styles struct{ Muted, Current, CurrentDim, Hint, Rule, RuleFocused lipgloss.Style }
  func DefaultStyles() Styles
  type Option func(*Model)
  func WithStyles(Styles) Option
  type Model struct{ /* segments []string; hint string; focused bool; w int; styles Styles */ }
  func New(opts ...Option) Model
  func (m *Model) SetSegments(s []string)
  func (m *Model) SetHint(h string)
  func (m *Model) SetFocused(f bool)
  func (m *Model) SetWidth(w int)
  func (m *Model) SetStyles(Styles)
  func (m Model) View() string // exactly one row, w cells; "" when w <= 0
  func (m Model) RuleView() string // one row of "─" in Rule/RuleFocused, w cells
  ```
  and `theme.Set.Breadcrumb breadcrumb.Styles`.

- [ ] **Step 1: Write the failing tests**

- `TestView_Full`: segments `["main","↳ explore: find the bug"]`, hint `"esc back"`, width 80, focused. `xansi.Strip(View())` must start with `main › ↳ explore: find the bug`, end with `esc back`, and have width exactly 80.
- `TestView_TruncationOrder`, table-driven. Segments are `["main","↳ explore: a","↳ general: b","edit · foo.go · 2 hunks"]` and the hint is `"esc back"`. For each width, the expected stripped row, right-trimmed:
  - 100: `main › ↳ explore: a › ↳ general: b › edit · foo.go · 2 hunks` followed by `esc back`
  - 60: `main › ↳ explore: a › ↳ general: b › edit · foo.go · 2 hunks`, with the hint dropped
  - 45: `main › … › ↳ general: b › edit · foo.go · 2 hunks`
  - 36: `main › … › edit · foo.go · 2 hunks`
  - 20: `main › … › edit · f…`
  - 5: `main ` (hard cut)
- `TestView_SanitizesNothingButMeasuresWide`: the widget does not sanitize (the App does), but it measures wide runes correctly. A segment containing `日本` must not overflow the width.
- `TestView_ZeroWidth`: `View() == ""`.
- Golden tests with `t.Parallel()` and pinned `DefaultStyles()`: `breadcrumb_focused_80`, `breadcrumb_unfocused_80`, `breadcrumb_collapsed_36`.
- `styles_test.go`: `SetStyles` changes the `View()` output.
- In `internal/ui/theme/widgets_test.go`: `TestBuild_BreadcrumbFromPalette` checks:
  - `Current` is bold with fg = `Primary`;
  - `Muted` and `Hint` have fg = `TextMuted`;
  - `CurrentDim` has fg = `Text`;
  - `Rule` has fg = `Border`;
  - `RuleFocused` has fg = `Primary`.

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/bubbles/breadcrumb/... ./internal/ui/theme/...`
Expected: FAIL (the package doesn't exist).

- [ ] **Step 3: Implement**

`View` builds a candidate row and applies the spec's truncation steps in order until `ansi.Width(row) <= w`:
1. Drop the hint.
2. While there are more than 2 segments, replace the leftmost middle segment (or extend the existing `…` segment) so the row reads `first › … › rest…`.
3. `ansi.Truncate` the last segment to fit, with `…`.
4. `ansi.Truncate` the whole row with no tail.

Then pad to `w`. The hint is right-aligned with at least 2 spaces before it. Separators and every segment but the last use `Muted`. The last uses `Current` when focused, otherwise `CurrentDim`.

- [ ] **Step 4: Run the tests and check they pass**

Run: `JIG_UPDATE_GOLDEN=1 go test ./internal/bubbles/breadcrumb/...` once to create the goldens, inspect them, then run `go test ./internal/bubbles/breadcrumb/... ./internal/ui/theme/...`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat(breadcrumb): path widget with slk-style truncation"`

---

### Task 3: `details.WithoutHeader`

**Files:**
- Modify: `internal/bubbles/details/details.go`, `details_test.go`, `testdata/golden/`

**Interfaces:**
- Produces: `func WithoutHeader() Option`, and `func (m Model) Header() string`, which returns the content's header so the App can use it as the pane title. With `WithoutHeader`, `View` has no header or rule rows, the body is all `h` rows, and `BodyOrigin()` returns `(0, 0, true)` when `w > 0 && h > 0`.

- [ ] **Step 1: Write the failing tests**

- `TestWithoutHeader_BodyFillsPane`: content with 5 lines at 20×3 gives exactly 3 rows, and the first row starts with the first body line.
- `TestWithoutHeader_ScrollBy`: `maxScroll` is `len(lines) - h`.
- `TestWithoutHeader_BodyOrigin`: returns `(0, 0, true)`.
- `TestHeader_ReturnsContentHeader`.
- A golden, `details_without_header`.
- All existing tests stay unchanged.

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/bubbles/details/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Add a `noHeader bool` field. `bodyHeight()` is `h` when it is set, otherwise `h - 2`.

- [ ] **Step 4: Run the tests and check they pass**

Run: `go test ./internal/bubbles/details/...`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat(details): WithoutHeader option for a caller-drawn header"`

---

### Task 4: Extract `pane` from `sessionState` (a pure refactor)

**Files:**
- Create: `internal/ui/pane.go`
- Modify: `internal/ui/session.go`, `app.go`, `widgets.go`, `mode_normal.go`, `permissions.go`, `send.go`, `sidebar.go`, `themestate.go`, `pickerlevels.go`, `mode_picker.go`, `images.go`, `detailsctl.go`, and the test helpers that read `a.w.list` or `a.sess.proj`

**Interfaces:**
- Produces:
  ```go
  type paneKind int
  const (paneTranscript paneKind = iota; paneDetails)
  type pane struct {
      kind paneKind; session core.SessionID; proj *transcript.Projection
      list blocklist.Model; body details.Model
      versions map[transcript.BlockID]int; dirty idSet; live map[transcript.BlockID]bool; times blockTimes
      listW, listH, pendingW, resizeGen int
      title string; owner *pane; forBlock transcript.BlockID; gen int
      loading bool; buffered []event.Event
  }
  func newTranscriptPane(proj *transcript.Projection, r *renderer, set *theme.Set) *pane
  func (p *pane) resetBlocks()
  func (p *pane) items(ids []transcript.BlockID, frame int) []blocklist.Item
  func (p *pane) allItems(frame int) []blocklist.Item
  func (p *pane) item(b transcript.Block, frame int) blocklist.Item
  func (p *pane) data(b transcript.Block, frame int) blockData
  func (p *pane) tick() []transcript.BlockID         // dirty then live, sorted; frame is advanced by the caller
  func (p *pane) withDirty(ids []transcript.BlockID) []transcript.BlockID
  func (p *pane) timeTools(ev event.Event, ids []transcript.BlockID, now time.Time)
  func (p *pane) dropUser(id transcript.BlockID, frame int) []blocklist.Item
  ```
  `sessionState` loses `proj`, `versions`, `dirty`, `live`, and `times`, and gains `main *pane`. `sessionState.apply` and `load` operate on `s.main`.
  `widgets.list` moves to `sess.main.list`. `widgets.upsert` and `setItems` take a `*pane` and keep counting `upserts` and `gen`: `func (w *widgets) upsert(p *pane, items []blocklist.Item)` and `func (w *widgets) setItems(p *pane, items []blocklist.Item)`. `viewState.listW`, `listH`, `pendingW`, and `resizeGen` move onto `pane`.
  `App.flush(ids)` becomes `a.flushPane(a.sess.main, ids)`, keeping a thin `flush` wrapper for main.
  `freshSession` builds a new `main` pane and reuses the old `list`'s styles; the list itself stays one `blocklist.Model` per pane.

- [ ] **Step 1: Make the move with no behaviour change**

The existing suite is the test. There is no new test, because a pure refactor must keep every golden byte-identical.

- [ ] **Step 2: Run the whole suite**

Run: `go test -race ./internal/ui/... && go test -run XXX -bench 'App_|Blocklist_' -benchmem ./internal/ui ./internal/bubbles/blocklist`
Expected: PASS with no golden diffs, and every benchmark within its AGENTS.md budget.

- [ ] **Step 3: Run `make check`**

Expected: PASS. `pane.go` and `session.go` are each ≤ 500 lines.

- [ ] **Step 4: Commit**

`git commit -m "refactor(ui): extract pane (projection + list + render bookkeeping) from sessionState"`

---

### Task 5: Live child panes (`kids`), load, and replay

**Files:**
- Create: `internal/ui/kids.go`, `internal/ui/kids_test.go`
- Modify: `internal/ui/transcript/projection.go` (plus a test for `AddNotice`), `internal/ui/apptest_test.go` (`recSessions.Messages`)
- Modify: `internal/ui/app.go` (`onEvent`, `onResult`, `onPortResult` for resume/switch), `session.go` (`adopt` and `load` clear kids via a callback, or the App clears them after calling them)

**Interfaces:**
- Consumes: `transcript.NewChild`, `pane`.
- Produces:
  ```go
  // App field: kids map[core.SessionID]*pane
  type childLoadedMsg struct{ session core.SessionID; gen int; msgs []core.Message; err error }
  func (a *App) kid(child core.SessionID, agent, desc string) (*pane, tea.Cmd) // get or create+load
  func (a *App) routeKids(ev event.Event) []kidChange // apply ev to every non-loading kid; buffer for loading ones
  type kidChange struct{ p *pane; ids []transcript.BlockID }
  func (a *App) childLoaded(msg childLoadedMsg) tea.Cmd
  func (a *App) clearKids()
  func replayable(ev event.Event, loaded map[core.MessageID]bool, lastStatus core.MessageStatus) bool
  ```
  - `replayable` returns false when:
    - the event's `MessageID` is non-empty and is in `loaded`;
    - `ev` is `RunFailed` and `lastStatus` is `StatusFailed` or `StatusInterrupted`.

    Otherwise it returns true. The event types that carry a `MessageID` are `MessageStarted`, `TextDelta`, `ReasoningDelta`, `ToolCallStarted`, `ToolCallFinished`, `StepFinished`, and `RunFinished`.
  - A pane's title is `ansi.SanitizeLine("↳ " + agent + ": " + desc)`.
  - The load uses `sessionMessagesCmd`.
  - An error adds a notice to the pane's projection. `Projection` has no public notice API, so add `func (p *Projection) AddNotice(text string, level Level) BlockID` to `transcript` in this task, with a test.

- [ ] **Step 1: Fix the fake, then write the failing tests in `kids_test.go`**

`recSessions.Messages` (in `apptest_test.go`) returns the root's `f.msgs` for any ID it doesn't know. With loading on spawn, every child pane would then load the root's history. Change it so an ID that is neither `f.info.ID` nor a key in `otherMsgs` returns `nil, nil`, then check that the existing suite still passes before going on.

These tests use `newTestApp`, `sendAndAdopt`, `startSubagent`, `childBase`, and `withSessions`. In the test harness, Cmds run synchronously, so a load on spawn resolves at once. To hold a load in flight, a test calls `p, cmd := ta.app.kid("ses_c", "explore", "find the config")` directly, delivers events (they are buffered because `p.loading`), and only then calls `ta.run(cmd)`.

```go
func TestKids_SpawnCreatesLivePane(t *testing.T)
// startSubagent; then TextDelta{childBase,"k1","hi"} and ToolCallStarted{childBase,"k1",bash "c1"}.
// kids["ses_c"].proj.Blocks() kinds == [KindText, KindTool]; title == "↳ explore: find the config".
// The root projection is unchanged: no child text blocks in main.

func TestKids_LoadThenReplayNoDuplicates(t *testing.T)
// withSessions: ses_c has a stored message "k1" (text "old", StatusComplete).
// Call ta.app.kid directly (see above) and hold its Cmd. Deliver
// TextDelta{childBase,"k1","old"} (to be dropped) and TextDelta{childBase,"k2","new"}
// (to be replayed), then ta.run(cmd).
// Blocks text == ["old","new"].

func TestKids_RunFailedReplayedOnlyIfNotLoaded(t *testing.T)
// case A: the last stored msg is StatusFailed, and RunFailed is buffered (held Cmd, as above): exactly one "run failed" notice.
// case B: the last stored msg is StatusComplete, and RunFailed is buffered: exactly one notice.

func TestKids_LoadErrorShowsNotice(t *testing.T)
// Messages(ses_c) returns an error: the pane has one notice whose text starts "could not load subagent: ".

func TestKids_GrandchildGetsOwnPane(t *testing.T)
// A child task call plus SubagentSpawned{childBase, Child "ses_g"}: kids has "ses_g".
// A ses_g ToolCallStarted updates kids["ses_c"]'s t/<call> Sub.Tools == 1 and adds a
// block to kids["ses_g"].

func TestKids_ClearedOnResumeAndSwitch(t *testing.T)
// After a resumeMsg for a different session, len(a.kids) == 0.

func TestReplayable(t *testing.T) // table of the rules above
```

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/ui -run 'Kids|Replayable'`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `onEvent` calls `routeKids` after `sess.apply`, for every event whose `Root()` is the current root.
- On `SubagentSpawned` it calls `a.kid(e.Child, e.Agent, e.Description)`. It does that *after* `routeKids`, so the spawn updates the parent pane first.
- `childLoadedMsg` is handled in `onResult`. A message whose `gen` is stale is dropped.
- Upserting into a pane's list happens only while the pane is in the column. Until Task 7, `routeKids` just returns its changes, and nothing is upserted.

- [ ] **Step 4: Run the tests and check they pass**

Run: `go test -race ./internal/ui/...`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat(ui): live child panes for every spawned subagent, with load+replay"`

---

### Task 6: The column replaces the details split

**Files:**
- Create: `internal/ui/column.go`, `internal/ui/column_test.go`
- Modify: `layout.go`, `layout_test.go`, `app.go` (`View`, `relayout`, `viewState`), `mode_normal.go`, `detailsctl.go`, `images.go`, `mode_picker.go`, `themestate.go`, `widgets.go` (add `crumb breadcrumb.Model`, `mainCrumb breadcrumb.Model`), `normal_test.go`, and the details goldens

**Interfaces:**
- Consumes: `breadcrumb`, `details.WithoutHeader`, `pane`.
- Produces:
  ```go
  type focus int
  const (focusMain focus = iota; focusColumn)
  // App fields: col []*pane; focus focus
  func (a *App) columnOpen() bool
  func (a *App) top() *pane                        // nil when the column is closed
  func (a *App) focused() *pane                    // a.sess.main, or top() when focusColumn and it's a transcript pane
  func (a *App) entryFor(owner *pane, id transcript.BlockID) (*pane, tea.Cmd) // details pane, or a kid pane for a subagent with a Child
  func (a *App) push(p *pane) tea.Cmd
  func (a *App) pop()
  func (a *App) closeColumn()
  func (a *App) replaceColumn(p *pane) tea.Cmd
  func (a *App) crumbSegments() []string           // ["main", col[0].title, …]
  func (a *App) columnView() string                // crumb row + rule row + top body
  ```
  - `computeLayout(w, h, promptH int, sidebarPref *bool, columnOpen bool, focus focus) rects`.
    - When narrow and the column is open: if `focus == focusColumn`, `Side` takes the whole region and `Transcript.W == 0`. If `focus == focusMain`, `Transcript` takes the region with `MainCrumb` set, and `Side.W == 0`.
    - `rects` gains `ColumnOpen bool` (renamed from `DetailsOpen`) and `MainCrumb bool`. When `MainCrumb` is true, compose puts two rows at the top of the transcript rect: the main breadcrumb and the rule.
  - `detailsMsg` gains `pane *pane` and `gen int`. `detailsCtl.result` applies content only when `msg.pane` is still in `col` and `msg.gen == msg.pane.gen`. It sets `pane.body` content and sets `title` from `Content.Header`, sanitized by `header()` already.
  - Remove `detailsCtl.childEvent`, `refresh`, `shownSubagent`, `streamState.subStale`, `buildSubagentDetails`, `subagentLines`, `oneLiner`, `TestDetails_SubagentSummary`, and `viewState.detailsOpen`/`detailsFor`.
  - `placeSixel` uses `a.lay.Side` plus 2 rows for the breadcrumb and rule, plus `top().body.BodyOrigin()`, and only when `top().kind == paneDetails`.

- [ ] **Step 1: Write the failing tests**

In `layout_test.go`:
- `TestLayout_ColumnWide`: 160 wide, column open, `focusMain`. `Side.W == 80`, `Transcript.W == 80`, `!SideVisible`.
- `TestLayout_ColumnNarrowFocus`: 100 wide. With `focusColumn`, `Side.W == 100` and `Transcript.W == 0`. With `focusMain`, `Transcript.W == 100`, `Side.W == 0`, and `MainCrumb`.
- Keep the existing table cases, with `DetailsOpen` renamed to `ColumnOpen`.

In `column_test.go`:
- `TestColumn_EnterOpensDetailsEntry`: `sendAndAdopt`, bash block, `esc`, `enter`. `len(a.col) == 1`, `top().kind == paneDetails`, and the stripped view contains `main › bash · `.
- `TestColumn_EnterSameBlockCloses`: `enter` twice gives `len(col) == 0`.
- `TestColumn_FollowsMainCursor`: with one details entry and main focused, `k` re-points `top().forBlock` to the new block.
- `TestColumn_EscFromMainCloses` and `TestColumn_QPopsWhenColumnFocused` (the latter completes in Task 7; here, `q` from main closes).
- `TestColumn_StaleDetailsMsgDropped`: open A, then open B, then deliver A's `detailsMsg`. The content is still B's. This replaces `TestNormal_StaleDetailsMsgIgnored`.
- `TestColumn_ClosedOnSessionSwitch` (Review Focus 4): open the column, `send(resumeMsg{info: other})`. `len(col) == 0`, `len(kids) == 0`, `focus == focusMain`, and the view has no `main ›`.
- `TestColumn_PopsWhenBlockDropped` (spec §6): open a details entry on a pending user block (`u/pending/0`, a send with a queued failure via `chat.errs`), then `returnSend` so `dropUser` runs. `len(col) == 0`.
- `TestLayout_ColumnTiny`, in `layout_test.go`: at 20×4 with the column open, no rect is negative, and `View()` has exactly 4 rows.
- Goldens: `app_details_column` at 120×30, and the regenerated `normal_details_open` and `normal_narrow_details`.

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/ui -run 'Layout|Column|Normal'`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `enter`:
  - Main focused: if `top() != nil && col[0].forBlock == selected`, close. Otherwise `replaceColumn(entryFor(main, selected))`.
  - A details pane is sized with `computeLayout(..., true, a.focus)`'s `Side`, minus 2 rows.
- `q`/`esc` from main: close the column if it is open, else clear the search.
- The main cursor following the column: in `syncDetails`, when `focus == focusMain && len(col) == 1 && col[0].kind == paneDetails`, rebuild it.
- `ctrl+e` and `ctrl+y` scroll `top().body` when it is a details pane.
- `pushTheme` sets the styles of `crumb`, `mainCrumb`, and every details pane in `col`.
- `View` draws the side as `columnView()` when the column is open, and prefixes main's crumb rows when `lay.MainCrumb`.

- [ ] **Step 4: Run the tests and check they pass**

Run: `JIG_UPDATE_GOLDEN=1 go test ./internal/ui -run 'Golden|Column'`, inspect the diffs (only the header moving into the breadcrumb), then `go test -race ./internal/ui/...` and `go test -run XXX -bench DetailsToggle2000 ./internal/ui`.
Expected: PASS, and `DetailsToggle2000` < 50 ms/op.

- [ ] **Step 5: Commit**

`git commit -m "feat(ui): the column — a breadcrumbed stack replacing the details split"`

---

### Task 7: Subagent panes in the column, focus keys, and the narrow takeover

**Files:**
- Modify: `column.go`, `mode_normal.go`, `app.go` (`onTick`, `relayout`, `resizeMsg`), `themestate.go`, `actions/keymap.go` and its tests, `column_test.go`
- Delete and rewrite: `TestApp_SubagentDetailsRefreshPerTick` and `TestApp_SubagentDetailsRefreshKeepsScroll` in `permissions_test.go`, rewritten as `TestColumn_ChildStreamsPerTick` and `TestColumn_ChildKeepsScroll`

**Interfaces:**
- Consumes: `kids`, `a.kid`, `routeKids`, and everything from Task 6.
- Produces:
  - `resizeMsg{gen int; pane *pane}`
  - `func (a *App) setFocus(f focus)`: this also calls `SetHighlight(mode == modeNormal && focused)` on the main list and on the top transcript pane's list.
  - `a.focused()` now returns the top transcript pane when the column has focus.
  - `routeKids` changes to transcript panes in `col` are upserted: deltas are marked dirty, everything else is upserted at once.
  - `isFixed("normal", "h"|"l") == true`.

- [ ] **Step 1: Write the failing tests**

```go
func TestColumn_EnterOnSubagentPushesLivePane(t *testing.T)
// startSubagent; child TextDelta "**bold** child"; esc; select t/c9; enter.
// top().kind == paneTranscript, top().session == "ses_c", focus == focusColumn.
// The stripped view contains "main › ↳ explore: find the config" and "bold child"
// (markdown rendered, with no "**").

func TestColumn_ChildStreamsPerTick(t *testing.T)
// With the pane open: 3 child TextDeltas give top().list upserts == 0 until fire();
// after fire(), exactly 1 upsert for the child pane, and the view shows the text.
// Messages("ses_c") was called exactly once in total (the load on spawn), never again.

func TestColumn_ChildKeepsScroll(t *testing.T)
// A 60-line child text; ctrl+u/k to scroll up in the column; a new child delta plus fire;
// the first visible row is unchanged.

func TestColumn_TabHLFocus(t *testing.T)
// Open a subagent pane: focus == focusColumn. tab → focusMain; tab → focusColumn;
// h → focusMain; l → focusColumn. With the column closed, tab cycles the agent (the agent changes).

func TestColumn_NestedDrill(t *testing.T)
// A grandchild spawn inside ses_c; in the column, select the child's subagent block and press enter.
// len(col) == 2; the breadcrumb is "main › ↳ explore: find the config › ↳ general: deeper".
// esc → len(col) == 1 and focus is still the column; esc → len(col) == 0 and focus == focusMain.

func TestColumn_EnterChildToolPushesDetails(t *testing.T)
// In the child pane, select the bash block and press enter: top().kind == paneDetails,
// the breadcrumb ends "bash · ls", and esc returns to the child pane with its selection intact.

func TestColumn_NarrowTakeover(t *testing.T) // withSize(100,30)
// Open the subagent: the view has no main blocks, only the child's. tab: main's blocks
// plus a crumb row containing "tab → ↳ explore: find the config".

func TestColumn_FocusedPaneYankSearch(t *testing.T) // Review Focus 5
// In the child pane: y copies the child block's text (a SetClipboard cmd whose text is the child's);
// "/", "child", enter sets the search on top().list, and main's search stays "".

func TestColumn_ThemeRestylesChildPane(t *testing.T) // Review Focus 1
// Open the pane; apply another theme (themeApplyMsg); the child pane's items' versions
// were bumped, and View differs from before in the child region.

func TestColumn_ResizeAppliesToTopPane(t *testing.T) // Review Focus 2
// Open the pane at 160 wide; WindowSizeMsg 200; fire; top().listW == 100.
// Then pop the pane and deliver its stale resizeMsg: no panic, and main's width is unchanged.

func TestColumn_ChildFinishStopsSpinners(t *testing.T) // Review Focus 3
// Open the pane with a running child bash; child ToolCallFinished plus RunFinished{childBase};
// then fire: len(top().live) == 0. Root RunFinished plus returnSend: no further streamTick scheduled.

func TestColumn_SubagentWithoutChildShowsHeaderOnly(t *testing.T)
// A root task ToolCallStarted with no SubagentSpawned yet; esc, enter: top().kind == paneDetails,
// the breadcrumb ends "subagent · explore · find the config", and no Messages call is made.

func TestColumn_GoldenWide(t *testing.T)   // "app_subagent_view_wide" at 160x30
func TestColumn_GoldenNarrow(t *testing.T) // "app_subagent_view_narrow" at 100x30
func TestColumn_GoldenNested(t *testing.T) // "app_subagent_nested" at 160x30
```

In `actions/keymap_test.go`: `[keybinds] "normal.h"` yields a fixed-key warning and is skipped.

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/ui/... -run 'Column|Keymap'`
Expected: FAIL.

- [ ] **Step 3: Implement**

- NORMAL keys route to `a.focused()`: the `navigate`, `gotoTop`, search, `yank`, `n`/`N`, and `gp` paths take a `*pane` instead of `a.w.list` and `a.sess.main.proj`.
- On a details pane, `j`/`k` call `body.ScrollBy(±1)`.
- `tab`, `shift+tab`, `h`, and `l` are handled before `Keymap.Lookup` while `columnOpen()`.
- `onTick` ticks main and each transcript pane in `col` with the shared `run.frame`.
- `relayout` sizes `top()` to `lay.Side` minus 2 rows. It uses the same debounce as main, keyed by `top().resizeGen`.
- Pushing a pane calls `SetItems(allItems)` and `SetStyles` with the current set and version.

- [ ] **Step 4: Run the tests and check they pass**

Run: `JIG_UPDATE_GOLDEN=1 go test ./internal/ui -run Golden`, inspect the goldens, then `go test -race ./internal/ui/...` and `make lint`.
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat(ui): drill into subagents — live child transcripts, tab/h/l focus, narrow takeover"`

---

### Task 8: Permission cards in the focused pane

**Files:**
- Modify: `internal/ui/permissions.go`, `widgets.go` (`cardAt.pane`, and `withCard` checks the pane), `permissions_test.go`

**Interfaces:**
- Consumes: `a.focused()`, `a.top()`, and `kids`.
- Produces:
  - `cardKey{pane *pane; block BlockID; ver, width int}`.
  - `permCtl` methods take their list and projection from a `*pane`.
  - `target()` follows spec §4.3's order.
  - `requested(e)`: when `top()` is a transcript pane with `session == e.Session()`, it focuses the column and selects `t/<e.Call.ID>` in `top()`. Otherwise the rule is unchanged, on main.

- [ ] **Step 1: Write the failing tests**

- `TestColumn_ChildPermissionCardOnChildToolBlock`:
  - Setup: open the child pane, then child bash `c1` plus a `PermissionRequested{childBase, "p1", Call{ID:"c1"}}`.
  - Assert that `focus == focusColumn`, the child pane has `t/c1` selected, and after `arm` the column view contains `wants to run bash`.
  - Assert that `a` answers it: `perms.replies[0].id == "p1"`.
  - Assert that main's `t/c9` shows ⚠ until the `PermissionResolved`, and that the card is drawn once, in the column and not in main.
- `TestColumn_ChildPermissionWhenNotOpen`: the existing behaviour. With the column closed, the card is on main's `t/c9`, and `TestApp_SubagentPermissionSelectsOwner` still passes.
- `TestColumn_GpInFocusedPane`: two pending requests in the child pane; `gp` cycles between the child's blocks.
- Regenerate `app_subagent_permission` only if it changes; it shouldn't.

- [ ] **Step 2: Run the tests and check they fail**

Run: `go test ./internal/ui -run 'Permission|Gp'`
Expected: FAIL.

- [ ] **Step 3: Implement** the interfaces above. `sync` flushes the old pane's block and the new pane's block through `flushPane`.

- [ ] **Step 4: Run the tests and check they pass**

Run: `go test -race ./internal/ui/...`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat(ui): permission cards in the focused pane, on the child's own tool block"`

---

### Task 9: The benchmark, the e2e test, and docs

**Files:**
- Modify: `internal/ui/app_bench_test.go`, `e2e/tui_test.go`, `AGENTS.md`

- [ ] **Step 1: Add `BenchmarkApp_SubagentStream`**

Set up a resumed 2,000-block root (reuse the existing bench setup) at 160×40, with a spawned `ses_c` whose pane is open and holds 200 blocks. Each iteration is one child `TextDelta` followed by `onTick` then `View`. Report `ms/op`.

Run: `go test -run XXX -bench SubagentStream -benchmem ./internal/ui`
Expected: < 3 ms/op. If it misses, fix the algorithm (only the top pane may render; main must render nothing), never the budget.

- [ ] **Step 2: Add `TestE2E_TUISubagentView` to `e2e/tui_test.go`**

The script is:
- m1: `task` call with `{"agent":"explore","description":"look around","prompt":"find"}`, then the text `parent done`.
- m2: the text `**child** found it`, under `[agents.explore] model = "jigtest/m2"`.

The test runs at 160×30 and does the following:
1. `t` to trust the project.
2. `delegate\r`.
3. Wait for `parent done`.
4. `\x1b` (esc).
5. `gg` to select the first block (the user prompt), then `j` to select the subagent block, which comes right after it.
6. `\r`, then wait for `main › ↳ explore: look around` and for `child found it`.
7. `\x1b` to pop, then wait for the breadcrumb to disappear by waiting for `parent done` with a new mark.
8. `\x04` to quit, and check the exit code is 0.

Run: `make test`
Expected: PASS.

- [ ] **Step 3: Update `AGENTS.md`**

- **Architecture tree:** add `internal/bubbles/breadcrumb/`.
- **"The TUI" / NORMAL:**
  - the column (a stack, a breadcrumb, wide/narrow);
  - `tab`/`shift+tab`/`h`/`l` focus while the column is open;
  - `enter` push/replace/close;
  - `q`/`esc` pop.
- **Shared-code table:**
  - `transcript.NewChild(root, child)`;
  - `breadcrumb.New` / `SetSegments` / `View`;
  - `a.focused()` / `a.top()`, "the pane keys act on";
  - `pane` (`items`, `tick`).
- **Invariants:**
  - "The App owns `kids` and `col`; child panes are created on `SubagentSpawned` (always loaded, then buffered events replayed per `replayable`) and cleared whenever the root projection is rebuilt."
  - Update the permission-card invariant to "the focused pane's selected block's, else its first, else main's".
- **Budgets table:** the `BenchmarkApp_SubagentStream` row with its measured value.

- [ ] **Step 4: Run `make check`**

Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "test(ui): subagent stream benchmark and TUI e2e; docs(agents): subagent view"`
