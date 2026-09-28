# Mouse Wheel Scrolling and Text Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mouse wheel scrolling of the transcript and the details split, with a highlight that follows the view's middle row, plus press-drag-release text selection that copies on release.

**Architecture:**
- A new `internal/bubbles/selection` widget holds the range and does the highlighting and text extraction.
- `blocklist` gains `ScrollBy`, a `follow` flag, and the package functions `HitTest`/`Lines`. `details` gains `HitTest`/`Lines`.
- A new `internal/ui/mouse.go` routes mouse events by `a.lay`, keeps the drag state in `viewState`, and paints the selection over the rendered panes in `App.View`.

**Tech Stack:** Go, `charm.land/bubbletea/v2` (v2.0.10) mouse messages and `tea.SetClipboard`, `charm.land/lipgloss/v2`, `internal/bubbles/ansi`.

**Spec:** `docs/superpowers/specs/2026-09-29-mouse-scroll-select-design.md`

## Global Constraints

- **Before every commit:** `make check` must pass (AGENTS.md: build, `go test ./... -race`, e2e + jigtest tests, golangci-lint with and without `--build-tags jigtest`, gofmt). Every commit, not just the last one.
- **Import rules:** `internal/bubbles/...` imports only stdlib, `charm.land/...`, the allowed third-party list, and `internal/bubbles/{ansi,overlay,scrollbar,wintree}`. It never imports another widget, `core`, `ui`, or `clock`.
- **Widget rules:** no package-level mutable vars, no `func(tea.Msg)` struct fields, and every `View` takes zero parameters.
- **archtest size limits:** a struct has ≤15 fields and ≤20 methods across its package; a file has ≤500 lines. Current counts: blocklist `Model` 19 methods, details `Model` 11, `App` 14 fields / 18 methods, `viewState` 13 fields. Allowlisting (`internal/archtest/allowlist.go`) is a last resort and needs a justification.
- **Tests:** no `time.Now`/`time.Sleep` in `_test.go`. All time comes from the App clock (`a.after`, `clock.Fake`).
- **Performance budgets** (AGENTS.md, binding): blocklist `View2000` < 2 ms, `Update2000` < 3 ms, `Load2000` < 250 ms; ui `Resize2000` < 1.5 s, `DetailsToggle2000` < 50 ms.
- **Values:** `wheelLines = 3`, `autoScrollEvery = 50 * time.Millisecond`, the details pane ID `"details"`, and the hint `copied N chars` (N = rune count).
- **Sanitizing:** clipboard text is plain: every escape stripped, trailing whitespace trimmed per line, joined with `\n`, no trailing newline.

## Review Focus

1. **Scrolled up inside a streaming last block:** new deltas must not snap the view to the bottom (spec §4.2). Pinned by Task 2's `TestBlocklist_ScrolledUpInsideLastBlockStaysPut`.
2. **Selection over styled, wrapped markdown with wide runes:** the copied text must match the visible characters exactly, never splitting a wide rune or leaking an escape. Pinned by Task 1's `TestText_StylesAndWideRunes`.
3. **A drag that leaves its pane** (into the sidebar, prompt, or across into details): the selection is pinned to the starting pane's edge. Pinned by Task 5's `TestMouse_DragPinnedToStartPane`.
4. **The picker opens while a drag is in progress:** the drag is cancelled and nothing is copied. Pinned by Task 5's `TestMouse_PickerCancelsDrag`.
5. **The last message is a 1-line block and you scroll up one notch:** the highlight must leave it only when it leaves the middle row, not stick to the bottom. Pinned by Task 2's `TestBlocklist_ScrollByFollowsMiddleRow` (the short-last-block case).

---

### Task 1: `selection` package

**Files:**
- Create: `internal/bubbles/selection/selection.go`
- Test: `internal/bubbles/selection/selection_test.go`, `internal/bubbles/selection/testdata/golden/highlight.ansi`

**Interfaces:**
- Produces:
  ```go
  type Point struct{ ID string; Line, Col int }
  type Range struct{ Start, End Point; Active bool }
  func (r Range) Normalized(order func(id string) int) (lo, hi Point)
  func (r Range) Empty() bool   // !Active or Start == End
  func Before(a, b Point, order func(id string) int) bool
  func Highlight(row, id string, line int, r Range, order func(string) int, on, off string) string
  func Text(r Range, ids []string, order func(string) int, lines func(id string) []string) string
  ```
  `order` maps an item ID to its position (the transcript's block order; details always returns 0). `ids` lists every ID from `lo.ID` to `hi.ID`, in order.

- [ ] **Step 1: Write the failing tests**
  - `TestBefore_OrdersByItemThenLineThenCol`: with `order` over `a:0, b:1`, check that `{a,5,9}` is before `{b,0,0}`, `{a,1,0}` is before `{a,1,3}`, and a point is not before itself.
  - `TestRange_NormalizedAndEmpty`: a backwards range (`Start {b,2,4}`, `End {a,0,1}`) normalizes to `lo={a,0,1}, hi={b,2,4}`. A range with `Active: false`, or with `Start == End`, is `Empty()`.
  - `TestHighlight_WrapsOnlySelectedCells`:
    - Row `"hello world"`, id `a`, line 0, range `{a,0,2}`–`{a,0,7}`, on/off `"<"`/`">"`: `xansi.Strip`-free output equals `"he<llo w>orld"`.
    - The same row on line 1 (outside the range) is returned unchanged.
    - A middle line of a multi-line range is highlighted whole.
  - `TestHighlight_KeepsExistingEscapes`: the row is `"\x1b[1mbold\x1b[m text"`. Highlighting cols 2–6 still contains `"\x1b[1m"`, and `xansi.Strip` of the result minus on/off equals the original stripped text.
  - `TestText_AcrossLinesAndItems`: `lines(a) = ["one two  ", "three"]`, `lines(b) = ["four"]`, range `{a,0,4}`–`{b,0,2}`. The result is `"two\nthree\nfo"`: trailing spaces trimmed, no trailing newline.
  - `TestText_StylesAndWideRunes`:
    - `lines(a) = ["\x1b[31m日本\x1b[m語 x"]`, range `{a,0,1}`–`{a,0,5}`. Col 1 is the second cell of `日` and snaps to col 0. The result is `"日本語"`, with no `\x1b`.
    - The same row with end col 3 (the second cell of `本`) gives `"日本"`.
  - `TestHighlight_Golden`: three styled rows, a two-line range, pinned on/off from `ansi.SGR(#000000, #5fafff)`. `golden.Assert(t, "highlight", strings.Join(rows, "\n"))`.

- [ ] **Step 2: Run to verify they fail**
  Run: `go test ./internal/bubbles/selection/`
  Expected: build failure, undefined `Point`/`Range`/`Before`/`Highlight`/`Text`.

- [ ] **Step 3: Implement `selection.go`**
  - **Slicing:** `Highlight` and `Text` slice rows by display cell with `ansi.Cut(s, left, right)`. For `Text`, strip escapes with `xansi.Strip` from `github.com/charmbracelet/x/ansi`, which is on the bubbles third-party allowlist.
  - **Wide runes:** snap a column that lands inside a wide rune down to that rune's first cell, both for the start and for the end (exclusive end = cell just after the snapped end rune).
  - **Clamping:** clamp `Line` and `Col` to the item's current lines (spec §5).

- [ ] **Step 4: Run to verify they pass**
  Run: `JIG_UPDATE_GOLDEN=1 go test ./internal/bubbles/selection/ && go test ./internal/bubbles/selection/ ./internal/archtest/`
  Expected: PASS. archtest passes, which confirms the imports.

- [ ] **Step 5: Commit** (after `make check`)
  `git commit -m "feat(selection): selection range, highlight, and plain-text extraction"`

---

### Task 2: blocklist `ScrollBy`, `follow`, `HitTest`, `Lines`

**Files:**
- Modify: `internal/bubbles/blocklist/blocklist.go` (Model gets `follow bool`; `Bottom`, `SetItems`, `ScrollBy`), `internal/bubbles/blocklist/scroll.go` (`relayout`, `moveTo`)
- Create: `internal/bubbles/blocklist/hit.go`
- Test: `internal/bubbles/blocklist/mouse_test.go`

**Interfaces:**
- Produces:
  ```go
  func (m *Model) ScrollBy(n int)                                        // Model: 20th method
  func HitTest(m Model, x, y int) (id string, line, col int, ok bool)    // package func
  func Lines(m Model, id string) []string                                // package func: the item's rendered lines at the current width
  ```
  - `HitTest` coordinates are view-local: x=0 is the selection-prefix column, x=w-1 is the scrollbar column, and `col` is the cell within the item line (x-1).
  - `ok` is false for the prefix column, the scrollbar column, and out-of-range cells. A gap row returns the nearest item line: the end of the item above it, or the start of the item below it for a leading gap.

- [ ] **Step 1: Write the failing tests** (the items use a fake render returning N numbered lines, as the existing blocklist tests do)
  - `TestBlocklist_ScrollByFollowsMiddleRow`:
    - **Setup:** h=10, items with heights `[3, 40, 1]`, gap 1, starting at the bottom.
    - **Scrolling up:** `ScrollBy(-3)` leaves `yOffset` at `total-h-3`. The selection is the item at line `yOffset+5`: the 40-line item, because the 1-line last item has left the middle row.
    - **Continuing:** repeated `ScrollBy(-3)` keeps the 40-line item selected until its first line passes the middle row. `yOffset` changes by exactly 3 each step until it is clamped at 0.
    - **At the top:** `yOffset==0` selects item 0.
    - **Back down:** `ScrollBy(+1000)` selects the last item.
  - `TestBlocklist_ScrollByNeverJumpsToBlockTop`: selecting a tall item with `ScrollBy` leaves `yOffset` exactly `start+3k`, never the item's first line.
  - `TestBlocklist_ScrolledUpInsideLastBlockStaysPut`:
    - **Setup:** the last item is 40 lines; `ScrollBy(-6)` from the bottom.
    - **Streaming:** `Upsert` of the last item, now 45 lines, keeps `yOffset` unchanged.
    - **Resuming:** `ScrollBy(+1000)` then `Upsert` of 50 lines leaves `yOffset == total-h`, following again.
  - `TestBlocklist_KeysKeepFollowing`: `j` onto the last item, `G`, and `ctrl+d` to the end each leave the view following (an `Upsert` that grows the last item keeps it bottom-aligned). This pins the existing behavior.
  - `TestBlocklist_HitTest`:
    - **Rejected cells:** x=0 and x=w-1 give `ok=false`.
    - **Item line:** `(3, row)` maps to the right `(id, line, 2)`.
    - **Gap row:** maps to the item above's last line.
    - **Out of range:** y ≥ h gives `ok=false`.
    - **Lines:** `Lines(m, id)` returns the item's rendered lines.

- [ ] **Step 2: Run to verify they fail**
  Run: `go test ./internal/bubbles/blocklist/ -run 'ScrollBy|StaysPut|KeepFollowing|HitTest'`
  Expected: build failure, undefined `ScrollBy`/`HitTest`/`Lines`.

- [ ] **Step 3: Implement**
  - **`follow`:**
    - It replaces `m.sel == len(m.items)-1` in `relayout`'s bottom-align check.
    - `Bottom()`, and `moveTo(i)` onto the last item, set it. `moveTo` elsewhere, and `ScrollBy` leaving the bottom, clear it.
    - `SetItems` computes `wasLast` from `m.follow`.
    - `New` starts with `follow = true`.
  - **`ScrollBy`:** clamp `yOffset+n` to `[0, total-h]`, then set `sel` by the spec §4.2 rule (top → 0, bottom → last and `follow=true`, else `itemAt(offsets, yOffset+h/2)`). No `ensureVisible`.
  - **`hit.go`:** an unexported `hitter` built from a `Model`, walking `offsets` the way `visibleRows` does. `Lines` reads `m.c.get(item, m.w-2, m.sv, m.styles, m.render).lines`.

- [ ] **Step 4: Run to verify they pass, plus the whole package and the budgets**
  Run: `go test ./internal/bubbles/blocklist/ ./internal/archtest/ && go test -run XXX -bench . -benchmem ./internal/bubbles/blocklist/`
  Expected:
  - Tests: PASS, and archtest reports blocklist `Model` at 20 methods, no violation.
  - Benchmarks: `View2000` < 2 ms, `Update2000` < 3 ms, `Load2000` < 250 ms.

- [ ] **Step 5: Commit** (after `make check`)
  `git commit -m "feat(blocklist): line-wise ScrollBy, follow flag, HitTest and Lines"`

---

### Task 3: details `HitTest`, `Lines`

**Files:**
- Modify: `internal/bubbles/details/details.go`
- Test: `internal/bubbles/details/details_test.go`

**Interfaces:**
- Produces: `func (m Model) HitTest(x, y int) (line, col int, ok bool)` and `func (m Model) Lines() []string`.
  - `line` is the content line (`scroll + y - 2`), and `col = x`.
  - `ok` is false for y ∈ {0, 1} (the header and rule rows), for y ≥ h, and for a line past the end of the content.
  - `Lines` returns the content lines with tabs expanded to 4 spaces, exactly as `View` draws them.

- [ ] **Step 1: Write the failing test** `TestDetails_HitTest`:
  - **Setup:** h=6 and 10 content lines, then `ScrollBy(3)`.
  - **Rejected rows:** y=0 and y=1 give `ok=false`.
  - **Body cell:** `(4, 2)` gives `(3, 4, true)`.
  - **Past the content:** `(0, 5)` gives `(6, 0, true)`, and with only 5 content lines it gives `ok=false`.
  - **Tabs:** `Lines()` equals the content with `\t` → 4 spaces.
- [ ] **Step 2: Run to verify it fails**
  Run: `go test ./internal/bubbles/details/ -run HitTest` → build failure, undefined `HitTest`.
- [ ] **Step 3: Implement both methods in `details.go`**
- [ ] **Step 4: Run to verify it passes**
  Run: `go test ./internal/bubbles/details/ ./internal/archtest/` → PASS.
- [ ] **Step 5: Commit** (after `make check`)
  `git commit -m "feat(details): HitTest and Lines for mouse selection"`

---

### Task 4: theme selection colors and App wheel scrolling

**Files:**
- Modify: `internal/ui/theme/widgets.go` (a `Selection SelectionStyles` field on `Set`, built in `Build`), `internal/ui/theme/widgets_test.go`, `internal/ui/app.go` (`Update` case, `View` mouse mode)
- Create: `internal/ui/mouse.go` (`mouseCtl`, `mouseState`, wheel only in this task), `internal/ui/mouse_test.go`
- Modify: `internal/ui/apptest_test.go` (the `.mouse` helper)

**Interfaces:**
- Consumes: `blocklist.(*Model).ScrollBy` (Task 2), `details.(*Model).ScrollBy` (existing).
- Produces:
  - **Theme:** `type SelectionStyles struct{ On, Off string }` in `theme`, built with `ansi.SGR(lipgloss.Color(p.SelectionForeground), lipgloss.Color(p.SelectionBackground))`.
  - **Mouse state:** `mouseState` as the `mouse` field of `viewState` (14 fields).
  - **Controller:** `mouseCtl{a *App}` with `handle(msg tea.MouseMsg) tea.Cmd`.
  - **Test helper:** `(ta *testApp) mouse(msg tea.Msg)`, an alias of `send` that documents intent.
  - **Pane:** `type pane int` with `paneNone`, `paneTranscript`, `paneDetails`, and `paneAt(a, x, y) pane`, using `a.lay` (`Transcript`, and `Side` only when `DetailsOpen`). Other rects, including `Gap`/`Prompt`/`Status` and the sidebar, give `paneNone`.

- [ ] **Step 1: Write the failing tests**
  - `TestBuild_SelectionFromPalette`: `set.Selection.On/Off` equal `ansi.SGR` of the palette's `SelectionForeground`/`SelectionBackground`.
  - In `mouse_test.go`:
    - `TestMouse_ViewEnablesCellMotion`: `ta.app.View().MouseMode == tea.MouseModeCellMotion`.
    - `TestMouse_WheelScrollsTranscriptAndMovesHighlight`: resume a session of 20 text blocks at 120×30 and start at the bottom. Then a `tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}` scrolls the list by exactly `wheelLines` (compare the stripped frame's first transcript line before and after), and `a.w.list.Selected()` changes to the block at the middle row.
    - `TestMouse_WheelInInsertKeepsDraft`: type `"half a thought"`, wheel up twice, and `a.mode` is still `modeInsert` and `a.w.prompt.Value()` is unchanged.
    - `TestMouse_WheelIgnoredOverPromptAndSidebar`: a wheel event at a prompt cell and at a sidebar cell (150-wide terminal) leaves the frame byte-identical.
    - `TestMouse_WheelScrollsDetails`: open details on a tall tool block (`enter`), wheel down over `a.lay.Side`, and the details body's first line advances by 3.
    - `TestMouse_IgnoredWhilePickerOpen`: open the picker (`ctrl+p`), wheel, and the frame is unchanged.
- [ ] **Step 2: Run to verify they fail**
  Run: `go test ./internal/ui/... -run 'Mouse|SelectionFromPalette'` → build failure.
- [ ] **Step 3: Implement**
  - **Theme:** add the `Selection` field to `theme.Set` and build it in `Build`. No widget's `SetStyles` changes: the App reads `a.theme.set.Selection` directly when painting (Task 5).
  - **App:** `Update` gets `case tea.MouseMsg: cmd = mouseCtl{a}.handle(msg)`, placed before `default`. `View` sets `v.MouseMode = tea.MouseModeCellMotion`.
  - **Wheel:** `MouseWheelMsg` → `paneAt` → `a.w.list.ScrollBy(±wheelLines)` or `a.w.details.ScrollBy(±wheelLines)`. Returns nil when `a.mode == modePicker`. Any wheel also clears `a.view.mouse.sel`.
  - **Pane coordinates:** convert screen (x, y) to pane-local by subtracting the rect's `X`/`Y`.
- [ ] **Step 4: Run to verify they pass**
  Run: `go test ./internal/ui/... ./internal/archtest/` → PASS.
- [ ] **Step 5: Commit** (after `make check`)
  `git commit -m "feat(ui): mouse wheel scrolls the transcript and the details split"`

---

### Task 5: drag, selection highlight, copy on release, click, auto-scroll

**Files:**
- Modify: `internal/ui/mouse.go`, `internal/ui/app.go` (`View` paints the selection before `compose`), `internal/ui/mode_normal.go` and `internal/ui/mode_insert.go` (`esc` clears an active selection first), `internal/ui/mode_picker.go` (opening cancels a drag), `internal/ui/detailsctl.go` (`SetContent` clears a details selection)
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `selection.*` (Task 1), `blocklist.HitTest`/`Lines` (Task 2), `details.(Model).HitTest`/`Lines` (Task 3), `theme.Set.Selection` (Task 4).
- Produces:
  - `mouseState` fields:
    - `phase`: `dragIdle`, `dragPressed`, or `dragMoving`.
    - `pane`: a `pane` value.
    - `sel`: a `selection.Range`.
    - `last`: the last pointer cell, a `struct{ x, y int }`.
    - `autoGen`: an `int`.
  - Message: `mouseScrollMsg{gen int}`.
  - Paint function: `paintSelection(a *App, pane pane, rendered string, r wintree.Rect) string`.

- [ ] **Step 1: Write the failing tests** (in `mouse_test.go`, each resuming a known session; the clipboard is checked with `%T` containing `ClipboardMsg`, as `TestNormal_YankCmdAndHint` does, plus the hint)
  - `TestMouse_DragCopiesTextAndHints`: press at a text block's cell, motion 6 cells right, release.
    - The run produces tea's clipboard message.
    - `a.view.hint == "copied 6 chars"`.
    - The frame shows the selection's `On` escape on that row.
  - `TestMouse_DragAcrossBlocksCopiesInOrder`: drag from a cell in block 2 back up into block 1. The copied text is in document order, which `selection.Text` over `blocklist.Lines` produces.
  - `TestMouse_DragPinnedToStartPane`: press in the transcript, move into the sidebar (x past `a.lay.Transcript.W`) and into the prompt row. The range's `End` stays inside the transcript: the last column, then the last visible row.
  - `TestMouse_ClickSelectsBlock`: press and release at the same cell of block 1 while block 3 is selected. `a.w.list.Selected().ID` is block 1, nothing is copied, and no hint is shown.
  - `TestMouse_ClickOnCardDisarms`: resume with a pending permission on block 1, select block 3, and click block 1. `a.w.card` is disarmed, which the existing `permCtl.sync` → `guard` does on arrival. Its keys do nothing until `ta.clk.Advance(cardArmDelay); ta.fire()`.
  - `TestMouse_AutoScrollAtEdge`:
    - Press mid-transcript, then drag onto row 0 of `a.lay.Transcript`.
    - Each `ta.clk.Advance(autoScrollEvery); ta.fire()` scrolls up one line and extends the range.
    - After release, a further fire scrolls nothing.
  - `TestMouse_EscClearsSelection`: after a drag, `esc` clears the highlight. In INSERT mode `esc` also still switches to NORMAL, since the selection is cleared and the key is then handled as before.
  - `TestMouse_PickerCancelsDrag`: press and move, then `ctrl+p`, then release. Nothing is copied and the selection is cleared.
  - `TestMouse_DetailsSelection`: drag in the details body, and the copied text comes from `details.Lines()`. Opening another block's details clears it.
  - `TestMouse_SelectionSurvivesStreaming`: select inside a streaming text block, then deliver a `TextDelta` and fire. The highlight is still on the same text, and the range is unchanged.
- [ ] **Step 2: Run to verify they fail**
  Run: `go test ./internal/ui/ -run 'TestMouse_(Drag|Click|AutoScroll|Esc|Picker|Details|Selection)'` → FAIL.
- [ ] **Step 3: Implement in `mouse.go`**
  - **Press** (`MouseClickMsg`, `MouseLeft`):
    - `paneAt` gives the pane, and `HitTest` gives a `Point`.
    - Pane ID `"details"` for details, or the block ID for the transcript.
    - Sets `phase=dragPressed` and `sel={Start: p, End: p, Active: false}`.
    - A pane with a body height ≤ 1 starts nothing.
  - **Motion:**
    - Pin (x, y) to the start pane's rect.
    - `HitTest`, and set `sel.End`, `Active=true`, `phase=dragMoving`.
    - On an edge row, schedule `a.after(autoScrollEvery, mouseScrollMsg{gen})` with `autoGen++`.
  - **`mouseScrollMsg`:** when `gen == autoGen` and `phase == dragMoving` and the pointer is still on an edge row, `ScrollBy(∓1)` the pane, re-`HitTest` `last`, and re-schedule.
  - **Release:**
    - After `dragMoving`: `selection.Text`, then `tea.SetClipboard(text)` and `a.view.hint = fmt.Sprintf("copied %d chars", utf8.RuneCountInString(text))`.
    - After `dragPressed` in the transcript: `a.w.list.Select(id)`.
    - Then `phase=dragIdle` and `autoGen++`, which cancels the auto-scroll.
  - **Paint:** `paintSelection` walks the pane's visible rows. For the transcript, row y maps to (ID, line) through `blocklist.HitTest(m, 1, y)` (x=1 is the first content cell). For details, it maps through `details.HitTest(0, y)`. Each touched row goes through `selection.Highlight` with `a.theme.set.Selection.On/Off`, skipping the transcript's prefix column (the first cell) so the `▌` bar is never highlighted. Only rows the range touches are processed.
  - **Clearing the selection:**
    - A press, a wheel event, or `esc` (checked first in both mode handlers, before their fixed keys; it returns nil only for NORMAL, so INSERT's `esc` still switches to NORMAL).
    - Opening the picker.
    - `detailsCtl` calling `SetContent`, for a details selection.
    - A `Load` that drops the selected block, for a transcript selection: check in `paintSelection`, and clear when `a.sess.proj.Block(id)` is missing.
- [ ] **Step 4: Run to verify they pass, plus the ui budgets**
  Run: `go test ./internal/ui/... ./internal/archtest/ && go test -run XXX -bench 'Resize2000|DetailsToggle2000' -benchmem ./internal/ui/`
  Expected: PASS; `Resize2000` < 1.5 s and `DetailsToggle2000` < 50 ms.
- [ ] **Step 5: Commit** (after `make check`)
  `git commit -m "feat(ui): mouse text selection with highlight, copy on release, click, auto-scroll"`

---

### Task 6: e2e and docs

**Files:**
- Modify: `e2e/tui_test.go`, `AGENTS.md` (the TUI section: mouse behavior; the shared-code table: `selection`, `blocklist.HitTest`/`Lines`)

**Interfaces:**
- Consumes: the built binary from `make build` under `pty.StartWithSize`, with `newScreen`/`.waitFor`/`.mark`.

- [ ] **Step 1: Write the failing e2e test** `TestE2E_TUIMouseScrollAndCopy`:
  - **Setup:** run a scripted jigtest session producing 60 lines of text, at 30×100.
  - **Wheel:** write the SGR wheel-up sequence `"\x1b[<64;10;5M"` three times. `waitFor` a line from earlier in the text to appear.
  - **Drag:** write press `"\x1b[<0;5;4M"`, motion `"\x1b[<32;15;4M"`, and release `"\x1b[<0;15;4m"`.
  - **Clipboard:** `waitFor` the raw (unstripped) output to contain the OSC 52 prefix `"\x1b]52;c;"`. Add a `rawContains` helper next to `.text()`, since `waitFor` strips escapes.
- [ ] **Step 2: Prove it can fail**
  Run: `make build && go test -tags jigtest ./e2e/ -run MouseScrollAndCopy -count=1`
  Expected: PASS on the current tree. To confirm the clipboard assertion is real, temporarily delete the `tea.SetClipboard` call in `mouse.go`'s release branch, rebuild and re-run. It must FAIL on the OSC 52 `waitFor`. Then restore the call with `git checkout internal/ui/mouse.go`.
- [ ] **Step 3: Update AGENTS.md**
  - **INSERT and NORMAL mode bullets:** the wheel scrolls the transcript and the details split in both modes, a drag selects and copies on release, and `esc` clears a selection first.
  - **Shared-code table:** add a row for `selection.Highlight`/`Text` and `blocklist.HitTest`/`Lines`.
- [ ] **Step 4: Run** `make check` → PASS.
- [ ] **Step 5: Commit**
  `git commit -m "test(e2e): mouse wheel and drag-to-copy; docs(agents): mouse behavior"`
