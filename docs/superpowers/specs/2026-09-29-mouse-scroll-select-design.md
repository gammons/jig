# jig: Mouse Wheel Scrolling and Text Selection

- **Status:** draft, awaiting review
- **Date:** 2026-09-29
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md` (Plan 2, implemented).
- **Changes Plan 2:** it moves "mouse support beyond wheel scrolling" (§2, Deferred) into scope for text selection, and it replaces §5.2's bottom-pinning rule (see §4.2).
- **Prior art:** slk's `internal/ui/selection`, `internal/ui/drag.go`, and `internal/ui/reducer_mouse.go` (MIT, same author).

## 1. Intent

**What the author asked for**

- Mouse wheel scrolling in the TUI. Scrolling changes which transcript block is highlighted.
- Mouse text selection with copy, as both opencode and slk support. jig takes over mouse reporting to get wheel events, and that disables the terminal's own drag-to-select, so jig has to provide selection itself.

**Decisions made in review**

1. **The wheel scrolls by lines, and the highlight follows.** Each notch scrolls 3 lines. The highlighted block is the one at the vertical middle of the transcript. Near the top and bottom, where the view can't scroll further, it falls back to the first or last block. A long block scrolls smoothly, line by line, instead of jumping from block to block.
2. **Selection works in the transcript and the details split.** It does not work in the sidebar or the prompt.
3. **Releasing the mouse copies the selection.** This matches slk and most terminals. `y` keeps copying the whole highlighted block.

**Success criteria**

- Wheel scrolling moves through a tall block 3 lines per notch with no jumps. A 40-line block in a 30-row transcript crosses the view in about 14 notches (its 40 lines past the middle row). The highlight changes as blocks cross the middle row.
- Scrolling up inside a streaming last block does not snap the view back to the bottom.
- Dragging across wrapped markdown, a tool line, and a details-split diff copies exactly the visible text, without styling.
- The existing performance budgets (AGENTS.md) still pass.

## 2. Scope

**In scope**

- Wheel scrolling of the transcript and the details split, in INSERT and NORMAL modes.
- Press-drag-release text selection in the transcript and the details split, with the selection drawn highlighted, auto-scrolling at the edges, and copying on release.
- A click on a transcript block highlights it.

**Out of scope**

- Selection in the prompt or the sidebar.
- Double-click word selection and triple-click line selection.
- Clicking the prompt to switch to INSERT mode.
- A setting to turn mouse support off.
- The wheel over the sidebar, the prompt, or the status bar. These ignore the wheel.

## 3. Behavior

### 3.1 Wheel

- **Transcript:** one notch scrolls 3 lines (`wheelLines`). The highlight then moves to the block at row `h/2` of the view. If no block line is at that row (a gap between blocks), the block just above it is used. When the view is at the very top or bottom, the highlight is the first or last block.
- **Details split:** one notch calls `ScrollBy(±3)`.
- **Everywhere else:** the prompt, the sidebar, the status bar, and the gap row ignore the wheel. While the picker is open, it gets no mouse events at all.
- **Modes:** the wheel works in INSERT and NORMAL modes and never changes the mode. In INSERT, the prompt keeps its draft and cursor.
- **Following new output:** the view follows new output only while it is scrolled all the way down (§4.2). Wheel-scrolling back to the bottom turns following on again and highlights the last block.

### 3.2 Selection

- **Starting:** a press in the transcript or the details split's body starts a drag in that pane. A press on the transcript's selection-prefix column or scrollbar column, or on the details header or rule rows, starts nothing. A press on a gap row between blocks snaps to the nearest block line.
- **Dragging:** motion extends the selection from the press point to the current point, and it is drawn highlighted. The selection is in document order, so it can run backwards and can span lines and blocks. Motion outside the pane that started the drag is pinned to that pane's edge, so a selection never crosses panes.
- **Auto-scroll:** while the pointer is on the top or bottom row of the pane (or pinned there), the pane scrolls one line every 50 ms (`autoScrollEvery`), and the selection extends with it. This stops on release, or when the pointer leaves the edge row.
- **Release:** if the pointer moved, the selected text is copied with `tea.SetClipboard`, and the status hint shows `copied N chars` (N is the rune count). If it didn't move, the release counts as a click (§3.3).
- **Clearing:** the selection stays drawn until a new press, any wheel event, `esc`, or the conditions in §5.
- **The text copied:** plain text with every escape sequence stripped. Trailing whitespace is trimmed from each line, lines are joined with `\n`, and there is no trailing newline. Wide characters are copied whole (§5).

### 3.3 Click

- **Transcript:** a press and release without motion highlights the block under the pointer, as `j`/`k` do (`Select` plus `ensureVisible`). If that block has a permission card, the card disarms for `cardArmDelay` as it does for any selection move, so one click can never also answer it.
- **Elsewhere:** a click anywhere else only clears the selection.

## 4. Architecture

Everything follows the Plan 2 layer rules and the `internal/archtest` limits. The new package is a widget under `internal/bubbles`.

### 4.1 `internal/bubbles/selection` (new)

- **Holds:** the selection only, no mouse or clipboard handling. It is ported from slk's selection package.
- **Imports:** stdlib and `internal/bubbles/ansi` only.

```go
type Point struct {
    ID   string // item ID (blocklist) or a fixed pane ID (details)
    Line int    // line within the item's rendered lines, from 0
    Col  int    // display cell column within that line, from 0
}
type Range struct{ Start, End Point; Active bool }

func (r Range) Normalized(order func(id string) int) (lo, hi Point) // document order
func (r Range) Empty() bool
func Before(a, b Point, order func(id string) int) bool

// Highlight applies on/off around the selected cells of row, the rendered
// line (line) of item id, keeping existing escapes intact.
func Highlight(row string, id string, line int, r Range, order func(string) int, on, off string) string
// Text extracts the plain selected text from lines(id) for every id in ids,
// which runs from lo.ID to hi.ID in order.
func Text(r Range, ids []string, order func(string) int, lines func(id string) []string) string
```

- **Document order:** points are ordered by item order, which `order` returns, then line, then column. The same function serves the transcript (block order) and the details split (one ID).
- **Cells:** `Highlight` and `Text` slice by display cell with `ansi.Cut`. `Text` strips escapes afterwards.

### 4.2 `internal/bubbles/blocklist`

**`ScrollBy(n int)` (new method):**
- Moves `yOffset` by n, clamped.
- Sets `sel` to the item at line `yOffset + h/2` (via `itemAt`), without `ensureVisible`, so the view never jumps to the top of a tall block. At `yOffset == 0` it selects item 0. At the bottom it selects the last item.

**Following, replacing Plan 2 §5.2:**
- A new `follow` bool is true while the view is scrolled to the bottom. The view stays pinned to the bottom as content arrives only while `follow` is set, not merely while the last item is selected.
- `follow` is set by `Bottom`, by `moveTo` onto the last item (so `j`, `G`, and `ctrl+d` keep their current behavior), and by `ScrollBy` reaching the bottom.
- `ScrollBy` clears it when it leaves the bottom.
- `relayout`'s `m.sel == len(m.items)-1` check becomes `m.follow`. `SetItems` keeps "move to the last item when the selection was on it" only while following.

**`hitter` (new unexported type in `hit.go`):**
- Built from a `Model` value. It maps a view cell to an item line, using `offsets`, and returns an item's rendered lines at the current width (from the cache).
- It is exposed through two package functions, `blocklist.HitTest(m Model, x, y int) (id string, line, col int, ok bool)` and `blocklist.Lines(m Model, id string) []string`. `selection.Text` needs the second.
- `ok` is false for the prefix column, the scrollbar column, and out-of-range cells. A gap row returns the nearest item line.

**Size:** `Model` has 19 methods and the limit is 20. Adding `HitTest` and `Lines` as methods alongside `ScrollBy` would make 22, which is why they are package functions. Only `ScrollBy` is added to `Model` (20 of 20). Allowlisting is a last resort, and only with a justification.

### 4.3 `internal/bubbles/details`

- **New:** `HitTest(x, y int) (line, col int, ok bool)` over the body, with `ok` false on the header and rule rows, plus `Lines() []string` returning the current content's lines for `Text`. The pane's `selection.Point.ID` is the constant `"details"`.
- **Already there:** `ScrollBy` and `BodyOrigin`.

### 4.4 `internal/ui/mouse.go` (new)

**`mouseCtl{a *App}`** handles `tea.MouseWheelMsg`, `tea.MouseClickMsg`, `tea.MouseMotionMsg`, and `tea.MouseReleaseMsg`, routed from one new `case` in `App.Update`. The mode handlers don't change.

- **Hit-testing:** the pointer is mapped to a pane with `a.lay`: `Transcript`, `Side` (when `DetailsOpen`), `Prompt`, or other.
- **Drag state** is a `mouseState` struct inside `viewState` (13 → 14 fields; `App` stays at 14):
  - `phase`: idle, pressed, or dragging.
  - `pane`: transcript or details.
  - `sel`: a `selection.Range`.
  - `last`: the last pointer cell.
  - `autoGen`: the generation of the auto-scroll tick.
- **Auto-scroll:** a `mouseScrollMsg{gen}` scheduled with `a.after(autoScrollEvery, …)` (the App clock), and dropped when `gen` is stale, the same way `resizeDebounce` works.
- **Mouse reporting:** `View()` sets `v.MouseMode = tea.MouseModeCellMotion`.

### 4.5 Drawing the highlight

- **Where:** in `App.View`, after `a.w.list.View()` and `a.w.details.View()` and before `compose`. Each visible row that the selection touches is re-mapped to its (ID, line) with `blocklist.HitTest`'s row mapping, and passed through `selection.Highlight`.
- **Cost:** it works only on visible rows. It never bumps an item version or touches the render cache, so the budgets are unaffected.
- **Color:** the highlight uses a new `theme.Set.Selection` on/off pair built in `theme.Build` with `ansi.SGR` from the palette's existing `SelectionForeground`/`SelectionBackground` (its "selected text" colors, already defaulted by `Complete`), the same way the search highlight is built.

## 5. Edge cases

- **Content changes under a selection:** if streaming or reflow changes a selected item's lines, `Line` and `Col` are clamped to the item's current lines when drawing and copying. If a selected item disappears (a `Load`), the selection is cleared.
- **Resize or theme change:** points are kept and redrawn on the next frame.
- **Details split:** a details selection is cleared when the split closes or shows different content (`SetContent`, not `ReplaceContent`).
- **Picker opens:** it cancels any drag with nothing copied.
- **Wide characters:** a point on the second cell of a wide character snaps to its first cell, so copying never splits a character.
- **Lost release:** if a release never arrives (the pointer was released outside the window), the next press starts a new drag.
- **Tiny panes:** a pane with a body height of at most 1 never starts a drag or auto-scrolls.
- **Sanitizing:** copied text comes from rendered lines, which are already sanitized. `Text` also strips all escapes, so nothing but plain text reaches the clipboard, the same guarantee as `y`.

## 6. Testing

All tests are written test-first.

- **`selection`:** ordering across items; `Empty`/`Normalized`; `Highlight` over styled and wide-rune rows, keeping escapes; `Text` across wrapped lines and multiple items, with trimming; a golden frame of a highlighted region.
- **`blocklist`:**
  - **`ScrollBy`:** 3-line steps, middle-row selection, tall-block continuity, and the top and bottom fallbacks.
  - **Following:** scrolled up inside the last block, `Upsert` doesn't move the view; scrolling back to the bottom resumes following; `j`/`G` unchanged.
  - **`HitTest`:** prefix, scrollbar, gap, and item cells.
  - **Speed:** the three blocklist budgets are re-run.
- **`details`:** `HitTest` on header, rule, and body rows; wheel `ScrollBy`.
- **App (`newTestApp`, plus a `.mouse(msg)` helper):**
  - The wheel in INSERT mode keeps the draft.
  - The wheel over the prompt and the sidebar is ignored.
  - A drag copies the expected text and shows the hint.
  - A drag across the pane border is pinned.
  - Auto-scroll advances on the fake clock and stops on release.
  - A click selects and disarms a card; `esc` clears.
  - The picker cancels a drag.
  - `View()` enables `MouseModeCellMotion`.
- **e2e:** under a pty, send SGR mouse sequences for a wheel and a drag to the built binary. Assert that the transcript scrolled and that an OSC 52 clipboard sequence was written.
