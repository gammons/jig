# jig: Subagent View (Drill-In Column with Breadcrumb)

- **Status:** draft, awaiting review
- **Date:** 2026-09-29
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md` (Plan 2, implemented).
- **Changes Plan 2:** it replaces the details split (§5.4) with a stack of right-hand views (the "column"), replaces the subagent details summary with a live child transcript, and redefines NORMAL `tab`/`shift+tab`/`q`/`esc` while the column is open (§6.3).
- **Interacts with:** `docs/superpowers/specs/2026-09-29-mouse-scroll-select-design.md` (not yet implemented). Wherever that spec says "the details split", read it as "the column's top entry". A subagent view in the column is a blocklist, so the mouse spec's transcript rules apply to it.
- **Prior art:** slk's thread view: `internal/ui/panellayout.go` (`Compute`), `internal/ui/thread/breadcrumb.go` (`renderBreadcrumb`), and `CloseThread` in `internal/ui/app.go` (MIT, same author).

## 1. Intent

**What the author asked for**

- A subagent should render with the same view as the main screen: the same block renderer, markdown, styled tool lines, reasoning, spinners, durations, edit counts, and permission cards. Today a subagent's details are a flat, unstyled list of text lines.
- A breadcrumb at the top of the subagent view, like slk's thread view.
- On a wide terminal, split the screen: main on the left, the subagent view on the right. On a narrow terminal, the subagent view takes over the main panel.

**Decisions made in review**

1. **Drill-in, not just a restyled details split (option B).** A subagent view is a full transcript. It has its own block cursor, `j/k/gg/G`, search, and `enter` to open a child block's details. Nested subagents drill further in.
2. **Focus follows slk.** `tab` switches focus between main and the column. `h` focuses main and `l` focuses the column. `enter` on a child block pushes its details onto the column, and `q`/`esc` pops back.
3. **One mechanism.** The old details split becomes the column. A details entry and a subagent entry are two kinds of the same stacked pane.
4. **A live projection per spawned subagent.** The App folds each child's bus events into that child's own projection from the moment the child is spawned. It does not read the store on open, because that would miss the child's step still in flight.

**Success criteria**

- While a subagent is running, `enter` on its block shows the child's streaming markdown text, reasoning spinner, and tool lines with spinners and durations. They look exactly as they would in main.
- Wide terminal: main and the column are side by side, and the breadcrumb reads `main › ↳ explore: find the bug`. Narrow terminal: only the focused pane is drawn.
- `tab`, `h`, and `l` move focus. Only the focused pane highlights its selection, and the breadcrumb shows which pane has focus.
- A permission request from the child shows its card under the child's own tool block in the column. It still shows on the owning subagent block in main too, and answering either one resolves it.
- Opening a subagent that finished before this App started, or one from a resumed session, shows its full transcript loaded from the store.
- The performance budgets in AGENTS.md still pass, and the new `BenchmarkApp_SubagentStream` passes too.

## 2. Scope

**In scope**

- `transcript.NewChild`: a projection of one descendant session.
- The column: a stack of transcript and details panes, with a breadcrumb.
- Focus between main and the column; the wide and narrow layouts.
- Routing child events live, loading on open, and replaying events that arrived while the load was in flight.
- Permission cards in child panes.
- A new `internal/bubbles/breadcrumb` widget, and a `WithoutHeader` option on `details`.

**Out of scope**

- Sending prompts to a subagent. The prompt always sends to the root.
- More than two panes on screen at once.
- Mouse support. That comes from the mouse spec, applied to the column.
- Status bar or sidebar changes. Both keep showing the root.
- Cancelling a single subagent. `ctrl+c` still cancels the root run.

## 3. Components

### 3.1 `transcript.NewChild`

`transcript.NewChild(root, child core.SessionID) *Projection` builds a projection whose own session is `child`.

- `Projection` gains a `self core.SessionID` field: `New(root)` sets it to `root`, and `NewChild` sets it to `child`.
- `Apply` still ignores events whose `Root()` isn't `root`.
- Every current `ev.Session() != p.root` check becomes `ev.Session() != p.self`. `self`'s events therefore create text, reasoning, tool, and notice blocks, and end steps and runs, exactly as a root's do.
- `lineage.spawn` and `permissions.request` receive `self` where they receive `root` today. `self`'s spawns then attach to its own subagent blocks, and its permission requests attach to its own tool blocks.
- Events from sessions below `self` update `self`'s subagent blocks through `lineage`, as the root projection does.
- **Any event from outside the subtree is dropped at the top of `Apply`.** The subtree is `self` plus every session in `lineage.owner`. This applies to every event kind, including `PermissionRequested`. Without it, `permissions.request` would list a sibling's or the root's request as pending here, because it lists a request even when the request has no block. For the root projection this check never fires, since everything under `root` is in its subtree.
- `Load` is unchanged; it takes the child's stored messages.
- `New(root)` behaves exactly as it does now.

The package stays stdlib plus `internal/core/...` only.

### 3.2 `pane` (`internal/ui/pane.go`)

A `pane` is one transcript or details view:

```go
type paneKind int // paneTranscript, paneDetails

type pane struct {
    kind     paneKind
    session  core.SessionID            // paneTranscript: the session shown
    proj     *transcript.Projection    // paneTranscript
    list     blocklist.Model           // paneTranscript
    body     details.Model             // paneDetails
    versions map[transcript.BlockID]int
    dirty    idSet
    live     map[transcript.BlockID]bool
    times    blockTimes
    listW, listH, pendingW, resizeGen int
    title    string                    // breadcrumb segment, pre-sanitized
    owner    *pane                     // paneDetails: the pane whose block it shows
    forBlock transcript.BlockID        // paneDetails: that block
    gen      int                       // async-result generation
    loading  bool                      // paneTranscript: a store load is in flight
    buffered []event.Event             // events received while loading
}
```

These methods move from `sessionState` to `pane`: `items`, `allItems`, `item`, `data`, `tick`, `withDirty`, `timeTools`, `thinking`, and `dropUser`. `data` takes the spinner frame as an argument, because the frame belongs to the root run.

After the move, `sessionState` holds only root-level state: `info`, `model`, `run`, `queued`, `usage`, `cost`, `todos`, `cat`, `attach`, `clk`, plus `main *pane`.

### 3.3 The column (`internal/ui/column.go`)

- `App.col []*pane` is the column stack. Only `col[len-1]` is drawn. The breadcrumb path is `main › col[0].title › … › col[len-1].title`.
- `App.focus` is `focusMain` or `focusColumn`.
- `App.kids map[core.SessionID]*pane` is the live pane for every subagent session known under the current root. The App is its one owner. `kids` is cleared whenever the root projection is rebuilt: a session switch, `load`, or `adopt`.
- **Pane titles**, each passing `ansi.SanitizeLine`:
  - subagent pane: `↳ <agent>: <description>`
  - details pane: the header its build returns today, e.g. `edit · foo.go · 2 hunks`
- The details header is no longer drawn by `details.Model`: the new `details.WithoutHeader()` option drops its header and rule rows, and `BodyOrigin` returns `(0, 0)`. The column's breadcrumb and rule rows take their place.
- `viewState.detailsOpen` becomes `len(a.col) > 0`, and `viewState.detailsFor` becomes the top entry's `forBlock`.
- `detailsCtl.childEvent`, `detailsCtl.refresh`, `streamState.subStale`, `buildSubagentDetails`, `subagentLines`, and `oneLiner` are removed.

### 3.4 `internal/bubbles/breadcrumb` (new widget)

**API:** `New(opts ...Option)`, `WithStyles(Styles)`, `SetStyles(Styles)`, `SetSegments([]string)`, `SetHint(string)`, `SetFocused(bool)`, `SetWidth(int)`, `View()`.

**`View` output:** exactly one row, `width` cells wide.

**Styles:**
- `Muted`: earlier segments and the ` › ` separators.
- `Current` (bold accent): the last segment when focused. `CurrentDim` when not focused.
- `Hint`: muted text on the right.
- `Rule` and `RuleFocused`: the colour the App uses for the rule row below the breadcrumb.

**Truncation order**, applied until the row fits:
1. Drop the hint.
2. Collapse middle segments one at a time, from the left, into a single `…` segment. `main` and the last segment are always kept.
3. Cut the last segment with `…`.
4. Cut the whole row hard.

**Theme:** it gets a `theme.Set.Breadcrumb` field, a `breadcrumbStyles(p Palette)` builder with `TestBuild_BreadcrumbFromPalette`, and a `SetStyles` line in `pushTheme`.

**Imports:** stdlib, `charm.land/...`, and `internal/bubbles/ansi` only.

## 4. Data flow

### 4.1 Child panes, live

- **On `SubagentSpawned` under the current root:** if `kids[e.Child]` doesn't exist, the App creates `pane{kind: paneTranscript, session: e.Child, proj: transcript.NewChild(root, e.Child)}`.
  - This covers grandchildren as well, because their spawn events carry the same root.
  - The pane's title comes from `e.Agent` and `e.Description`.
- **`onEvent` routing for each event under the current root:**
  1. `a.sess.main` applies it, as today.
  2. Every pane `p` in `kids` is offered the event, because a child pane needs its descendants' events to update its own subagent blocks:
     - While `p.loading`, the event is appended to `p.buffered`.
     - Otherwise `p.proj.Apply(ev)` runs. It ignores events outside `p`'s subtree (§3.1). Deltas mark blocks dirty, as for the root.
     - Blocks the event changed are upserted into `p.list` only when `p` is in `a.col`.
  3. A spawn's event belongs to the parent session, so step 2 hands it to the parent's pane. There it updates the parent's subagent block, and `kids` gets the new child from the rule above.

  **Cost:** offering an event to a pane outside its subtree is one map lookup, so each event costs O(number of known subagents).
- **Panes not in the column** fold events into their projection and track dirty and live IDs. They never build `blocklist` items.
- **Pushing a pane onto the column** calls `p.list.SetItems(p.allItems())`.
- **`onTick`** ticks `a.sess.main` and every transcript pane in `a.col`, sharing the root run's spinner frame. It keeps rescheduling for as long as the root run is running.

### 4.2 Loading on open

This covers two cases:
- opening a subagent block whose `Sub.Child` has no entry in `kids` (a resumed session, or a subagent that finished before this App started);
- a `SubagentSpawned` for a child that already has history (a resumed `task`).

In both, the App does the following:
1. Creates the pane with `loading = true` and bumps its `gen`.
2. Issues `sessionMessagesCmd(child)`, which resolves to `childLoadedMsg{session, gen, msgs, err}`.
3. When the message arrives with a matching `gen`:
   - It runs `p.proj.Load(msgs)`.
   - It replays each event in `p.buffered` whose `MessageID` (for events that carry one) isn't among the loaded messages' IDs. Events without a `MessageID` are always replayed.
   - It clears the buffer and sets `loading = false`.
   - If the pane is in the column, it calls `SetItems`.
4. A load for a stale `gen` is dropped.

**Why the replay rule is sound.** The Runner saves a step's message *before* it publishes that step's `StepFinished` (`runner.go`).
- Any event whose message is in the loaded snapshot is already reflected in `Load`, so skipping it is correct.
- Any event whose message isn't in the snapshot belongs to a step still in flight, so replaying it is correct.
- Events with no `MessageID` are safe to replay:
  - `SubagentSpawned` sets the child on an existing block.
  - `PermissionRequested` targets a mid-step call, which by definition isn't saved yet.
  - A `PermissionResolved` for an unknown request is a no-op.
  - `RunFailed` is the exception. It has no `MessageID`, and `Load` already adds a notice for a message whose status is failed or interrupted. So a buffered `RunFailed` is dropped when the snapshot's last assistant message has either status; otherwise it is replayed.

  Tests cover `RunFailed` arriving both before and after the snapshot. Each checks that exactly one notice appears.

**Every new pane loads, including one created on spawn.** The App can't tell a new child from a resumed one, so it doesn't try. For a genuinely new child the load returns zero or one message, and the replay rule removes any overlap. The cost is one store read per spawn. While that read is in flight, the pane buffers its events, and nothing is lost.

### 4.3 Permissions

- The child projection receives the child's own `PermissionRequested` and `PermissionResolved`, and shows the request on the child's tool block. The root projection also shows it on the owning subagent block.
- There is still one `permcard`. `cardAt` gains `pane *pane`: the card renders under `cardAt.block` in `cardAt.pane` only.
- **`permCtl.target()`**, in order:
  1. The request currently being typed into.
  2. The focused pane's selected block's request.
  3. The focused pane's first pending request with a block.
  4. `main`'s first pending request with a block.
- **`permCtl.requested`:** if the request's session is shown by `a.col`'s top pane, focus moves to the column and that pane selects the tool block. Otherwise the rule is the same as today, applied to main.
- `gp` and `onCard` use the focused pane.
- The status bar's `Pending` still counts `a.sess.main.proj.Pending()`, which already includes descendants.

## 5. Layout, keys, focus

### 5.1 Layout

`computeLayout`'s `detailsOpen` argument becomes `columnOpen`. The geometry is unchanged: the column takes 50% of the width when wide (≥ 120 columns) and the whole transcript region when narrow. The sidebar is hidden while the column is open.

**The column's rows:** row 1 is the breadcrumb, row 2 is the rule, and rows 3 onward are the body. The body is the top pane's `list.View()` or `body.View()`.

**Narrow:**
- Only the focused pane is drawn.
- When main has focus and the column is open, main's region also carries a breadcrumb row, `main`, with the hint `tab → <top title>` on the right, and a rule row. Main's list loses those 2 rows.
- When the column has focus, it fills the whole region.

**Per-pane width:** each pane's list width is debounced separately (`resizeDebounce`, keyed by its own `resizeGen`). `resizeMsg` gains a `pane *pane` field, and a message for a pane no longer in `a.col` or `main` is dropped. Main keeps the existing `viewState.listW`/`listH`/`pendingW`, which move onto `a.sess.main`.

**Sixels:** placement uses the column's rect plus 2 rows plus the `details` body origin.

### 5.2 NORMAL keys

These are fixed keys: the mode handler runs them before `Keymap.Lookup`, and `actions.isFixed` adds `h` and `l`.

| Key | Column closed | Column open |
|---|---|---|
| `tab` / `shift+tab` | cycle agents (unchanged) | toggle focus main ↔ column |
| `h` / `l` | nothing | focus main / focus column |
| `enter`, main focused | open the column with an entry for the selected block | if the selected block is `col[0].forBlock`, close the column; otherwise replace the stack with a new entry for it |
| `enter`, column focused on a transcript pane | — | push an entry for the selected child block |
| `enter`, column focused on a details pane | — | nothing |
| `q` / `esc` | close nothing; clear the search | column focused: pop one entry, and if the column is now empty, focus main. Main focused: close the whole column |
| `j k gg G ctrl+u n N / y gp a A d D` | main | the focused pane. On a details pane, `j`/`k` scroll the body and the rest do nothing |
| `ctrl+e` / `ctrl+y` | — | scroll the top entry if it is a details pane, whatever has focus |

**"An entry for a block":**
- For a subagent block with `Sub.Child`, the entry is `kids[child]`, created and loaded (§4.2) if it doesn't exist yet.
- For any other block, it is a new details pane built with `buildDetails` at the column's size.
- A subagent block with no child yet (the spawn hasn't arrived) gets a details pane showing only its header, as today.

**Focus on push:**
- Pushing a subagent pane focuses the column.
- Pushing a details pane from main keeps focus on main. While main has focus and `len(col) == 1` with a details entry, moving main's cursor rebuilds that entry for the new block. This is today's behaviour, where the split follows the cursor.
- Pushing from inside the column keeps focus on the column.

**Highlight:** only the focused pane's list highlights its selection. That is `SetHighlight(mode == NORMAL && focused)`.

**Unaffected:** INSERT and PICKER, including `tab`/`shift+tab` cycling agents in INSERT.

## 6. Errors and edge cases

- **A child load fails:** the pane's projection gets one error notice, `could not load subagent: <err>`, sanitized. The pane stays navigable, and its buffered events are applied anyway.
- **The root is switched, loaded, or adopted:** `a.col` and `kids` are cleared, and focus returns to main.
- **The block under a details entry disappears** (for example, `dropUser` removes the user block): that entry and every entry above it are popped.
- **Stale async results:** each pane has a `gen`, and every `detailsMsg` and `childLoadedMsg` carries the pane and `gen` it was built for. A result that no longer matches is dropped, except that an image result is still cached, as today.
- **A tiny terminal:** a column under 3 rows shows only the breadcrumb row. `fit` clips everything, and a 0×0 frame renders empty.
- **Depth:** there is no depth limit. The breadcrumb collapses middle segments to stay readable.

## 7. Testing

TDD applies throughout: each test is written before its implementation, and golden files are regenerated with `JIG_UPDATE_GOLDEN=1`.

**`transcript`:**
- `NewChild` builds text, reasoning, and tool blocks from the child's events.
- It ignores the root's and siblings' events, including their permission requests: `Pending()` stays empty.
- A grandchild spawn becomes a subagent block, and the grandchild's tool calls update that block.
- A child `PermissionRequested` attaches to the child's tool block, and `PermissionResolved` clears it.
- Every existing `New` test stays green, unchanged.

**`bubbles/breadcrumb`:**
- Golden frames at 120, 60, and 30 columns, focused and unfocused, one per truncation step.
- A `SetStyles` test.
- `TestBuild_BreadcrumbFromPalette`.

**`bubbles/details`:** golden frames with `WithoutHeader`, and the new `BodyOrigin`.

**`ui/layout`:** wide and narrow rects with the column open, and the narrow main breadcrumb rows.

**`ui` App tests** (`newTestApp`):
- Drill into a running subagent: streamed child text renders through `mdrender` in the column.
- A nested drill-in, and the breadcrumb path it produces.
- `tab`, `h`, and `l` focus changes, and the narrow takeover with the hint.
- `esc` pops back to the previous details entry with its scroll intact.
- A resumed task: the load plus replay produces no duplicate blocks.
- A child permission card on the child's tool block, answered from the column.
- `gp` in the focused pane.
- Switching sessions closes the column.
- `enter` on the same block in main closes the column.
- Golden frames: `app_subagent_view_wide`, `app_subagent_view_narrow`, `app_subagent_nested`, `app_details_column`.

**Migrated tests:**
- The details split goldens are regenerated, because the header moves into the breadcrumb.
- `TestDetails_SubagentSummary` is removed.
- `TestApp_SubagentDetailsRefreshPerTick` and `TestApp_SubagentDetailsRefreshKeepsScroll` are rewritten against the live pane: one upsert per tick, and scroll is kept.

**Benchmarks:**
- The existing blocklist and App budgets must still pass.
- New `BenchmarkApp_SubagentStream`: a 2,000-block root plus a streaming child pane in the column, measuring one `onTick` with a dirty child block. Budget: < 3 ms/op.

**e2e** (`e2e/tui_test.go`): a scripted `task` call. The test presses `esc`, selects the subagent block, presses `enter`, then waits for the child's text and `main ›`.

**`make check`** must pass before every commit.

## 8. Docs

`AGENTS.md` gets:
- the new NORMAL keys and the column in "The TUI";
- `transcript.NewChild` and `breadcrumb` in the architecture tree and the shared-code table;
- a one-owner invariant: the App owns `kids` and `col`, and child panes are rebuilt only with the root projection;
- the `BenchmarkApp_SubagentStream` row in the budgets table.
