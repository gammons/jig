# Subagent View — Port onto `main` (e1a42ae)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Re-land the subagent view (PR #4, branch `subagent-view`, head `a0964c6`) on top of current `main`. `main` has since added tool-call groups (`itemTrack`/`foldState`/`foldCtl`), mouse wheel scrolling and selection (`mouse.go`), a margin/`fitRows` layout, MCP, and reasoning display. The result replaces `subagent-view` with a force-push, which the user approved.

**Spec:** `docs/superpowers/specs/2026-09-29-subagent-view-design.md` (in this worktree after Task P0). It is still the authority for behaviour. **Original plan:** `docs/superpowers/plans/2026-09-29-subagent-view.md`.

**Reference implementation:** the old branch. Read any of its files with `git show subagent-view:<path>`, and diff one of its tasks with `git show <sha>`. Its commits, in order:
- `3310a91` transcript.NewChild
- `2c09716` + `ae04798` breadcrumb
- `695002a` details.WithoutHeader
- `60a860a` + `e431884` pane refactor
- `edee4e5` kids
- `eb37d8d` + `04565dc` the column
- `2cedd94` drill-in and focus
- `e54a54b` narrow, resize, and theme
- `b0374db` permissions
- `95a8101` benchmark
- `2ec0b30` e2e and docs
- `a0964c6` title sanitization

Port the behaviour, not the text: `main`'s structure wins wherever the two differ.

## Decisions

- **D1: one pane type that contains `main`'s `itemTrack`.**

  ```go
  type pane struct {
      kind paneKind; session core.SessionID; proj *transcript.Projection
      list blocklist.Model; track itemTrack; times blockTimes
      sz listSize; title string; load kidLoad
      body details.Model; owner *pane; forBlock transcript.BlockID; buildGen int
  }
  ```

  - `itemTrack`'s methods, `groupMembers`, `data`, `timeTools`, `withDirty`, and `thinking` take a `*pane` (plus the spinner frame) instead of `*sessionState`.
  - `sessionState` keeps only root state plus `main *pane` and `kids map[core.SessionID]*pane`.
  - `foldCtl` operates on a pane: `foldCtl{a, p}`, or `foldCtl{a}` using `columnFocused(a)`. Every transcript pane, main and each subagent, therefore gets tool-call groups, fold `o`, the search hold, and the permission hold. The user confirmed they want that.
- **D2: naming.** `mouse.go` on `main` already declares `type pane int` (`paneNone`/`paneTranscript`/`paneDetails`). Rename the mouse enum to `mouseRegion` (`regionNone`/`regionTranscript`/`regionColumn`), so that `pane` is the struct from D1.
- **D3: mouse selection and wheel follow the column.**
  - The mouse's second region is the column's body: `a.lay.Side` minus the breadcrumb and rule rows (`columnHeaderRows = 2`).
  - When the column's top entry is a transcript pane, hit-testing, scrolling, and copying use that pane's `list` through the same blocklist paths the transcript region uses: `blocklist.HitTest`, `blocklist.Lines`, and the transcript IDs from that pane's projection.
  - When the top entry is a details pane, they use `top.body`, as `main`'s code does with `a.w.details` today, so `details.HitTest` and `details.Lines` must exist on the per-entry body.
  - A click in the column focuses the column; a click in the transcript focuses main.
  - The drag stays pinned to the region it started in.
  - A click on the breadcrumb row does nothing.
  - Every `mouse_test.go` test that used the details split must keep passing when re-pointed at a details entry in the column. Add one test for selecting and copying inside a subagent pane.
- **D4: layout.** Keep `main`'s `computeLayout`/`layoutBody`/`rects` shape, margins, and `SideSpans`. Change the following:
  - rename `DetailsOpen` to `ColumnOpen`;
  - add a `focus` parameter and the `MainCrumb` field;
  - narrow with the column open: the focused pane takes the region (the old branch's rule);
  - wide: the column takes `detailsPercent`. `SideSpans` applies only to the sidebar, never the column, unless `main`'s code already treats the details split as spanning; if it does, keep the same behaviour for the column.
  - Compose with `fitRows`/`hjoin`/`vjoin`, never lipgloss joins (AGENTS.md).
- **D5: goldens.**
  - Existing goldens change only where the details split's header becomes the breadcrumb and rule, and where `main`'s new goldens include a details split.
  - Regenerate the old branch's new goldens (`app_details_column`, `app_subagent_view_wide/_narrow`, `app_subagent_nested`) under `main`'s layout, and inspect each one.
- **D6:** every original task's tests carry over, adapted to `main`'s helpers. None is dropped without a ledger ruling.

## Tasks

Each task: TDD where it adds behaviour; `make check` green; one or more commits; a task review.

### Task P0: Carry over the unchanged pieces

- `git checkout subagent-view -- <paths>` for the parts `main` didn't touch:
  - `internal/ui/transcript/{projection.go,apply.go,subagent.go,child_test.go,projection_test.go}`, merging by hand if `main` changed them. Check `git diff 2f48f44 main -- internal/ui/transcript` first: `main` added `groups.go`, and `Projection`'s 20-method archtest budget matters.
  - `internal/bubbles/breadcrumb/**`
  - `internal/bubbles/details/details.go` + tests (`WithoutHeader`/`Header`). `main` may have added `HitTest`/`Lines`, so merge them.
  - `internal/ui/theme/widgets.go` + test (`Set.Breadcrumb`), merged with `main`'s changes.
  - The spec and plan docs.
- `make check`, then commit `feat: carry NewChild, breadcrumb, details.WithoutHeader onto main`.

### Task P1: `pane` with `itemTrack` (a refactor with no behaviour change)

- Rename the mouse enum (D2).
- Build the D1 `pane`: move `proj`, `track`, and `times` off `sessionState` into `sess.main`, and move `a.w.list` and `viewState`'s list size into the pane. Carry over `installMain` (from `e431884`) for the switch and new-session paths.
- Every existing test and golden stays byte-identical. The benchmarks (`App_*`, `Fold*`) stay within their budgets. On this machine `BenchmarkApp_Resize2000` measured ~1.7 s on `main` before the port; it must not regress.

### Task P2: kids

- Port `edee4e5`: `kids.go`, `kids_test.go`, the `recSessions.Messages` fake fix, `Projection.AddNotice`, and clearing kids on every root rebuild.
- `routeKids` also runs `track.regroup` for a kid pane (`regroupsOn`), so groups stay correct inside subagent panes. Panes not in the column still build no items.

### Task P3: the column (details entries)

- Port `eb37d8d` + `04565dc`:
  - `column.go`;
  - the layout per D4;
  - `detailsctl.go` removed;
  - details bodies moved per entry;
  - the breadcrumb;
  - `h`/`l` in `isFixed`;
  - details-focused `j`/`k` scrolling;
  - `ctrl+e`/`ctrl+y`;
  - `images.go` sixel offset;
  - `pushTheme`.
- Port the mouse to the column's details body (D3, the details half).
- Tests: `column_test.go` (the Task 6 set) and `layout_test.go`, plus `mouse_test.go` re-pointed.

### Task P4: drill-in, focus, fold in subagent panes

- Port `2cedd94`:
  - the live kid pane on `enter`;
  - column upserts;
  - `onTick` over the column's panes;
  - `setFocus`/`syncHighlight`;
  - keys routed to the focused pane (including `o` fold, which `main` added, and `y`/`/`/`gp`).
- Port the mouse to a transcript pane in the column (D3, the second half), plus the new mouse test.
- New test: `TestColumn_ChildGroupsFold`. Three consecutive child `read` calls in a subagent pane show as one `explored` header, and `o` expands it.

### Task P5: narrow, resize, theme, permissions

- Port `e54a54b` (narrow takeover hint, per-pane resize debounce, theme restyle of column panes, the spinner-stop test, goldens) and `b0374db` (`cardKey.pane`, focused-pane `target`, card width per pane, details-focused card keys do nothing).

### Task P6: benchmark, e2e, docs, sanitization

- Port `95a8101`, `2ec0b30`, and `a0964c6`: `BenchmarkApp_SubagentStream` < 3 ms/op; `TestE2E_TUISubagentView`; `TestColumn_DetailsTitleSanitized`.
- AGENTS.md: merge the old branch's additions into `main`'s text. `main` documents groups, mouse, and MCP; keep all of it, and describe the column where it mentions the details split.
