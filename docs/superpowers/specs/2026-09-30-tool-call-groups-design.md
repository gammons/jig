# jig: Tool-Call Groups in the Transcript

- **Status:** draft, awaiting review
- **Date:** 2026-09-30
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md` (Plan 2, implemented).
- **Changes Plan 2:** a run of exploration calls (§5.3 tool lines) renders as one collapsible group line. It adds a NORMAL key (§6.3) and one action (§6.1).
- **Interacts with:** `docs/superpowers/specs/2026-09-29-subagent-view-design.md` (not yet implemented). Wherever this spec says "the details split", read it as "the column's top entry" once that spec lands. Grouping inside a subagent's child transcript is out of scope here; that spec's child panes can adopt `foldState` later.
- **Prior art:** opencode's TUI, which folds consecutive reads and searches into one line.

## 1. Intent

**What the author asked for**

- opencode groups consecutive reads and searches into one line in its UI. jig shows every call on its own line, so an exploration of ten files takes twenty rows (each line plus its gap) and pushes the model's reply off screen.

**Decisions made in review**

1. **Group consecutive exploration.** A group is an unbroken run of `read`, `grep`, and `glob` calls, across model steps, not only one parallel batch.
2. **Expand inline.** A key expands a group in place, showing each call's normal line. Details, yank, and search then work per call exactly as today.
3. **Collapsed while live, showing the current call.** A group stays one line while it runs: a spinner, the running tally, and the call running now.
4. **Fold in the App (approach A).** The transcript `Projection` is unchanged. A pure grouping function and a new `foldState` in `ui` map groups onto blocklist items. The rejected options were a `KindGroup` block in the transcript (it would push a display concept into the projection and change permissions, changed files, and every `Blocks()` consumer), and generic folding in `blocklist` (hidden-item logic in the list the performance budgets cover, while the App would still have to compute the groups).

**Success criteria**

- A model that reads four files and runs two greps, with reasoning between its steps, shows one line: `▸ explored · 4 reads, 2 greps`.
- While it runs, that line shows a spinner and the call in flight: `⠋ exploring · 3 reads, 1 grep · read internal/ui/render.go`.
- `o` on the line shows each call's own line underneath, and `enter`/`y` on one of those behaves exactly as on an ungrouped call today.
- A permission request on a grouped call is never hidden: its card shows and answers as today.
- Search finds text inside a collapsed group.
- The performance budgets in AGENTS.md still pass, and the two new benchmarks (§7) pass too.

## 2. Scope

**In scope**

- `transcript.Groups`, a pure grouping function.
- `foldState` in `internal/ui`: groups, expanded groups, forced expansion, and the visible layout.
- Group header rendering, live and settled; nested member rendering.
- The `transcript.fold` action on NORMAL `o`.
- Group details, group yank, search, permission, and selection behavior.

**Out of scope**

- Grouping any other tools (`edit`, `write`, `todo`, `skill`, `task`, extension tools). `bash` was added later (§10).
- Grouping inside subagents. A subagent's calls already live inside its `↳` block.
- Persisting which groups are expanded. A resumed session opens with every group collapsed.
- The headless renderer (`ui/plain`). It keeps printing one line per call.
- Mouse clicks to expand a group.

## 3. Grouping rules

`transcript.Groups(blocks []Block) []Group` in `internal/ui/transcript/groups.go`. It is pure and imports only the standard library and `internal/core`, like the rest of the package.

```go
type Group struct {
	ID      BlockID   // "g/<first member call ID>"
	Members []BlockID // display order: calls and absorbed reasoning
}
```

- **Exploration call.** A `KindTool` block whose `Call.Name` is `read`, `grep`, `glob`, or `bash` (§10), in any state (running, awaiting permission, ok, error, denied, cancelled, pending).
- **A group** is a maximal run of blocks, in display order, containing at least two exploration calls, where every block is either an exploration call or an absorbed reasoning block.
- **Absorbed reasoning.** A `KindReasoning` block joins the run only when an exploration call comes both before and after it within the run. Reasoning before the first call or after the last call is not a member.
- **Breakers.** Every other block ends a run: `KindText`, `KindUser`, `KindNotice`, `KindSubagent`, and any `KindTool` with another name.
- **A single call is never grouped.** A run with one exploration call (with or without reasoning around it) renders exactly as today.
- **Identity.** `ID` is `"g/"` plus the first member call's `Call.ID`. It does not change as the group grows, and a reload of the same messages produces the same ID. The `g/` prefix cannot collide with the transcript's `u/`, `m/`, `t/`, or `n/` IDs.

Example: `text, read, ∴, grep, ∴, read, edit, read, text` gives one group `[read, ∴, grep, ∴, read]`. The last `read` stays a plain line.

Live, a group appears when its second exploration call starts; until then the first call is a plain line, and its item is replaced by the group header.

## 4. Rendering

### 4.1 Collapsed header

**Live** while any member call is `running` or `awaiting-permission`, or any absorbed reasoning block is still thinking (`Block.Thinking`):

```
⠋ exploring · 3 reads, 1 grep · read internal/ui/render.go
```

- The spinner replaces `▸`, and animates like a running tool's (the App's `frame`).
- **Tally:** every member call so far, in the fixed order reads, greps, globs, with singular and plural forms (`1 read`, `2 reads`, `1 grep`, `2 greps`, `1 glob`, `2 globs`), joined with `, `. A kind with no calls is left out.
- **Current call:** the last `running` member call in display order (with a parallel batch in flight, several run at once; the last one is shown), formatted as `<name> <summary>` with `toolLine`'s summary for it (a read's path, a search's quoted pattern). When no call is running (for example while the model thinks between steps), the current-call part is left off.
- The live line uses the running tool style (`Render.Tool`).

**Settled** otherwise:

```
▸ explored · 4 reads, 2 greps, 1 glob
▸ explored · 4 reads, 2 greps · 1 failed ✗
```

- The main part uses the OK style (`Render.OK`).
- Problem calls add suffixes after the tally, each in its own state style, in this order: `· N failed ✗` (`Render.Error`), `· N denied ⊘` (`Render.Denied`), `· N cancelled ⊘` (`Render.Dim`). A `pending` call (a loaded, unanswered one) adds nothing.
- Between steps without reasoning, a group can briefly show as settled and then live again when the next call joins. This is accurate and accepted.

A collapsed header never shows `awaiting-permission`: a member awaiting permission forces its group open (§5.4).

### 4.2 Expanded header and members

- The header is the same line with `▾` in place of `▸`. While live, the spinner replaces `▾`, and the current-call part is left off, since that call is visible below.
- Each member (calls and absorbed reasoning) renders as its normal line (`renderTool`, `renderReasoning`), laid out two columns narrower and prefixed with two spaces.
- ~~Members keep the transcript's normal one-line gap between items.~~ Superseded by §10: the header and its members stack with no gap.

### 4.3 Safety and width

- Every part taken from a call input or result passes `ansi.SanitizeLine`, as in every other tool line.
- The header is cut to width with `…` through `fitLine`, like every one-line block.

## 5. Interaction

### 5.1 Expand and collapse

A new action `transcript.fold` ("Toggle group", picker group "Transcript"), bound to NORMAL `o` in `actions.DefaultBindings()`. `[keybinds]` can remap it.

- On a group header, it toggles the group. The selection stays on the header.
- On a member of an expanded group, it collapses the group and selects the header.
- On any other block, it does nothing.
- On a group held open by a search or a pending permission, it flips the group's own state, which shows once the hold ends.
- It is not bound in INSERT.

When the view is following the bottom, expanding a group keeps the view pinned. Otherwise the blocklist's anchor keeps the header in place and the members open below it.

### 5.2 Details and yank

- **`enter` on a header** opens the details split with header `group · 4 reads, 2 greps` (the tally, through `details.go`'s `header`) and one line per member in order: each call's one-line summary, and each absorbed reasoning block as `∴ thought for <dur>` (or `∴ thinking`). It needs no port call.
- **`enter` on a member** opens that call's own details, as today.
- **`y` on a header** copies each member call's subject, one per line, in order: a read's `path`, a grep's or glob's `pattern`. Reasoning is skipped.
- **`y` on a member** copies what it copies today.

### 5.3 Search

The blocklist searches rendered lines, so a collapsed group would hide its members' text. While a search query is set, every group is laid out expanded. Clearing the search (`q`/`esc`) restores each group's own state. `n`/`N` therefore reach matches inside groups that were collapsed.

### 5.4 Permissions

- A group with a member in `awaiting-permission` is forced open while that request is pending. The card stays on the member's own block, so switching to NORMAL, selecting the block, the 400 ms arm delay (`cardArmDelay`), `gp`, and `a A d D` all work unchanged.
- When the request resolves, the group returns to its own state. If the selection was on a member that is now hidden, the header is selected.

### 5.5 Navigation, mouse, sidebar

- `j`/`k`, `gg`/`G`, and `ctrl+u` move over visible items: a collapsed group is one stop; an expanded group is its header plus each member.
- Mouse wheel scrolling and drag-to-copy act on rendered lines and need no change.
- The sidebar's changed files and browser URL come from the unchanged projection and are unaffected.

## 6. Architecture

### 6.1 `foldState` (`internal/ui/fold.go`)

`sessionState` is at archtest's limits (15 fields, 20 methods), so its list bookkeeping (`versions`, `dirty`, `live`) moves into a new `itemTrack` type (`internal/ui/items.go`), one field in their place, which also holds each block's last-issued nesting and the `foldState`. The App reaches the fold through a `foldCtl{a}` controller, like `permCtl`. `foldState` owns:

- the current `[]transcript.Group` and a member-to-group index;
- the set of group IDs the user expanded;
- forced expansion: all groups while a search query is set, and the groups holding an `awaiting-permission` member.

`layout(blocks []transcript.Block) []entry` returns the visible item sequence, and is the only place the collapsed, expanded, or forced rule lives. An entry is either a plain block, a group header, or a nested member. `foldState` resets with the other per-block state in `resetBlocks` (load, resume, new session).

### 6.2 Data flow

- **Structural changes rebuild the item list** with `setItems`: a new block, `load`, `dropUser`, a fold toggle, search set or cleared, and a permission forcing a group open or releasing it. The blocklist keeps cached renders for every ID still present (`SetItems` prunes only removed IDs), so only changed items re-render.
- **Content changes still upsert**: a tool finishing, a stream tick, a spinner frame. A member of a collapsed group maps to an upsert of its group header. A member of an expanded group maps to an upsert of the member and its header.
- **Versions.** A header's version is bumped whenever any member changes, its fold state flips, or (while live) the spinner frame advances. A live header is added to `s.live` so each tick re-renders it.
- **Text deltas never regroup.** Only a new block can change grouping, so the streaming delta path is unchanged.
- **Selection.** When a rebuild removes the selected member's item (a collapse, a released permission, a cleared search), the App selects the group header explicitly. Without that, `SetItems` would move the selection to the last item.

### 6.3 Rendering

- `blockData` gains `Group *groupData` (set on header items: the member blocks, their durations, whether the group is expanded) and `Nested bool` (set on member items of an expanded group).
- `render` dispatches a header to a new `renderGroup`. A `Nested` item renders at `width-2` and gets a two-space prefix.
- The tally, current call, and suffixes reuse `toolLine`'s summaries and `styleState`'s styles.

### 6.4 Details and yank

`buildDetails` and yank look up the selected `transcript.Block` today. A selected `g/` ID goes instead to new `buildGroupDetails` and group-yank functions, which read the members from `foldState`. Member IDs are real block IDs and take the existing paths.

### 6.5 Action

`actions.TranscriptFold` (`"transcript.fold"`, title "Toggle group", group "Transcript") joins the built-in catalogue. `normal.o` binds to it in `DefaultBindings()`. `App.runAction` dispatches it to the NORMAL handler's fold logic.

## 7. Performance

New budgets, bound by AGENTS.md's rule: if a benchmark misses, fix the algorithm; never raise the budget. They run over a resumed session of 2,000 messages through the real `mdrender` at 150×40, in which every fourth message holds a group (reasoning, a read, and a grep).

| Benchmark (`internal/ui`) | Budget |
|---|---|
| `BenchmarkApp_FoldToggle2000`: `o` on a group, then `View` | < 50 ms/op |
| `BenchmarkApp_ToolStart2000`: one `ToolCallStarted` that joins a group (a structural rebuild), then `View` | < 5 ms/op |

The existing `internal/ui` benchmarks keep their budgets and their group-free history, so their recorded numbers stay comparable. If `ToolStart2000` misses its budget with a full regroup and rebuild, regroup incrementally: blocks are only appended, so only the trailing group can change.

## 8. Testing

**`transcript.Groups`**, table-driven, with hand-written expected groups:

- two or more exploration calls group; a lone call does not;
- reasoning between calls is absorbed; leading and trailing reasoning is not;
- each breaker ends a run: text, user, notice, subagent, `bash`, `edit`, `write`, `todo`, `skill`, and an unknown tool;
- error, denied, cancelled, and pending calls are members;
- the ID comes from the first call and is unchanged as the group grows;
- two groups split by a breaker stay separate;
- empty input and input with no tools give no groups.

**`foldState.layout`**: collapsed, expanded, forced by search, forced by a permission, and each forced state released back to the group's own state.

**Rendering**, golden frames with pinned `Styles` (`golden.Assert`): live header with and without a current call; settled header; settled with failed, denied, and cancelled suffixes; expanded header with nested members; truncation to width; a member path holding an escape sequence rendered harmlessly.

**App behavior** with `newTestApp`:

- a second read turns the first read's line into a header, and a following view stays pinned;
- `o` toggles a header; `o` on a member collapses the group and selects its header; `o` elsewhere does nothing;
- `enter` on a header shows the member list; `enter` on a member shows its own details;
- `y` on a header copies the subjects, one per line;
- `/` expands every group; clearing it restores each group's state; `n`/`N` land on a match inside a previously collapsed group;
- a permission on a member forces its group open, the card answers after arming, `gp` reaches it, and resolving it restores the group with the selection on the header;
- a stored session with groups loads collapsed;
- a `[keybinds]` entry remaps `transcript.fold`.

**Actions**: `transcript.fold` is in the catalogue (group "Transcript") and bound to `normal.o` by default.

**End to end** (`e2e/`, under a pty): a `jigtest` script makes three reads across two steps and then replies. After the reply, `esc`, `k`, and `o` expand the group, and the screen shows `▾`. The pty output is redrawn cell by cell, so a line that changes in place (`exploring` to `explored · 3 reads`) never reaches the stream as one string; the test waits only for text drawn fresh. The header's exact text is covered by the App-level tests, which render whole frames.

**Benchmarks**: the two in §7, plus the existing five, within budget.

## 9. Error handling

Grouping is pure and cannot fail. Group details and yank need no port calls, since they come from blocks already in memory. A member whose input does not parse is still counted by its tool name, and its summary falls back to what `toolLine` already shows for it (an empty path or pattern).

## 10. Addendum: bash in groups, and tight tool lines (2026-10-01)

Agreed in review after the first version shipped, from a transcript where runs of `bash` calls each took two rows.

- **bash groups.** `bash` is an exploration call (§3), in the same run as `read`/`grep`/`glob`; `edit`, `write`, and the rest still end a run. Every bash call joins, including `agent-browser` commands and commands that change things. The header verb stays `explored`.
- **Tally.** Commands come last: `2 reads, 1 grep, 3 commands` (`1 command` when there is one). The live current call is `bash <first line>`, or an `agent-browser` command's summary alone (it has no tool name).
- **Failures.** A bash call that exited non-zero or timed out counts toward `· N failed ✗`, as its own line renders it as failed.
- **Yank.** A bash member yanks its command, its lines joined with spaces (one member per line).
- **No gaps between one-line rows.** A one-line row (a tool or subagent call, a group header, a nested member, or a reasoning block the model is done thinking in, shown as `∴ thought for …`) directly under another is laid out with no blank line between them; every other neighbor keeps the one-line gap, including a reasoning block still showing its text. `blocklist.Item` gains `Compact`, and the blocklist drops the gap between two neighboring `Compact` items. The App sets it from the item's own block each time it builds the item, so it needs no neighbor lookups and never forces a rebuild. A reasoning block stops thinking when the next block of its message arrives; `Projection.Apply` now reports it alongside the new block, so both are rebuilt in the same pass.
