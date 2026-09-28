# jig Plan 2a — TUI Substrate, Transcript, Trust, and Images Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land everything the Plan 2 TUI stands on that can ship and be tested without the App: the widget substrate (`ansi`, `overlay`, `scrollbar`, `wintree`), the `ui/transcript` projection, and every service, port, event, and data addition in spec §8–§9. That covers project-config trust, blobs, images in tool results and attachments, and the headless side of the agent-browser integration. It ends as working headless software: `jig run --trust-project`, `jig run --attach`, image `read`, and agent-browser skills, presets, and screenshots, all e2e-tested.

**Architecture:** Plan 1's four layers are unchanged. New data packages (`blobfs`, `prefsfs`, `trustfs`, `atomicfile`) sit under `internal/data`. New services (`trust`, `media`) sit under `internal/service` and declare the interfaces they need. `internal/bubbles` is a new widget tree whose substrate packages land here. `internal/ui/transcript` is a pure projection from events and messages to blocks. Plan 2b builds the widgets and the App on top.

**Tech Stack:** Go 1.27; existing deps (fantasy v0.45.2, catwalk v0.52.56, sqlite, toml, yaml). New in this plan: `charm.land/lipgloss/v2` v2.0.6 and `github.com/charmbracelet/x/ansi` v0.11.8 (substrate), `golang.org/x/image` v0.46.0 (webp decode and scaling).

**Spec:** `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md` (and Plan 1's `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` for the unchanged parts). Plan 2b: `docs/superpowers/plans/2026-09-27-jig-plan-2b-tui.md`.

**Research notes** (verified API signatures; read these before a task that touches a library): `.superpowers/handoff/research/{lib-apis,slk,jig-core-data,jig-services,jig-client-app}.md`.

## Global Constraints

- Every Plan 1 constraint still holds: stdlib `testing` only, white-box tests, no `time.Sleep`/`time.Now` in tests, no package-level mutable vars, files ≤ 500 lines, `internal/app` funcs ≤ 40 lines, structs ≤ 15 fields and ≤ 20 methods, `funlen` 80, `gocognit` 30.
- Every commit passes `make check`. `AGENTS.md` is updated in the same commit as any new shared helper, port, invariant, or package.
- Allowed new deps in 2a: `charm.land/lipgloss/v2` v2.0.6, `github.com/charmbracelet/x/ansi` v0.11.8, `golang.org/x/image` v0.46.0. Adding anything else requires a line in the task explaining why. Add a dep in the task that first imports it (`go get <mod>@<ver>` then `go mod tidy`).
- Layer rules: Plan 1 rules plus spec §3.3, as extended by Task 1 (see Rulings R1–R2).
- Numbers from the spec: image long edge ≤ 1568 px; re-encode as JPEG for photos whose PNG exceeds 1 MiB; refuse > 5 MiB after encoding; text attachments ≤ 50 KB; agent-browser skill discovery timeout 3 s; blob path `$XDG_DATA_HOME/jig/blobs/<sha256[:2]>/<sha256>`; prefs `$XDG_STATE_HOME/jig/prefs.json`; trust `$XDG_DATA_HOME/jig/trust.json`.
- Exact user-facing copy from the spec: `warning: project config not trusted; loosening settings ignored (use --trust-project)`; `[image omitted: <model> does not accept images]`; image read output `image WxH (<bytes>)`.
- **Untrusted text is data.** Nothing in this plan renders to a terminal, but every string that Plan 2b will render (model text, tool output, file content, paths, titles, effects) goes through `bubbles/ansi.Sanitize`/`SanitizeLine` at the render boundary. Task 2 builds and tests them.
- Secrets never appear in user-facing text: trust effects print `api_key → (set)`, never the value.
- **No slash commands, ever.** `ext.Command` is renamed in docs to "picker action"; its interface is unchanged.
- Subagent models: `anthropic/claude-sonnet-5` by default; `anthropic/claude-opus-5-5` for tasks marked **(opus)**. Always set the model explicitly.

## Rulings (decisions the spec left open; recorded here and in the ledger)

- **R1 — bubbles third-party allowlist.** `internal/bubbles/...` may import stdlib, `charm.land/...`, `github.com/charmbracelet/x/ansi`, `github.com/alecthomas/chroma/v2`, `golang.org/x/image/...`, `github.com/sahilm/fuzzy` (picker ranking), and `github.com/aymanbagabas/go-udiff` (diffs in `coderender`). The spec lists `charm.land` but the ANSI helpers live at `github.com/charmbracelet/x/ansi`; fuzzy and udiff replace hand-rolled code.
- **R2 — ui may import bubbles.** `internal/ui/...` may import `internal/bubbles/...` in addition to `core`, `ui`, and `clock`. Its third-party rule stays `charm.land/...` only. So `ui` reaches x/ansi through `bubbles/ansi`.
- **R3 — core changes land before the transcript.** `ui/transcript` needs `event.Base.RootID`, `StepFinished`, and `SubagentSpawned.CallID`, so the event task (Task 6) precedes the transcript (Tasks 7–8). This deviates from the handoff's order for a dependency reason.
- **R4 — `core.Media{MIME, Ref string; Data []byte}`.** §9.1's name `Ref` wins over §8.2's `BlobRef`. `Data` is `json:"-"`: it is never stored and is filled only by `client/llm` just before conversion.
- **R5 — `SubagentSpawned` gains `CallID`.** This maps a child session to the `task` tool block that spawned it, which is ambiguous with parallel task calls otherwise.
- **R6 — `core.ErrBusy`.** `agent.ErrBusy` moves to `core.ErrBusy = errors.New("session is busy")` so the UI can match it. The Runner owns busyness: `Runner.Exclusive` registers a compaction in the same `running` map as a turn.
- **R7 — the tighten-only merge is conservative where the spec's literal rule is unsound.** Checking a project pattern `P` only against the baseline's verdict on the literal string `P` can loosen. Example: the baseline has `"*"=allow` and `"git push*"=deny`. A project adds `"gi* push --force"=allow`, whose literal matches only `"*"` and so passes the check. That pattern is more specific than `"git push*"`, so it wins on `git push --force`, turning a deny into an allow. So, per tool:
  - A project `Default` is applied iff it ranks ≥ the baseline default (empty = ask).
  - A `deny` pattern is always applied.
  - An `ask` pattern is applied iff the baseline rule for that tool contains no `deny` anywhere (Default or pattern).
  - An `allow` pattern is never applied.

  This is never looser than the baseline, and it still honors every tightening the spec's examples need. Reviewers must probe it.
- **R8 — Effects are computed in `service/trust`**, not `config.Load`, because they span TOML and markdown agent files. `config.Load` returns the Global and Project layers separately (spec §8.3).
- **R9 — `--trust-project` persists the grant**, exactly as pressing `t` in the dialog does. The warning prints only when the untrusted merge actually dropped something.
- **R10 — custom providers declare image support** with a new provider key, `image_models = ["m"]`. Catalog models use `catwalk.Model.SupportsImages`. Without it, a custom provider can never receive images, and the e2e tests could not exercise the media path.
- **R11 — `jig run --attach PATH`** (repeatable) is the headless way to send attachments; spec §10's `TestE2E_ImageAttachment` needs one.
- **R12 — `Runner.Run(ctx, rc, text string, atts ...core.Attachment)`.** It is variadic, so the `task` and `Proxy` call sites stay unchanged.
- **R13 — screenshot directories.** An image path named by `agent-browser screenshot` output is attached only if it resolves (after `EvalSymlinks`) under the workdir, or under `os.TempDir()` with a base name starting `screenshot`. agent-browser's default output is `Screenshot saved to /tmp/screenshot-<ts>.png`. `AGENT_BROWSER_SCREENSHOT_DIR` is not honored.
- **R14 — the agent-browser skills dir** is the directory `agent-browser skills path` prints. If that directory itself holds a `SKILL.md`, its parent is used, because skillfs scans a directory's children.
- **R15 — golden files** live at `testdata/golden/<name>.ansi` next to the test. They are updated with `JIG_UPDATE_GOLDEN=1 go test ./...`, not an `-update` flag. A mismatch writes `<name>.ansi.actual`, which is gitignored.
- **R16 — write vs. edit in `ChangedFiles`.** A successful `write` counts as `added` unless the projection has already seen that path read, written, or edited; then it counts as `modified`. `edit` is always `modified`. This works because Plan 1's tracker refuses to overwrite an unread file, so a write to an unseen path creates it.
- **R17 — stored session `Cwd` is canonical.** `internal/app` sets `workDir = pathid.Key(workDir)` at startup, and `ListForCwd` matches on `pathid.Key(cwd)`. Pre-Plan-2 sessions stored with a symlinked cwd still resume (via `pathid`) but don't show in the session picker.
- **R18 — the max-steps notice.** A text part or delta whose trimmed text matches `^\[stopped: reached max_steps \(\d+\)\]$` becomes a `Notice` block. `RunFailed{Err: "cancelled"}` becomes a dim `cancelled` notice; any other `RunFailed` becomes a red `run failed: <err>` notice.

## Review Focus

1. **An untrusted project config tries to loosen a permission**, directly or through the pattern-specificity trick in R7, or through a project agent overriding a built-in agent's `ask`. The effective decision is never less restrictive than without the project layer. Tested in Task 14: `TestTighten_AllowPatternNeverApplied`, `TestTighten_AskPatternDroppedWhenBaselineHasDeny`, `TestRestrict_ProjectAgentCannotLoosenBuiltin`. End to end in Task 15: `TestE2E_UntrustedProjectCannotLoosen`.
2. **Hostile escape sequences in text jig will render.** Model output, tool output, file contents, filenames, or session titles can contain OSC 52 clipboard writes, kitty APC uploads, CSI screen clears, 8-bit C1 introducers, or a forged kitty placeholder rune. `Sanitize` makes all of them inert while keeping printable text, `\n`, and `\t`. Tested in Task 2: `TestSanitize_StripsHostileSequences`.
3. **A screenshot or attachment path escapes its allowed directory** via a symlink, `..`, or an absolute path outside the workdir, or a temp-dir file not named `screenshot*`. Nothing is attached, and the tool result still succeeds. Tested in Task 21: `TestScreenshots_RefusesEscapes`. A blob ref that is not 64 hex chars is refused before touching the filesystem: Task 11, `TestBlobfs_RejectsBadRefs`.
4. **A decompression bomb, a truncated or corrupt image, or an image over 5 MiB after re-encoding** returns a tool error result or a ConfigError. It never panics or allocates unbounded memory. Tested in Task 16: `TestProcess_RefusesHugeDimensions`, `TestProcess_CorruptImageIsError`, `TestProcess_TooLargeAfterEncode`.
5. **Rename, Configure, a background title, Touch, and Compact race a running turn.** No update is lost: a user's rename is never clobbered by a late title. Compact during a run returns `core.ErrBusy`, and a run during a compaction returns `core.ErrBusy`. Tested under `-race` in Task 9 (`TestGenerateTitle_DoesNotClobberRename`, `TestModify_ConcurrentRenameAndConfigure`) and Task 10 (`TestCompact_BusyDuringRun`, `TestRun_BusyDuringCompact`).

## File Map (new and heavily changed)

```
internal/archtest/bubbles_test.go            §3.3 rules (Task 1)
internal/golden/golden.go                     golden-file assertion helper (Task 1)
internal/pathid/                              (exists) path identity key
internal/bubbles/ansi/                        Sanitize, Highlight, width/cut helpers, PlaceholderRune
internal/bubbles/overlay/  scrollbar/  wintree/   ported substrate
internal/core/media.go                        Media, Attachment, ImageInfo
internal/core/errors.go                       ErrBusy
internal/core/prefs.go                        Prefs + PrefsService
internal/core/event/event.go                  RootID, StepFinished, SessionUpdated, SubagentSpawned.CallID
internal/ui/transcript/                       Projection (Tasks 7–8)
internal/data/atomicfile/  blobfs/  prefsfs/  trustfs/
internal/data/config/                         layered Load + Merge (Task 13)
internal/service/trust/                       Restrict + Effects (Task 14)
internal/service/permission/tighten.go        Tighten (Task 14), AgentBrowserPreset (Task 20)
internal/service/media/                       image pipeline (Task 16)
internal/service/tools/screenshot.go          agent-browser screenshot attach (Task 21)
internal/client/llm/media.go                  mediaLLM wrapper (Task 18)
internal/client/agentbrowser/                 detection + skills path (Task 20)
internal/app/trust.go                         trust decision + warning (Task 15)
```

---

### Task 1: Archtest rules for Plan 2 and the golden helper

**Model:** sonnet

**Files:**
- Create: `internal/archtest/bubbles_test.go`, `internal/golden/golden.go`
- Modify: `internal/archtest/layers_test.go` (UI rule allows `internal/bubbles/...`), `AGENTS.md`
- Test: `internal/archtest/bubbles_test.go`, `internal/golden/golden_test.go`

**Interfaces:**
- Produces:
  ```go
  package golden
  // Assert compares got with testdata/golden/<name>.ansi (relative to the
  // test's package dir). With JIG_UPDATE_GOLDEN=1 it (re)writes the file and
  // passes. On mismatch it writes <name>.ansi.actual and fails, reporting the
  // first differing line of each side with %q.
  func Assert(t testing.TB, name, got string)
  ```

- [ ] **Step 1: Write the failing archtest tests** in `bubbles_test.go`. Each reports `path:line: rule: detail`, like Plan 1's.
  - `TestLayers_BubblesImports`: non-test files under `internal/bubbles/`.
    - An `internal/...` import is allowed only if it is one of `internal/bubbles/ansi`, `internal/bubbles/overlay`, `internal/bubbles/scrollbar`, or `internal/bubbles/wintree`, and is not the file's own package.
    - A non-stdlib import must have one of these prefixes: `charm.land/`, `github.com/charmbracelet/x/ansi`, `github.com/alecthomas/chroma/v2`, `golang.org/x/image/`, `github.com/sahilm/fuzzy`, `github.com/aymanbagabas/go-udiff` (R1).
  - `TestBubbles_NoFuncMsgFields`: no struct field in `internal/bubbles/` has type `func(<tea>.Msg)`, where `<tea>` is the file's local name for `charm.land/bubbletea/v2`.
  - `TestBubbles_ViewTakesNoParams`: every method named `View` declared in `internal/bubbles/` has zero parameters.
  - `TestLayers_TranscriptImportsOnlyCore`: non-test files under `internal/ui/transcript` import only stdlib and `internal/core/...`.
  - In `layers_test.go`, `TestLayers_UIImportsOnlyCoreAndUI` also accepts `internal/bubbles/...` (R2). Update its error text to match.
- [ ] **Step 2: Self-check that each new rule bites.** Add a scratch `internal/bubbles/x/x.go` with, in turn:
  - an import of `internal/core`;
  - a struct field `f func(tea.Msg)`;
  - `func (M) View(w int) string`.

  Confirm each fails the matching test, then delete the scratch file.
- [ ] **Step 3: Write `internal/golden/golden_test.go`**:
  - `TestAssert_UpdateThenMatch`: with `t.Setenv("JIG_UPDATE_GOLDEN","1")`, `Assert` writes the file; without the env var, the same content passes.
  - `TestAssert_MismatchWritesActual`: runs `Assert` through a fake `testing.TB` (embed `testing.TB`, override `Errorf`/`Fatalf`/`Helper`), and checks that the `.actual` file exists and the failure message contains `%q`-quoted lines.

  Use `t.Chdir(t.TempDir())` so the testdata lands in a temp dir.
- [ ] **Step 4: Run** `go test ./internal/archtest/ ./internal/golden/`. Expected: FAIL (undefined `Assert`); the archtest tests PASS on the current tree.
- [ ] **Step 5: Implement `golden.Assert`.** Create directories 0755 and files 0644. Normalize nothing: goldens are byte-exact.
- [ ] **Step 6: Run** `make check`. Expected: PASS.
- [ ] **Step 7: Update `AGENTS.md`.**
  - Add the §3.3 rules to "Dependency rules".
  - Add `internal/golden/` and `internal/bubbles/` to the map.
  - Add a shared-code row: "Golden-frame assertion | `golden.Assert(t, name, got)`; update with `JIG_UPDATE_GOLDEN=1`".
- [ ] **Step 8: Commit.** `git add -A && git commit -m "test(archtest): Plan 2 bubbles/transcript rules; golden helper"`

### Task 2: `bubbles/ansi` — sanitize, highlight, width helpers

**Model:** opus (security boundary)

**Files:**
- Create: `internal/bubbles/ansi/ansi.go`, `internal/bubbles/ansi/sanitize.go`, `internal/bubbles/ansi/highlight.go`
- Test: `internal/bubbles/ansi/sanitize_test.go`, `internal/bubbles/ansi/highlight_test.go`

**Interfaces:**
- Produces:
  ```go
  package ansi   // import xansi "github.com/charmbracelet/x/ansi"
  const PlaceholderRune = '\U0010EEEE'   // kitty unicode-placeholder cell; shared with overlay and imgrender
  func Sanitize(s string) string       // untrusted → inert: keeps printable runes, '\n', '\t'
  func SanitizeLine(s string) string   // Sanitize, then '\n' and '\t' → ' '
  func Width(s string) int                               // xansi.StringWidth
  func Truncate(s string, width int, tail string) string // xansi.Truncate
  func Cut(s string, left, right int) string             // xansi.Cut
  func Wrap(s string, width int) string                  // xansi.Wordwrap, then xansi.Hardwrap for words longer than width
  // Highlight wraps every case-insensitive occurrence of query in the
  // *visible* text of s with on/off. Existing escape sequences are copied
  // byte-for-byte and never matched inside (e.g. an OSC 8 URL).
  func Highlight(s, query, on, off string) string
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestSanitize_StripsHostileSequences`: a table in which each output contains no `\x1b`, no rune in `0x80–0x9f`, and no `PlaceholderRune`, and keeps the visible text:

    | input | want |
    |---|---|
    | `"a\x1b]52;c;aGk=\x07b"` (OSC 52, BEL-terminated) | `"ab"` |
    | `"a\x1b]52;c;aGk=\x1b\\b"` (ST-terminated) | `"ab"` |
    | `"x\x1b_Ga=T,f=100;AAAA\x1b\\y"` (kitty APC) | `"xy"` |
    | `"\x1b[2J\x1b[Hhi"` (CSI) | `"hi"` |
    | `"a\u009b31mb"` (8-bit CSI) | `"ab"` |
    | `"a\x1bPq#0;2;0;0;0\x1b\\b"` (DCS/sixel) | `"ab"` |
    | `"a\x1b"` (lone trailing ESC) | `"a"` |
    | `"r\rn"` | `"rn"` |
    | `"l1\r\nl2"` | `"l1\nl2"` |
    | `"bell\x07del\x7f"` | `"belldel"` |
    | `"tab\there\nnext"` | unchanged |
    | `"ünï 日本 🎉"` | unchanged |
    | `"fake" + string(PlaceholderRune) + "\u0305"` | `"fake\u0305"` |

    An unterminated OSC (`"a\x1b]0;title"`) drops everything from the ESC to the end.
  - `TestSanitizeLine_FlattensNewlinesAndTabs`: `"a\nb\tc"` → `"a b c"`.
  - Highlight rule for all three tests below: `on` goes immediately before the first visible rune of a match and `off` immediately after its last visible rune. Escape sequences keep their byte positions, including any that fall inside a match.
  - `TestHighlight_VisibleTextOnly`:
    - `Highlight("\x1b[31mHello\x1b[0m world", "hello", "[", "]")` → `"\x1b[31m[Hello]\x1b[0m world"`.
    - A query that occurs only inside an OSC 8 URL (`"\x1b]8;;http://x/foo\x1b\\link\x1b]8;;\x1b\\"`, query `"foo"`) returns the input unchanged.
  - `TestHighlight_MatchAcrossEscape`: `Highlight("ab\x1b[1mcd", "bc", "[", "]")` → `"a[b\x1b[1mc]d"`.
  - `TestWrap_LongWordHardWraps`: every line of `Wrap("aaaaaaaaaa", 4)` has width ≤ 4.
- [ ] **Step 2: Run** `go test ./internal/bubbles/ansi/`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement.**
  - `Sanitize` is a single-pass byte scanner over UTF-8:
    - ESC followed by `[` → a CSI, skipped to its final byte `0x40–0x7e`.
    - ESC followed by `]`, `P`, `_`, `^`, or `X` → a string sequence, skipped to BEL or ST (`ESC \`), or to the end.
    - Any other ESC plus the next byte is dropped (`ESC c`, `ESC 7`, SS2/SS3).
    - The runes `0x80–0x9f` are C1 controls; `0x9b` (CSI) and `0x9d` (OSC) start sequences, skipped the same way.
    - Other C0 controls except `\n` and `\t`, plus DEL and `PlaceholderRune`, are dropped. A `\r` is dropped (`\r\n` → `\n`).
    - Invalid UTF-8 bytes become U+FFFD.

    Do not use `xansi.Strip`: it keeps C0 controls and doesn't treat C1 the same way.
  - `Highlight` segments `s` with `xansi.DecodeSequence` into escape and visible runs, builds the lowercase visible text with a rune → byte-position map, and inserts `on`/`off` at the mapped positions.
- [ ] **Step 4: Run** `go test ./internal/bubbles/ansi/ -race`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.**
  - Add an Invariant: "Every string from a model, a tool, a file, or the store passes `ansi.Sanitize` (or `SanitizeLine`) before it is rendered; only jig's own styling escapes reach the terminal."
  - Add shared-code rows for `Sanitize`, `SanitizeLine`, `Highlight`, and `Wrap`.
- [ ] **Step 6: Commit.** `git commit -am "feat(bubbles/ansi): sanitize untrusted text, ANSI-safe highlight and wrap"` (after `git add` of the new files).

### Task 3: `bubbles/overlay` and `bubbles/scrollbar` (ported from slk)

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/overlay/overlay.go`, `internal/bubbles/scrollbar/scrollbar.go`
- Test: `internal/bubbles/overlay/overlay_test.go`, `internal/bubbles/scrollbar/scrollbar_test.go`
- Source to port: `~/local_code/slk/internal/ui/overlay/`, `~/local_code/slk/internal/ui/scrollbar/` (MIT, same author; keep a one-line "Ported from slk" package comment)

**Interfaces:**
- Produces:
  ```go
  package overlay
  // Center composites box centered over background (width×height cells),
  // darkening the background by dim (0..1) with lipgloss.Darken. Wide-char
  // continuation cells are skipped; rows holding ansi.PlaceholderRune keep
  // the original box bytes (kitty image IDs are encoded in SGR colors).
  func Center(background string, width, height int, box string, dim float64) string

  package scrollbar
  func Overlay(visible []string, width, total, yOffset, visibleHeight int, bg, trackFg, thumbFg color.Color) []string
  func Visible(total, visibleHeight int) bool
  ```

- [ ] **Step 1: Port the tests first:** slk's `overlay_test.go`, `overlay_widechar_test.go`, and `scrollbar_test.go`. Change package names, the `DimmedOverlay` → `Center` argument order, and `image.PlaceholderRune` → `ansi.PlaceholderRune`. Add `TestCenter_BoxLargerThanBackgroundIsClipped`: a 10×3 box over a 6×2 background returns 2 lines, each of width 6, with no panic.
- [ ] **Step 2: Run** `go test ./internal/bubbles/overlay/ ./internal/bubbles/scrollbar/`. Expected: FAIL (undefined).
- [ ] **Step 3: Port the implementations.** Add `charm.land/lipgloss/v2@v2.0.6` and `github.com/charmbracelet/x/ansi@v0.11.8` to `go.mod`. Neither package reads any theme: colors are parameters.
- [ ] **Step 4: Run** `go test ./internal/bubbles/... -race`, then `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles): overlay and scrollbar substrate (ported from slk)"`

### Task 4: `bubbles/wintree` with fixed-size leaves

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/wintree/{wintree.go,layout.go,ops.go,navigate.go}`
- Test: `internal/bubbles/wintree/{wintree_test.go,ops_test.go,navigate_test.go}`
- Source to port: `~/local_code/slk/internal/ui/wintree/`

**Interfaces:**
- Produces (slk's API minus `Channel`, plus fixed sizes):
  ```go
  package wintree
  type Dir int         // SplitStacked, SplitSideBySide
  type NavDir int      // NavLeft, NavDown, NavUp, NavRight
  type LeafID int
  type Rect struct{ X, Y, W, H int }
  const MinWidth, MinHeight = 20, 3   // flex leaves only
  var ErrNotFound, ErrNoRoom, ErrLastWindow = errors.New(...), ...   // names as slk (Err* vars pass hygiene)
  func New() (*Tree, LeafID)
  func (t *Tree) Len() int
  func (t *Tree) Leaves() []LeafID
  func (t *Tree) Split(id LeafID, dir Dir, bounds Rect) (LeafID, error)
  func (t *Tree) Close(id LeafID) (LeafID, error)
  func (t *Tree) Only(id LeafID) error
  func (t *Tree) Cycle(id LeafID, delta int) LeafID
  func (t *Tree) NavigateDir(id LeafID, nd NavDir, bounds Rect) (LeafID, bool)
  // SetFixed pins id to n cells along its parent split's axis (0 = flex).
  func (t *Tree) SetFixed(id LeafID, n int) error
  func (t *Tree) Layout(bounds Rect) LayoutNode
  func (t *Tree) ComputeRects(bounds Rect) map[LeafID]Rect
  ```
  Layout rule: fixed children get `min(n, remaining)` first. The flex children then share the rest equally, with the remainder going one cell each to the earliest. If the fixed children alone overflow, they are shrunk from the last one backwards. The `MinWidth`/`MinHeight` checks apply to flex leaves only.

- [ ] **Step 1: Port slk's tests** with `Channel` removed.
- [ ] **Step 2: Add the fixed-size tests.**
  - `TestLayout_FixedLeaves`: the Plan 2 shape — `t, tr := New()`; `p, _ := t.Split(tr, SplitStacked, b)`; `SetFixed(p, 5)`; `s, _ := t.Split(tr, SplitSideBySide, b)`; `SetFixed(s, 40)`. With `b = Rect{0,0,120,40}`:
    - `tr` = `{0,0,80,35}`
    - `s` = `{80,0,40,35}`
    - `p` = `{0,35,120,5}`
  - `TestLayout_FixedOverflowShrinks`: fixed 50 + fixed 50 inside a width of 60 → `{0,0,50,h}` and `{50,0,10,h}`.
  - `TestClose_SidebarRestoresFullWidth`: after `Close(s)`, `tr` is `{0,0,120,35}`.
- [ ] **Step 3: Run** `go test ./internal/bubbles/wintree/`. Expected: FAIL.
- [ ] **Step 4: Port and extend.** Add `fixed int` to the node.
- [ ] **Step 5: Run** `go test ./internal/bubbles/wintree/ -race` and `make check`. Expected: PASS.
- [ ] **Step 6: Commit.** `git commit -m "feat(bubbles/wintree): window tree with fixed-size leaves (ported from slk)"`

### Task 5: Core value types for media and attachments; store round-trip

**Model:** sonnet

**Files:**
- Create: `internal/core/media.go`, `internal/core/errors.go`
- Modify: `internal/core/message.go` (PartAttachment, `Part.Attachment`, `ToolResult.Media`), `internal/core/model.go` (`ModelInfo.SupportsImages`), `internal/data/store/parts.go` (or wherever `encodePart`/`decodePart` live), `internal/service/agent/runner.go` + callers (`ErrBusy` → `core.ErrBusy`)
- Test: `internal/data/store/parts_test.go` (or the existing messages test file), `internal/core/media_test.go`

**Interfaces:**
- Produces:
  ```go
  package core
  const PartAttachment PartKind = "attachment"
  type Media struct {
      MIME string
      Ref  string            // blob sha256 (64 lowercase hex)
      Data []byte `json:"-"` // filled only by client/llm just before conversion (R4)
  }
  type Attachment struct {
      Path    string         // absolute path as resolved by chat
      Content string         // text attachments
      Media   *Media         // image attachments
  }
  type ImageInfo struct{ Width, Height, Bytes int }
  // Part gains:       Attachment *Attachment
  // ToolResult gains: Media []Media
  // ModelInfo gains:  SupportsImages bool
  var ErrBusy = errors.New("session is busy")
  ```
  `agent.ErrBusy` is deleted. `Runner.register` returns `core.ErrBusy` (R6), and `runner_test.go` asserts `errors.Is(err, core.ErrBusy)`.

- [ ] **Step 1: Write the failing tests.**
  - `TestStore_AttachmentPartRoundTrips`: save a user message with a text attachment `{Path:"/w/a.go", Content:"package a"}` and an image attachment `{Path:"/w/s.png", Media:&Media{MIME:"image/png", Ref:<64 hex>, Data:[]byte{1}}}`. After reloading, the message deep-equals the original except that `Media.Data` is nil.
  - `TestStore_ToolResultMediaRoundTrips`: the same round trip for a `ToolResult{..., Media: []Media{{MIME:"image/png", Ref: r}}}`.
  - `TestStore_LegacyToolResultWithoutMedia`: a `data_json` stored without a `Media` key (insert raw JSON through the test's DB handle, or save a result with nil Media) decodes with `Media == nil`.
- [ ] **Step 2: Run** `go test ./internal/data/store/ ./internal/core/`. Expected: FAIL (undefined `PartAttachment`).
- [ ] **Step 3: Implement.** `encodePart` marshals `p.Attachment` for `PartAttachment`, and `decodePart` reverses it; an unknown kind stays an error. Replace `agent.ErrBusy` with `core.ErrBusy` everywhere (`grep -rn ErrBusy internal`).
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Add an Invariant: "`core.Media.Data` is never persisted; only `client/llm` fills it, from blobs, for a single request."
- [ ] **Step 6: Commit.** `git commit -am "feat(core): media, attachments, ErrBusy; store round-trips them"` (add the new files first).

### Task 6: Events carry `RootID`; `StepFinished`, `SessionUpdated`, `SubagentSpawned.CallID`

**Model:** sonnet

**Files:**
- Modify: `internal/core/event/event.go`
- Modify: every publisher — `service/session/session.go` (`SessionCreated`), `service/task/task.go`, `service/permission/asker.go` + `hook.go` (`Request.RootID`), `service/tools/todo.go`, `service/agent/{runner.go,stream.go,exec.go}`
- Test: `internal/core/event/event_test.go`, `internal/service/agent/events_test.go`, and new assertions in the existing task, permission, and todo tests

**Interfaces:**
- Produces:
  ```go
  package event
  type Base struct{ SessionID, RootID core.SessionID }
  func (b Base) Session() core.SessionID
  func (b Base) Root() core.SessionID          // RootID, or SessionID when RootID is empty
  type Event interface{ Session() core.SessionID; Root() core.SessionID }
  type StepFinished struct{ Base; MessageID core.MessageID; Usage core.Usage; CostUSD float64 }
  type SessionUpdated struct{ Base; Info core.Session }   // published by Task 9
  // SubagentSpawned gains CallID string (the parent's task tool call ID) (R5)
  package permission
  // Request gains RootID core.SessionID; Hook.ask sets it from rootKey(rc).
  ```
  `StepFinished` is published by `Runner.step` after the step's message (with its tool results) is saved, with `msg.Usage` and `msg.CostUSD`. It is published for every step, including the last one and a step that ends at max steps. It is not published for an aborted step.

- [ ] **Step 1: Write the failing tests.**
  - `TestBase_RootFallsBackToSession`: `Base{SessionID:"a"}.Root() == "a"`; `Base{SessionID:"a", RootID:"r"}.Root() == "r"`.
  - `TestRunner_EventsCarryRootID` (agent package): run with `rc.SessionID="ses_c"`, `rc.RootID="ses_r"`, and a scripted LLM that makes one tool call then replies with text. Every published event has `Root() == "ses_r"` and `Session() == "ses_c"`.
  - `TestRunner_PublishesStepFinishedPerStep`: two steps (a tool call, then text), each with usage `{Input:10, Output:5}` and model info `CostIn:1, CostOut:1`, give exactly 2 `StepFinished` events, with the MessageIDs of the two assistant messages and `CostUSD == 15e-6` each.
  - In the task tests: `SubagentSpawned.CallID == call.ID` and `.RootID == rc.RootID`.
  - In the permission tests: `PermissionRequested.RootID` and `PermissionResolved.RootID` equal the rc's root, including on the cancel path.
  - In the todo test: `TodosUpdated.RootID == rc.RootID`.
- [ ] **Step 2: Run** `go test ./internal/core/... ./internal/service/...`. Expected: FAIL.
- [ ] **Step 3: Implement.** Set `RootID: rc.RootID` at every construction site listed in `research/jig-core-data.md` §2's publish-site list. `session.Create` publishes `SessionCreated` with `RootID` = the new session's ID. `BusAsker` stores the request's `RootID` in `pendingRequest` so `Reply` can stamp `PermissionResolved`.
- [ ] **Step 4: Run** `make check`. Expected: PASS (the `ui/plain` tests are unaffected).
- [ ] **Step 5: `AGENTS.md`.** Add an Invariant: "Every event's `Base.RootID` is the root session of the run that produced it; the UI routes descendant events by it."
- [ ] **Step 6: Commit.** `git commit -am "feat(event): RootID on every event; StepFinished; SessionUpdated; SubagentSpawned.CallID"`

### Task 7: `ui/transcript` — blocks, streaming, tools, notices, Load

**Model:** opus (projection design)

**Files:**
- Create: `internal/ui/transcript/{block.go,projection.go,load.go,apply.go}`
- Test: `internal/ui/transcript/{load_test.go,apply_test.go}`

**Interfaces:**
- Consumes: `core.Message`, `core.Part` (incl. `PartAttachment`), `event.*` from Task 6.
- Produces:
  ```go
  package transcript
  type BlockID string
  type Kind int        // KindUser, KindText, KindReasoning, KindTool, KindSubagent, KindNotice
  type ToolState string
  const (StatePending ToolState = "pending"; StateAwaiting = "awaiting-permission"; StateRunning = "running";
         StateOK = "ok"; StateError = "error"; StateDenied = "denied"; StateCancelled = "cancelled")
  type Level int       // LevelInfo, LevelError
  type Block struct {
      ID          BlockID
      Version     int              // starts at 1; +1 on every change
      Kind        Kind
      MessageID   core.MessageID
      Text        string           // user, text, reasoning, notice
      Attachments []string         // user: attachment paths
      Call        *core.ToolCall   // tool
      Result      *core.ToolResult // tool
      State       ToolState        // tool, subagent
      Permission  *PendingPermission
      Sub         *Subagent        // subagent
      Level       Level            // notice
      Streaming   bool             // text/reasoning block still receiving deltas
  }
  type Subagent struct{ Child core.SessionID; Agent, Description string; Tools int; Current string }
  type PendingPermission struct{ RequestID string; Session core.SessionID; Tool, Subject string; Call core.ToolCall; Block BlockID; Subagent string }
  type FileChange struct{ Path string; Kind ChangeKind }   // ChangeAdded "added", ChangeModified "modified"
  func New(root core.SessionID) *Projection
  func (p *Projection) Load(msgs []core.Message)          // replaces all blocks
  func (p *Projection) Apply(ev event.Event) []BlockID    // IDs changed, in block order; nil if none
  func (p *Projection) Blocks() []Block                   // copy, in order
  func (p *Projection) Block(id BlockID) (Block, bool)
  func (p *Projection) ChangedFiles() []FileChange         // first-seen order (Task 8)
  func (p *Projection) Pending() []PendingPermission       // request order (Task 8)
  func (p *Projection) LastBrowserURL() string             // Task 8
  ```
  Block IDs:
  - `"u/<msgID>"` for a user message.
  - `"m/<msgID>/<n>"` for the n-th text or reasoning block of a message.
  - The tool call ID for a tool or subagent block.
  - `"n/<k>"` for the k-th notice.

  A `task` call becomes a `KindSubagent` block (spec §5.3). `Sub.Child` is parsed from the result's `<task_result session_id="…">`.

- [ ] **Step 1: Write the failing table tests.** Assert on `Blocks()` with `Version` zeroed unless the test is about versions.
  - `TestLoad_UserTextToolsAndReasoning`:
    - one user message (text + one attachment) followed by one assistant message with the parts reasoning, text, tool_call(read), tool_result(read ok);
    - expect: User(Text, Attachments=[path]), Reasoning, Text, Tool(State ok, Result set).
  - `TestLoad_InterruptedRunCancelsUnansweredCalls`: an assistant message with status interrupted, a tool_call with no result, and a result `"cancelled"` for another call → both tools are `StateCancelled`, followed by a dim notice `"cancelled"`.
  - `TestLoad_ToolStatesFromResults`:
    - `IsError` → `error`;
    - output `"denied by permission rule for bash"` → `denied`;
    - `"user denied: no"` → `denied`;
    - `"cancelled"` → `cancelled`;
    - otherwise `ok`.
  - `TestLoad_TaskCallBecomesSubagentBlock`: a `task` call with input `{"agent":"explore","description":"find x","prompt":"…"}` and result `<task_result session_id="ses_child">\ndone\n</task_result>` → KindSubagent, `Sub.Child == "ses_child"`, `Sub.Agent == "explore"`, `Sub.Description == "find x"`, State ok.
  - `TestLoad_CompactionAndMaxStepsNotices`: a compaction part → Notice LevelInfo `"compaction summary"` (Text holds the summary). A text part `[stopped: reached max_steps (40)]` → Notice LevelInfo (R18).
  - `TestApply_StreamingAppendsAndSplitsOnKindChange`: `MessageStarted(m1)`; `ReasoningDelta "a"`, `"b"`; `TextDelta "c"`; `ReasoningDelta "d"` → three blocks (reasoning "ab", text "c", reasoning "d"). Each delta returns exactly the changed block's ID, and the text block's `Version` increments per delta. A second `MessageStarted(m2)`, then `TextDelta` → a new text block.
  - `TestApply_StreamingFlagClearsOnStepEnd`: a text block has `Streaming == true` until `StepFinished` for its message (or `RunFinished`/`RunFailed`), and false after.
  - `TestApply_ToolLifecycle`: `ToolCallStarted` → Tool `running`; `ToolCallFinished` → ok/error/denied/cancelled, using the same rule as Load.
  - `TestApply_RunFailedNotices`: `RunFailed{Err:"cancelled"}` → LevelInfo `"cancelled"`; `RunFailed{Err:"boom"}` → LevelError `"run failed: boom"`.
  - `TestApply_IgnoresOtherRoots`: an event whose `Root()` differs from the projection's root → nil, and no change.
- [ ] **Step 2: Run** `go test ./internal/ui/transcript/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Keep `[]*Block` plus `map[BlockID]int`. Keep the open streaming block per message and kind in `map[core.MessageID]*Block`, cleared on a kind change.
  - The state rule is one function, `stateOf(r core.ToolResult) ToolState`, shared by Load and Apply.
  - Descendant handling is stubbed to "ignore" (`ev.Session() != root` → nil) until Task 8.
  - Split files to stay under 500 lines; imports are stdlib and core only.
- [ ] **Step 4: Run** `go test ./internal/ui/transcript/ -race` and `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui/transcript): block projection — load, streaming, tool lifecycle, notices"`

### Task 8: `ui/transcript` — subagents, permissions, changed files, browser URL

**Model:** opus

**Files:**
- Modify: `internal/ui/transcript/apply.go`, `load.go`; create `internal/ui/transcript/derived.go`
- Test: `internal/ui/transcript/subagent_test.go`, `internal/ui/transcript/derived_test.go`

**Interfaces:**
- Consumes/produces: Task 7's API. No new exported names.

- [ ] **Step 1: Write the failing tests.**
  - `TestApply_SubagentLifecycle`:
    - root `ToolCallStarted(task, id "c1")` → a Subagent block, State running;
    - `SubagentSpawned{Base{root,root}, Child:"k1", CallID:"c1"}` → `Sub.Child = "k1"`;
    - child events (`Base{k1, root}`): `ToolCallStarted(read)` → `Tools == 1`, `Current == "read"`; `ToolCallFinished` → `Current == ""`;
    - root `ToolCallFinished(c1)` → State ok;
    - every child event returns `["c1"]`.
  - `TestApply_NestedSubagentRoutesToOwner`: `SubagentSpawned{Base{k1, root}, Child:"k2"}`, then a `k2` tool event → routed to block `c1` (Tools counts the whole subtree).
  - `TestApply_DescendantToolCallsNeverCreateBlocks`: after a child `ToolCallStarted`, `len(Blocks())` is unchanged.
  - `TestApply_RootPermissionAwaitsToolBlock`:
    - `PermissionRequested{Base{root,root}, RequestID:"p1", Call:{ID:"t1"}}` after `ToolCallStarted(t1)` → block t1 has State awaiting and `Permission.RequestID == "p1"`; `Pending()` has one entry with `Block == "t1"`;
    - `PermissionResolved{p1}` → State back to running, `Permission == nil`, `Pending()` empty.
    - Note: the Plan 1 executor publishes `ToolCallStarted` before the hook asks, so the block exists. If a request arrives for an unknown call ID, create no block, but still list the request in `Pending()` with `Block == ""`.
  - `TestApply_SubagentPermissionMarksOwner`: `PermissionRequested{Base{k1, root}}` → block c1 has `Permission` set and `Permission.Subagent == "explore"`; resolving it clears the permission.
  - `TestLoad_ResumeWithTaskChild`: after Load, a later `SubagentSpawned` for a resumed task (same `CallID` as a loaded block) updates that block; it does not duplicate it.
  - `TestChangedFiles`:
    - a write to a new path → added;
    - a read then a write of path B → modified;
    - an edit → modified;
    - failed write/edit results are ignored;
    - order is first-seen; a path counts once (R16).
    - Works from both Load and Apply.
  - `TestLastBrowserURL`: root bash calls `agent-browser open localhost:3000`, then `agent-browser click @e2`, then `agent-browser goto https://x.dev` → `"https://x.dev"`. The subcommands `open`, `goto`, and `navigate` set the URL. A descendant's browser call does not.
- [ ] **Step 2: Run** `go test ./internal/ui/transcript/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Keep `owner map[core.SessionID]BlockID`, filled from `SubagentSpawned` (the parent is root: use `CallID`; the parent is a descendant: use `owner[parent]`). Parse bash commands with `strings.Fields`. `Current` holds the tool name only.
- [ ] **Step 4: Run** `go test ./internal/ui/transcript/ -race` and `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Add a row: "Session events/messages → display blocks | `transcript.New(root)`, `Load`, `Apply`".
- [ ] **Step 6: Commit.** `git commit -m "feat(ui/transcript): subagents, permissions, changed files, browser URL"`

### Task 9: SessionService additions and the title fix

**Model:** sonnet

**Files:**
- Modify: `internal/core/ports.go` (`SessionService`), `internal/service/session/{session.go,title.go}`, `internal/data/store/sessions.go` (`ListRootsByCwd`), `internal/data/store/todos.go` (unchanged API; used via a new interface)
- Test: `internal/service/session/ports_test.go`, `internal/data/store/sessions_test.go`

**Interfaces:**
- Produces:
  ```go
  package core
  type SessionService interface {
      List(ctx context.Context, limit int) ([]Session, error)
      ListForCwd(ctx context.Context, cwd string, limit int) ([]Session, error) // roots whose Cwd == pathid.Key(cwd), newest first
      Get(ctx context.Context, id SessionID) (Session, error)
      Messages(ctx context.Context, id SessionID) ([]Message, error)
      Todos(ctx context.Context, id SessionID) ([]Todo, error)
      Rename(ctx context.Context, id SessionID, title string) error          // trimmed; "" → error; ≤ 50 runes via capTitle
      Configure(ctx context.Context, id SessionID, agent, model string) error // "" leaves a field unchanged
  }   // Compact moves to ChatService in Task 10
  package store
  func (s *Store) ListRootsByCwd(ctx context.Context, cwd string, limit int) ([]core.Session, error)
  package session
  // Store gains ListRootsByCwd and ListTodos(ctx, id) ([]core.Todo, error).
  // Deps gains nothing: Bus already exists.
  ```
  `Configure` validates that `agent` (when set) names a known agent (via `Agents.Get`), and that `model` (when set) parses with `core.ParseModelRef`. The UI passes resolved refs. `Rename`, `Configure`, and the title save go through `modify`, and each publishes `event.SessionUpdated{Base{id, root}, Info}` after a successful write, where root = `id` (roots only; this is a root-session API). `Touch` does not publish.

  **Title fix (spec §8.1):** `GenerateTitle` saves only if, inside `modify`, `sess.Title == PlaceholderTitle(firstPrompt)`. Otherwise it returns nil and saves nothing.

- [ ] **Step 1: Write the failing tests** (real SQLite through `store.Open(t.TempDir()+"/db")`, as in Plan 1's session tests).
  - `TestListForCwd_FiltersRootsByCanonicalCwd`: two roots in `/w` (stored as `pathid.Key(dir)`), one in `/other`, one child of the first. `ListForCwd(ctx, dir, 10)` returns the two `/w` roots, newest first. Calling it with a symlink to `dir` returns the same.
  - `TestRename_TrimsCapsAndPublishes`: `Rename(" x ")` → title `"x"` and one `SessionUpdated`. `Rename("  ")` → error, and no event.
  - `TestConfigure_ValidatesAndPublishes`: unknown agent → error; `"bad"` model → error; `("plan","anthropic/m")` → saved and published; `("", "")` → no change, no event.
  - `TestGenerateTitle_DoesNotClobberRename`: create a session with a placeholder title and start `GenerateTitle` with an `llmtest` client that blocks (use the fake's gating, or a channel-backed `core.LLM` in the test). Call `Rename(id, "mine")` and then release the LLM. The final title is `"mine"`.
  - `TestModify_ConcurrentRenameAndConfigure`: 50 goroutines alternate `Rename` and `Configure`. At the end, the agent, model, and title each equal one of the written values, and none is empty (`-race`).
  - `TestTodos_ReturnsStoredList`.
- [ ] **Step 2: Run** `go test ./internal/service/session/ ./internal/data/store/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `ListRootsByCwd` is a `SELECT … WHERE parent_id = '' AND cwd = ? ORDER BY updated_at DESC, id DESC LIMIT ?`, with the `limit <= 0` rule unchanged.
  - Remove `Compact` from `core.SessionService` (the method stays on `session.Service`).
  - In `internal/app`, canonicalize `workDir` with `pathid.Key` in `resolveWorkDir`'s result (R17), and update `cli_test`/`app_test` expectations if they compare paths.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.**
  - Update the SessionService line.
  - Add an Invariant: "`GenerateTitle` never overwrites a non-placeholder title."
  - Add an Invariant: "Stored `Session.Cwd` is `pathid.Key(workDir)`."
- [ ] **Step 6: Commit.** `git commit -am "feat(session): ListForCwd, Todos, Rename, Configure; title saves only over the placeholder"`

### Task 10: `ChatService.Compact` with busy exclusion

**Model:** sonnet

**Files:**
- Modify: `internal/core/ports.go` (`ChatService.Compact`), `internal/service/agent/runner.go` (`Exclusive`), `internal/service/chat/chat.go` (+ `compact.go` if chat.go nears 300 lines)
- Test: `internal/service/agent/exclusive_test.go`, `internal/service/chat/compact_test.go`

**Interfaces:**
- Produces:
  ```go
  package core
  type ChatService interface {
      Send(ctx context.Context, req SendRequest) (SendResult, error)
      Compact(ctx context.Context, id SessionID) error   // core.ErrBusy while a run is in progress
      Cancel(id SessionID)
      Close(ctx context.Context) error
  }
  package agent
  // Exclusive runs fn while holding id's slot in the running map: Run on id
  // returns core.ErrBusy meanwhile, and Exclusive returns core.ErrBusy if a
  // run (or another Exclusive) holds it. Cancel(id) cancels fn's ctx.
  func (r *Runner) Exclusive(ctx context.Context, id core.SessionID, fn func(context.Context) error) error
  package chat
  // Runner gains Exclusive; Sessions gains Compact(ctx, id) error.
  ```
  A missing session is a `*ConfigError`; `session.ErrNothingToCompact` passes through unchanged.

- [ ] **Step 1: Write the failing tests.**
  - `TestCompact_BusyDuringRun`: a Run blocked in a gated LLM makes `chat.Compact` return an error `errors.Is(_, core.ErrBusy)`.
  - `TestRun_BusyDuringCompact`: an `Exclusive` whose fn blocks on a channel makes `Run` return `core.ErrBusy`. After the fn is released, `Run` succeeds.
  - `TestExclusive_CancelStopsFn`: `Cancel(id)` cancels fn's ctx.
  - `TestCompact_MissingSessionIsConfigError`.
  - Run all of these with `-race`.
- [ ] **Step 2: Run** `go test ./internal/service/agent/ ./internal/service/chat/`. Expected: FAIL.
- [ ] **Step 3: Implement** `Exclusive` on top of `register`. Wire `chat.Compact` → `Runner.Exclusive(ctx, id, func(c) error { return Sessions.Compact(c, id) })`.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Update the ports paragraph and the `ErrBusy` invariant: "the Runner's `running` map is the one owner of per-session busyness; compaction registers there via `Exclusive`".
- [ ] **Step 6: Commit.** `git commit -am "feat(chat): Compact behind Runner.Exclusive; core.ErrBusy"`

### Task 11: State dir, atomic files, blobs, prefs

**Model:** sonnet

**Files:**
- Modify: `internal/data/paths/paths.go` (`StateDir`)
- Create: `internal/data/atomicfile/atomicfile.go`, `internal/data/blobfs/blobfs.go`, `internal/data/prefsfs/prefsfs.go`, `internal/core/prefs.go`
- Test: `internal/data/paths/paths_test.go`, `internal/data/atomicfile/atomicfile_test.go`, `internal/data/blobfs/blobfs_test.go`, `internal/data/prefsfs/prefsfs_test.go`

**Interfaces:**
- Produces:
  ```go
  package paths   // Paths gains StateDir: $XDG_STATE_HOME/jig, default $HOME/.local/state/jig
  package atomicfile
  // Write creates path's parent (0700), writes data to a temp file in the same
  // dir, fsyncs, chmods to perm, and renames it over path.
  func Write(path string, data []byte, perm fs.FileMode) error
  package blobfs
  type Store struct{ /* dir */ }
  func New(dir string) *Store                        // dir = DataDir/blobs
  func (s *Store) Put(data []byte) (string, error)   // ref = hex sha256; <dir>/<ref[:2]>/<ref>, 0600; idempotent
  func (s *Store) Open(ref string) ([]byte, error)
  func (s *Store) Dir() string
  var ErrBadRef = errors.New("blobfs: invalid ref")    // ref must match ^[0-9a-f]{64}$
  package core
  type AgentBrowserCache struct{ Bin string; ModTime int64; SkillsDir string }
  type Prefs struct {
      Theme        string
      Sidebar      *bool                 // nil = automatic (by width)
      Recent       []string              // action IDs, most recent first, ≤ 5
      History      map[string][]string   // project path → prompts, newest last, ≤ 100
      AgentBrowser AgentBrowserCache
  }
  type PrefsService interface{ Get() Prefs; Save(Prefs) error }
  package prefsfs
  type Store struct{ /* path, mu, cur core.Prefs */ }
  func Open(path string) (*Store, error)   // missing file → zero Prefs; corrupt JSON → error naming the path
  func (s *Store) Get() core.Prefs          // deep copy
  func (s *Store) Save(p core.Prefs) error  // atomicfile.Write(path, json, 0600); updates cur
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestResolve_StateDir`: `XDG_STATE_HOME=/s` → `/s/jig`; unset → `$HOME/.local/state/jig`.
  - `TestWrite_ReplacesAtomicallyWithPerm`: writes the file with perm 0600 and leaves no temp files in the dir afterwards.
  - `TestBlobfs_PutOpenDedup`: `Put` of the same bytes twice returns the same ref and creates one file, at `dir/ab/abcd…`. `Open` returns the bytes.
  - `TestBlobfs_RejectsBadRefs`: `Open` of `"../../etc/passwd"`, `"ABC"` (63 chars), an uppercase-hex ref, or `"a/b"` → `ErrBadRef`, with no file-system access (use a nonexistent dir to prove it).
  - `TestPrefs_RoundTripAndCopy`: `Save`, then reopen → equal. Mutating the slice returned by `Get` doesn't change the next `Get`.
  - `TestPrefs_CorruptFileIsError`.
- [ ] **Step 2: Run** `go test ./internal/data/...`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Add map entries, and shared-code rows for `atomicfile.Write` and `blobfs`.
- [ ] **Step 6: Commit.** `git commit -m "feat(data): state dir, atomic writes, content-addressed blobs, prefs"`

### Task 12: Trust store and project hash

**Model:** sonnet

**Files:**
- Create: `internal/data/trustfs/trustfs.go`
- Test: `internal/data/trustfs/trustfs_test.go`

**Interfaces:**
- Produces:
  ```go
  package trustfs
  type Grant struct {
      Hash      string    `json:"hash"`
      GrantedAt time.Time `json:"grantedAt"`
  }
  type Store struct{ /* path */ }
  func New(path string) *Store                                   // DataDir/trust.json
  func (s *Store) Get(project string) (Grant, bool, error)       // missing file → (zero, false, nil)
  func (s *Store) Put(project string, g Grant) error             // read-modify-write + atomicfile.Write 0600
  // Hash is sha256 over files sorted by path, each contributing
  // path + "\n" + strconv.Itoa(len(content)) + "\n" + content. "" for no files.
  func Hash(files []string) (string, error)
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestHash_OrderIndependentAndContentSensitive`: the same files in a different order give the same hash. Changing one byte changes it. Renaming a file changes it. `Hash(nil) == ""`.
  - `TestStore_PutGet`: `Put` for two projects → `Get` returns each; the file mode is 0600.
  - `TestStore_CorruptFileIsError`.
- [ ] **Step 2: Run** `go test ./internal/data/trustfs/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(data/trustfs): trust grants and project config hash"`

### Task 13: Config layers — Global and Project returned separately; `Merge`

**Model:** sonnet

**Files:**
- Modify: `internal/data/config/{load.go,merge.go,dto.go}`, `internal/core/config.go`, `internal/app/env.go` (+ any other `loaded.Config` users)
- Test: `internal/data/config/load_test.go` (adapt), `internal/data/config/merge_test.go`

**Interfaces:**
- Produces:
  ```go
  package config
  type Loaded struct {
      Global       core.Config   // the global file only
      Project      core.Config   // every .jig/config.toml, root→leaf, closer wins
      GlobalFiles  []string      // files actually read
      ProjectFiles []string
  }
  func Load(p paths.Paths, workDir string, getenv func(string) string) (Loaded, error)
  // Merge overlays hi on lo with Plan 1's per-key rules: non-empty scalars win;
  // providers and agents field by field; permissions rule by rule (Default
  // replaced when set, Patterns merged by key); aliases and keybinds by key;
  // instructions and skills.paths appended and de-duplicated; AgentBrowser
  // wins when set.
  func Merge(lo, hi core.Config) core.Config
  package core
  type Toggle string          // "" (unset), "auto", "true", "false"
  func (t *Toggle) UnmarshalTOML(v any) error   // accepts bool or "auto"/"true"/"false"
  // Config gains AgentBrowser Toggle  (TOML: [integrations.agent_browser] enabled = ...)
  // ProviderConfig gains ImageModels []string   (TOML: image_models) (R10)
  ```
  `Loaded.Config`, `GlobalAgents`, and `ProjectAgents` are removed. Callers use `config.Merge(l.Global, l.Project)` (trusted behavior; Task 15 inserts trust). `agents.Sources.GlobalTOML` = `l.Global.Agents`; `ProjectTOML` = `l.Project.Agents`.

- [ ] **Step 1: Write the failing tests.**
  - Change every existing Load test that asserted on `l.Config` to assert on `Merge(l.Global, l.Project)`. These tests are the regression suite proving Plan 1 behavior is unchanged.
  - `TestLoad_LayersSeparated`: the global file sets `permissions.bash = "ask"`, and a project file sets `permissions.bash = "allow"` → `l.Global.Permissions["bash"].Default == Ask` and `l.Project.Permissions["bash"].Default == Allow`. `ProjectFiles` lists the project file.
  - `TestMerge_Table`: one case per per-key rule above, including "an empty hi scalar keeps lo" and "`CanSpawn` nil in hi keeps lo".
  - `TestToggle_UnmarshalTOML`: `true`, `false`, `"auto"` → ok; `"maybe"` → error naming the value.
  - `TestLoad_ImageModelsAndIntegrations`: the DTO fields reach `core.Config`.
- [ ] **Step 2: Run** `go test ./internal/data/config/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Keep the per-file fold (`state.apply`) but run it into two accumulators (global and project). Build `Merge` from the same field helpers so the two cannot drift. In `env`, store `loaded`, and add `func (e env) cfg() core.Config` returning a merged config computed once in `loadEnv`.
- [ ] **Step 4: Run** `make check`. Expected: PASS (the e2e tests too; behavior is unchanged).
- [ ] **Step 5: Commit.** `git commit -am "refactor(config): return global and project layers separately; Merge"`

### Task 14: Tighten-only merge and trust effects

**Model:** opus (security)

**Files:**
- Create: `internal/service/permission/tighten.go`, `internal/service/trust/{trust.go,effects.go}`
- Modify: `internal/service/agents/builtin.go` (export `Builtin`)
- Test: `internal/service/permission/tighten_test.go`, `internal/service/trust/{trust_test.go,effects_test.go}`

**Interfaces:**
- Produces:
  ```go
  package permission
  // Overlay applies hi over lo per tool (overlayRule semantics).
  func Overlay(lo, hi core.PermissionRules) core.PermissionRules
  // Tighten splits add into the entries that can be overlaid on baseline
  // (already Effective, Defaults included) without loosening any decision,
  // and the entries it drops (R7).
  func Tighten(baseline, add core.PermissionRules) (kept, dropped core.PermissionRules)

  package agents
  func Builtin(name string) (core.Agent, bool)

  package trust
  type Layers struct {
      Global, Project     core.Config
      GlobalMD, ProjectMD map[string]core.AgentConfig   // agentfs results
  }
  type Effect struct{ Key, Value, Source string }        // Source: declaring file, "" for config.toml keys
  func (e Effect) String() string
  // Effects lists everything the project layer changes, sorted by Key then Value.
  func Effects(l Layers) []Effect
  // Restrict returns l with Project and ProjectMD reduced to what an
  // untrusted project may apply (spec §8.3 + R7), and the effects dropped.
  func Restrict(l Layers) (Layers, []Effect)
  ```
  **Restrict rules:**
  - **Dropped whole:** `Providers` (including `ImageModels`), `Instructions`, `SkillPaths`, and `AgentBrowser`. That last one is an addition to the spec's list: enabling the integration turns on the preset's allow patterns.
  - **Kept:** `DefaultModel`, `SmallModel`, `ModelAliases`, `Theme`, and `Keybinds`.
  - **Top-level permissions:** `baseline = Effective(nil, Global.Permissions)`.
  - **Agent permissions**, for each project agent name `n` (TOML, then MD):
    - `agentGlobal = Overlay(Overlay(Builtin(n).Permissions, Global.Agents[n].Permissions), GlobalMD[n].Permissions)`
    - `baseline = Effective(agentGlobal, mergedCfg)`, where `mergedCfg = Overlay(Global.Permissions, keptTopLevel)`.
    - The MD layer's baseline also includes the kept TOML entries for `n`.
  - Non-permission agent fields are kept.

  **Effect strings (`String()`):**
  - `permissions.bash → allow`
  - `permissions.bash "git push*" → deny`
  - `providers.anthropic.base_url → https://x`
  - `providers.anthropic.api_key → (set)`, and `options → (set)`; values are never printed
  - `instructions += docs/rules.md`
  - `skills.paths += …`
  - `default_model → …`, `model_aliases.fast → …`
  - `integrations.agent_browser.enabled → true`
  - `agents.reviewer (from /p/.claude/agents/reviewer.md) permissions.edit → allow`
  - `agents.reviewer (from …) prompt`: prompt and description are named without their value.

- [ ] **Step 1: Write the failing `Tighten` tests.**
  - `TestTighten_DefaultOnlyIfAtLeastAsRestrictive`: baseline bash default ask. Add `Default: allow` → dropped; `deny` → kept; `ask` → kept.
  - `TestTighten_AllowPatternNeverApplied`: baseline `{"*": allow}`, add `{"gi* push --force": allow}` → dropped (the R7 counterexample). Also an allow pattern equal to the baseline's verdict is still dropped.
  - `TestTighten_AskPatternDroppedWhenBaselineHasDeny`: baseline `{"*": allow, "git push*": deny}`, add `{"gi* push --force": ask}` → dropped; the same add over a baseline with no deny → kept.
  - `TestTighten_DenyPatternAlwaysKept`.
  - `TestTighten_ToolAbsentFromBaselineUsesDefaults`: `read` (Defaults allow) + add read `Default: ask` → kept.
  - `TestTighten_NeverLooser`: a property check over a small table of baselines × adds × subjects. For each subject `s`, `actionRank(Evaluate(Overlay(baseline, kept)[t], s)) >= actionRank(Evaluate(baseline[t], s))`. Use `actionRank` from the package (white-box).
- [ ] **Step 2: Write the failing `trust` tests.**
  - `TestRestrict_DropsProvidersInstructionsSkillsIntegration`, asserting both the reduced layer and the dropped effects.
  - `TestRestrict_KeepsAliasesAndModels`.
  - `TestRestrict_ProjectAgentCannotLoosenBuiltin`: a project MD agent `plan` with `permissions.write = allow` → dropped (the builtin plan asks). A project agent `plan` with `bash = deny` → kept.
  - `TestRestrict_NewProjectAgentBoundedByConfig`: a new agent `rev` with `edit = allow`, where Global has no edit rule (Defaults ask) → dropped.
  - `TestRestrict_TOMLThenMDLayering`: TOML `bash deny`, MD `bash ask` → the MD ask is dropped (its baseline now contains the deny).
  - `TestEffects_NeverPrintsSecrets`: `api_key = "sk-secret"` → no effect string contains `"sk-secret"`.
  - `TestEffects_SortedAndComplete`: one project config touching every kind of key → the exact expected `[]string`.
- [ ] **Step 3: Run** `go test ./internal/service/permission/ ./internal/service/trust/`. Expected: FAIL.
- [ ] **Step 4: Implement.** `trust` imports `service/permission` and `service/agents` (service→service is allowed), and `core` only.
- [ ] **Step 5: Run** `go test -race ./internal/service/...` and `make check`. Expected: PASS.
- [ ] **Step 6: `AGENTS.md`.** Add an Invariant: "An untrusted project layer goes through `trust.Restrict` before any merge; `permission.Tighten` never keeps an `allow` pattern, and keeps an `ask` pattern only where the baseline has no `deny`." Add `service/trust` to the map.
- [ ] **Step 7: Commit.** `git commit -m "feat(trust): tighten-only restriction of untrusted project config; effects"`

### Task 15: Trust in `internal/app`; `--trust-project`; headless e2e

**Model:** opus (wiring + security)

**Files:**
- Create: `internal/app/trust.go`
- Modify: `internal/app/{env.go,data.go,cli.go,headless.go,services.go}`, `internal/data/agentfs/agentfs.go` (only if a helper is needed to list files; `AgentConfig.Source` already holds the path)
- Test: `internal/app/trust_test.go`, `e2e/trust_test.go`

**Interfaces:**
- Produces:
  ```go
  package app
  type trustState struct {
      project string          // pathid.Key(gitRoot or workDir)
      hash    string          // trustfs.Hash(project config files + project agent files); "" = no project config
      trusted bool
      effects []trust.Effect  // everything the project layer would change
      dropped []trust.Effect  // what Restrict removed (empty when trusted)
  }
  // trustDecider decides for an untrusted, non-empty project. Headless: the flag. TUI (Plan 2b): the dialog.
  type trustDecider func(st trustState) (grant bool, err error)
  func loadEnv(cwd string, getenv func(string) string, decide trustDecider) (env, error)
  ```
  `env` gains `trust trustState`, `layers trust.Layers` (post-decision), and `merged core.Config`. `e.cfg()` returns `merged`.

  **Flow in `loadEnv`:**
  1. Load the config and agent layers.
  2. Hash the project files.
  3. `trustfs.Get(project)`.
  4. If the stored hash ≠ current and hash ≠ "", call `decide`. On grant, `Put{hash, clk.Now()}`.
  5. If still untrusted, run `Restrict`.
  6. `merged = config.Merge(Global, Project')`; the agent Sources come from `layers`.

  **Headless:** `--trust-project` → a decider returning true. Without the flag → false. When the result is untrusted and `len(dropped) > 0`, print exactly `warning: project config not trusted; loosening settings ignored (use --trust-project)` to stderr once (R9).

- [ ] **Step 1: Write the failing unit tests** (`internal/app`, temp XDG and project tree).
  - `TestLoadEnv_NoProjectConfigNeverAsks`: the decider is not called; `trusted == false`; `hash == ""`.
  - `TestLoadEnv_UntrustedRestricts`: a project `permissions.write = "allow"` with a decider returning false → `e.cfg().Permissions["write"]` has no Allow default, and `dropped` is non-empty.
  - `TestLoadEnv_GrantPersistsUntilConfigChanges`: the decider returns true once. The second load doesn't call it (trusted). Editing the project config makes the third load call it again.
  - `TestLoadEnv_ProjectAgentFileIsHashed`: changing `.claude/agents/x.md` changes the hash.
- [ ] **Step 2: Write the failing e2e tests** (`e2e/trust_test.go`, using `newEnv`, `writeScript`, `jigtestConfig`, `runPrompt`).
  - `TestE2E_UntrustedProjectCannotLoosen`:
    - `work/.jig/config.toml` = `[permissions]\nwrite = "allow"`;
    - script m1: call `write` `{"path":"x.txt","content":"hi"}`, then text `"done"`;
    - run without `--yes`;
    - expect: exit 0, stderr contains the exact warning, `work/x.txt` does not exist.
  - `TestE2E_TrustProjectFlag`:
    - the same setup with `--trust-project` → `x.txt` exists, and no warning;
    - a second run without the flag (fresh script writing `y.txt`) → `y.txt` exists;
    - append a comment line to the project config, then a third run → the warning is back and `z.txt` is not written.
- [ ] **Step 3: Run** `go test ./internal/app/` and `make test`. Expected: FAIL.
- [ ] **Step 4: Implement.** Keep every `internal/app` func ≤ 40 lines: split into `projectFiles`, `readTrust`, `decideTrust`, and `applyTrust`. Add `--trust-project` to `runOpts`, `parseRun`, `runUsage`, and `usage`.
- [ ] **Step 5: Run** `make check`. Expected: PASS.
- [ ] **Step 6: `AGENTS.md`.** Add an Invariant: "Trust is decided once in `loadEnv`, before any service is built; `e.cfg()` is already the trusted or restricted merge."
- [ ] **Step 7: Commit.** `git commit -am "feat(app): project-config trust; --trust-project; untrusted warning"` (add the new files).

### Task 16: Image pipeline (`service/media`)

**Model:** opus (image pipeline)

**Files:**
- Create: `internal/service/media/{media.go,encode.go}`
- Test: `internal/service/media/media_test.go` (fixtures generated in-test with `image/png`, `image/jpeg`, `image/gif`; a tiny webp as a byte-slice literal in `testdata/1x1.webp`)

**Interfaces:**
- Produces:
  ```go
  package media
  const (MaxEdge = 1568; JPEGOver = 1 << 20; MaxBytes = 5 << 20; MaxPixels = 50_000_000; MaxInput = 20 << 20)
  var (ErrUnsupported = errors.New("unsupported image format"); ErrTooLarge = errors.New("image too large"))
  type BlobStore interface{ Put(data []byte) (string, error) }
  type Pipeline struct{ /* blobs */ }
  func New(blobs BlobStore) *Pipeline
  // Process decodes png/jpeg/gif(first frame)/webp, scales so the long edge is
  // ≤ MaxEdge (x/image/draw CatmullRom), re-encodes as PNG, or as JPEG q85
  // when that PNG exceeds JPEGOver and the image is fully opaque, refuses
  // results > MaxBytes, stores the bytes, and returns the media and the
  // post-scaling size.
  func (p *Pipeline) Process(data []byte) (core.Media, core.ImageInfo, error)
  func IsImagePath(path string) bool   // .png .jpg .jpeg .gif .webp, case-insensitive
  ```
  Order of checks:
  1. `len(data) > MaxInput` → `ErrTooLarge`.
  2. Sniff the format: a `RIFF….WEBP` header → `webp.DecodeConfig`/`webp.Decode`, since webp doesn't self-register. Otherwise `image.DecodeConfig`.
  3. `w*h > MaxPixels` → `ErrTooLarge`, before the full decode.
  4. Decode, scale, encode.
  5. `len(out) > MaxBytes` → `ErrTooLarge`.
  6. `Put`.

- [ ] **Step 1: Write the failing tests** (a fake `BlobStore` recording puts).
  - `TestProcess_ScalesLongEdge`: 3000×1000 PNG → `ImageInfo{1568, 523, n}`; the MIME is `image/png`; the stored bytes decode to 1568×523.
  - `TestProcess_SmallImageUnscaled`: 10×10 → 10×10.
  - `TestProcess_OpaquePhotoBecomesJPEG`: a 1500×1500 opaque RGB noise image (deterministic `math/rand/v2`, `rand.NewPCG(1, 2)`) → `image/jpeg`. Its PNG is about 6.7 MB, so it goes over `JPEGOver`, and the image is not scaled.
  - `TestProcess_TransparentPhotoStaysPNG`: the same image with pixel (0,0) at alpha 0 stays PNG, which is over 5 MiB → `ErrTooLarge`.
  - `TestProcess_Webp`: the 1×1 webp fixture → png 1×1.
  - `TestProcess_RefusesHugeDimensions`: a PNG header claiming 100000×100000 (build one with `png.Encode` of a 1×1 image, then patch the IHDR width/height and recompute the CRC in-test) → `ErrTooLarge` without a full decode.
  - `TestProcess_CorruptImageIsError`: truncated PNG bytes → error; `"not an image"` → `ErrUnsupported`.
  - `TestProcess_TooLargeAfterEncode`: `MaxBytes` is a const, so exercise the check through an unexported `process(data, limit int)` that `Process` wraps. With `limit = 100`, a 50×50 noise PNG → `ErrTooLarge`.
  - `TestIsImagePath`.
- [ ] **Step 2: Run** `go test ./internal/service/media/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Add `golang.org/x/image@v0.46.0`.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(media): image decode/scale/re-encode pipeline into blobs"`

### Task 17: Image `read`; the read-note budget fix

**Model:** sonnet

**Files:**
- Modify: `internal/service/tools/read.go`, `internal/app/registry.go` (+ `registryDeps.blobs`, `registryDeps.media`), `internal/app/services.go` (build `blobfs.New(DataDir/blobs)` and `media.New`)
- Test: `internal/service/tools/read_image_test.go`, `internal/service/tools/read_test.go`

**Interfaces:**
- Produces:
  ```go
  package tools
  type Imager interface{ Process(data []byte) (core.Media, core.ImageInfo, error) }
  func NewRead(fs FS, tr *Tracker, img Imager) ext.Tool   // img nil → images are read as binary (refused), as today
  ```
  Reading an image path:
  - `media.IsImagePath`-equivalent extension check, implemented locally in `tools` (don't import `media`; keep the dependency through `Imager`);
  - then `fs.Stat`;
  - a size over 20 MiB → error result `image too large`;
  - `ReadFile` → `Process`;
  - result `core.ToolOK(call, fmt.Sprintf("image %dx%d (%s)", w, h, humanBytes(n)))` with `Media: []core.Media{m}`.
  - `humanBytes`: `< 1024` → `"%d B"`, `< 1 MiB` → `"%d KB"` (n/1024), else `"%.1f MB"`.
  - A process error becomes an error result naming the cause.

  **Read-note budget (spec §8.1):** the line budget becomes `maxReadBytes - readNoteReserve`, where `readNoteReserve = 96`, so the `(output truncated at 50 KB; continue with offset N)` note always survives the executor's 50 KB cap.

- [ ] **Step 1: Write the failing tests.**
  - `TestRead_ImageReturnsMedia`: write a 2000×1000 PNG to a temp dir → output `"image 1568x784 (… KB)"` (assert the prefix `image 1568x784 (`), `len(Media) == 1`, MIME `image/png`, and the fake blob store got one put.
  - `TestRead_ImageTooLarge`: stat size > 20 MiB (use a fake `FS` returning a big `Size()`) → `IsError`, containing `"image too large"`.
  - `TestRead_CorruptImageIsErrorResult`.
  - `TestRead_TruncationNoteSurvivesExecutorCap`: a file of 3000 lines × 40 bytes → the output ends with `continue with offset N)`, and `len(output) <= 50*1024 - len("\n[output truncated at 50 KB]")`.
- [ ] **Step 2: Run** `go test ./internal/service/tools/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Wire `blobfs` + `media` in `internal/app` so `jig run` reads images.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Add a row: "Turn image bytes into a stored `core.Media` | `media.New(blobs).Process(data)`".
- [ ] **Step 6: Commit.** `git commit -am "feat(read): images through the media pipeline; budget the truncation note"`

### Task 18: Media reaches the model — catalog flag, blob loading, converter

**Model:** opus (client/llm conversion)

**Files:**
- Modify: `internal/client/catalog/convert.go` (+ custom providers), `internal/client/llm/{source.go,convert.go}`, `internal/client/llm/jigtest/jigtest.go` (`Turn` fields), `internal/app/services.go` (pass blobs to `NewSource`), `e2e/harness_test.go` (`jigtestConfig` gains `image_models = ["m1"]`)
- Create: `internal/client/llm/media.go`
- Test: `internal/client/catalog/convert_test.go`, `internal/client/llm/media_test.go`, `internal/client/llm/convert_test.go`, `e2e/image_test.go`

**Interfaces:**
- Produces:
  ```go
  package llm
  type BlobReader interface{ Open(ref string) ([]byte, error) }
  func NewSource(cat Catalog, reg ext.View, providers map[string]core.ProviderConfig, getenv func(string) string, blobs BlobReader) *Source
  // For wraps the provider's LLM so each request's media is resolved first:
  // SupportsImages → Media.Data loaded from blobs (a failed load drops that
  // medium and appends "\n[image unavailable: <err>]"); otherwise media
  // are dropped and "\n[image omitted: <provider/model> does not accept
  // images]" is appended to the tool result's Output (for attachments, it
  // replaces Content). Stored messages are never mutated: parts are copied.
  func (s *Source) For(ref core.ModelRef) (core.LLM, core.ModelInfo, error)
  package jigtest
  // Turn gains:
  //   ExpectMedia          *int     // exact count of media parts (tool-result media + file parts) in the prompt
  //   ExpectPromptContains []string // substrings of the concatenated prompt text (text parts + tool-result text)
  ```
  Converter (`toolResultPart`): a non-error result whose `Media[0].Data != nil` → `fantasy.ToolResultOutputContentMedia{Data: base64.StdEncoding.EncodeToString(data), MediaType: MIME, Text: Output}`. Additional media are dropped with `" [+N more images omitted]"` appended to Text. An `IsError` result ignores media. Catalog: `convertModel` copies `m.SupportsImages`. A custom provider's model gets `SupportsImages = slices.Contains(cfg.ImageModels, id)` (R10).

- [ ] **Step 1: Write the failing tests.**
  - `TestConvertModel_SupportsImages`.
  - `TestCustomProvider_ImageModels`.
  - `TestMediaLLM_LoadsDataWhenSupported`: a fake inner LLM records the request → `Media[0].Data` equals the blob bytes, and the caller's original message has `Data == nil` afterwards (no aliasing).
  - `TestMediaLLM_PlaceholderWhenUnsupported`: the Output ends with `"\n[image omitted: p/m does not accept images]"`, and Media is empty.
  - `TestMediaLLM_BlobErrorPlaceholder`.
  - `TestToFantasy_ToolResultMedia`: the base64 matches; `Text == Output`.
  - `TestToFantasy_ErrorResultIgnoresMedia`.
  - jigtest: `TestStream_ExpectMediaMismatchFails`.
- [ ] **Step 2: Write the failing e2e test** `TestE2E_ReadImageReachesModel`: write `work/shot.png` (a 20×10 PNG).
  - m1 turn 1: call `read {"path":"shot.png"}`. Turn 2: `ExpectMedia: 1`, text `"saw it"` → stdout contains `saw it`.
  - Subtest with `--model jigtest/m2` (m2 is not in `image_models`): turn 2 `ExpectMedia: 0`, `ExpectPromptContains: ["[image omitted: jigtest/m2 does not accept images]"]`.
- [ ] **Step 3: Run** `go test ./internal/client/...` and `make test`. Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run** `make check`. Expected: PASS.
- [ ] **Step 6: `AGENTS.md`.** Add an Invariant: "Media bytes are loaded only in `client/llm`'s `For` wrapper, per request; unsupported models get the `[image omitted: …]` text instead."
- [ ] **Step 7: Commit.** `git commit -am "feat(llm): send tool-result images to image-capable models; placeholder otherwise"`

### Task 19: Attachments (`SendRequest.Attachments`, `--attach`)

**Model:** sonnet

**Files:**
- Modify: `internal/core/ports.go`, `internal/service/chat/{chat.go,attach.go}`, `internal/service/agent/runner.go` (+ `proxy.go`, chat's and task's `Runner` interfaces), `internal/client/llm/convert.go` (`convertUserMessage`), `internal/app/{cli.go,headless.go,services.go}`
- Test: `internal/service/chat/attach_test.go`, `internal/service/agent/runner_test.go`, `internal/client/llm/convert_test.go`, `e2e/image_test.go`

**Interfaces:**
- Produces:
  ```go
  package core   // SendRequest gains Attachments []string (paths; relative ones resolve against WorkDir)
  package chat
  type FileReader interface{ Stat(string) (fs.FileInfo, error); ReadFile(string) ([]byte, error) }
  type ReadMarker interface{ MarkRead(sid core.SessionID, path string, info fs.FileInfo) }
  type Imager interface{ Process(data []byte) (core.Media, core.ImageInfo, error) }
  // Deps gains Files FileReader, Reads ReadMarker, Images Imager (tools.OSFS(), the shared *tools.Tracker, the media pipeline).
  package agent
  func (r *Runner) Run(ctx context.Context, rc ext.RunContext, userText string, atts ...core.Attachment) (core.Message, error) // R12
  ```
  **Validation in `prepare`** (each failure is a `*ConfigError` raised before the session is created):
  - missing → `attachment <p>: no such file`;
  - a directory → `attachment <p> is a directory`;
  - an image extension → ≤ 20 MiB → `Images.Process`;
  - a non-image file: > 50 KB → `attachment <p> is larger than 50 KB`; a NUL byte in the first 8 KB → `attachment <p> is binary`.

  `MarkRead` runs after `commit` for text attachments. The user message's parts are the text part, then one `PartAttachment` per attachment, in request order. Converter: a text attachment → `fantasy.TextPart{Text: "<attachment path=\"<p>\">\n<content>\n</attachment>"}`; an image with `Data` → `fantasy.FilePart{Filename: base(p), Data, MediaType}`; an image without Data can't reach the converter (Task 18's wrapper replaced it with text). CLI: `--attach PATH`, repeatable, via `flag.Func` (R11).

- [ ] **Step 1: Write the failing tests.**
  - `TestSend_TextAttachmentStoredAndMarkedRead`: the user message has a PartAttachment with Content, and after Send an `edit` on that file needs no prior read (assert via the tracker's `CheckWritable` returning nil).
  - `TestSend_AttachmentErrorsAreConfigErrors`: a table of the four errors above, each creating no session (`f.roots()` is empty).
  - `TestSend_ImageAttachmentUsesPipeline`.
  - `TestRunner_AttachmentsOnUserMessage`.
  - `TestToFantasy_UserAttachments`.
- [ ] **Step 2: Write the failing e2e tests.**
  - `TestE2E_ImageAttachment`: `jig run --attach shot.png --attach notes.txt "look"`, with m1 turn 1 `ExpectMedia: 1` and `ExpectPromptContains: ["<attachment path=", "notes content"]`.
  - `TestE2E_BinaryAttachmentExits2`: a file with NUL bytes → exit 2 and stderr names the file.
- [ ] **Step 3: Run** `go test ./internal/...` and `make test`. Expected: FAIL.
- [ ] **Step 4: Implement.** Share the NUL sniff with `read`'s `looksBinary` by moving it into `tools` as the exported `LooksBinary(b []byte) bool`, used by chat (service→service).
- [ ] **Step 5: Run** `make check`. Expected: PASS.
- [ ] **Step 6: `AGENTS.md`.** Add a row: "Is this file binary? | `tools.LooksBinary(b)`"; update the ChatService description.
- [ ] **Step 7: Commit.** `git commit -am "feat(chat): text and image attachments; jig run --attach"`

### Task 20: agent-browser — detection, skills, permission preset

**Model:** sonnet

**Files:**
- Create: `internal/client/agentbrowser/agentbrowser.go`, `internal/app/agentbrowser.go`
- Modify: `internal/service/permission/rules.go` (`AgentBrowserPreset`), `internal/app/{env.go,data.go,services.go}` (prefs opened from `StateDir/prefs.json`)
- Test: `internal/client/agentbrowser/agentbrowser_test.go`, `internal/service/permission/preset_test.go`, `internal/app/agentbrowser_test.go`, `e2e/agentbrowser_test.go`

**Interfaces:**
- Produces:
  ```go
  package agentbrowser
  const SkillsTimeout = 3 * time.Second
  func Detect(lookPath func(string) (string, error)) (bin string, ok bool)
  func SkillsPath(ctx context.Context, bin string) (string, error)   // runs `<bin> skills path` under SkillsTimeout; trimmed stdout
  package permission
  // AgentBrowserPreset: bash patterns (allow) "agent-browser snapshot*",
  // "agent-browser screenshot*", "agent-browser console*",
  // "agent-browser errors*", "agent-browser get *", "agent-browser is *",
  // "agent-browser tab", "agent-browser a11y*", "agent-browser vitals*",
  // "agent-browser read*"; (ask) "agent-browser *". No Default.
  func AgentBrowserPreset() core.PermissionRules
  package app
  type browserIntegration struct{ enabled bool; bin, skillsDir string }
  ```
  **Flow** (inside `loadEnv`, after the trust decision, before `Restrict`):
  1. The toggle is `Project.AgentBrowser` if trusted and set, else `Global.AgentBrowser`.
  2. `"false"` → off. `"true"` without the binary → warning `warning: integrations.agent_browser.enabled = true but agent-browser is not on PATH`. `""` or `"auto"` → on iff detected.
  3. When on: `Global = config.Merge(core.Config{Permissions: AgentBrowserPreset()}, Global)`, so the preset sits below the user config.
  4. The skills dir comes from prefs when `Prefs.AgentBrowser.{Bin,ModTime}` matches `os.Stat(bin)`; otherwise from `SkillsPath`, normalized per R14 and saved to prefs.
  5. A `SkillsPath` error → warning `warning: agent-browser skills path: <err>`; the integration stays on without skills.
  6. The skills dir is prepended to `skillfs.Dirs(…)` as the lowest precedence.

- [ ] **Step 1: Write the failing tests.**
  - `TestSkillsPath_Timeout`: a fake script `sleep 10` in `t.TempDir()`, run with a parent ctx whose deadline is already set by the test via `context.WithTimeout(ctx, 50*time.Millisecond)` → an error. (The 3 s cap is a const inside `SkillsPath`, applied with `context.WithTimeout`, so the test's shorter deadline wins.)
  - `TestSkillsPath_TrimsOutput`.
  - `TestPreset_Decisions`: with `Effective(nil, Merge(preset, empty))`, `agent-browser snapshot -i` → allow; `agent-browser click @e1` → ask; `agent-browser snapshot; rm -rf x` → ask (the metachar downgrade, via `Hook.decideOne` in a white-box test).
  - `TestBrowser_ToggleAndCache` (app): `"false"` → off with no exec. A cached prefs entry means no `SkillsPath` call (inject `skillsPath func` into the helper for the test). A missing binary with `"true"` → a warning.
- [ ] **Step 2: Write the failing e2e test** `TestE2E_AgentBrowserPresetAndSkill`:
  - A fake `agent-browser` shell script in `root/bin`, on `PATH`, answers:
    - `skills path` → prints `root/abskills` (containing `agent-browser/SKILL.md` with `name: agent-browser`, `description: drive a browser`) and appends a line to `root/calls.log`;
    - `snapshot` → `- button "Go" [ref=e1]`;
    - anything else → `ran $*`.
  - m1 turn 1: `ExpectSystemContains: ["agent-browser"]`; calls bash `agent-browser snapshot -i` and bash `agent-browser click @e1`.
  - m1 turn 2: `ExpectPromptContains: ["[ref=e1]", "permission required"]`; text `done`.
  - Run twice without `--yes` (the second with a fresh one-turn script) → `calls.log` has exactly 1 line (cached).
- [ ] **Step 3: Run** the tests. Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run** `make check`. Expected: PASS.
- [ ] **Step 6: `AGENTS.md`.** Add an Invariant: "The agent-browser preset is the lowest config layer, and only when the integration is enabled; an untrusted project cannot enable it."
- [ ] **Step 7: Commit.** `git commit -am "feat(agent-browser): detection, bundled skills, permission preset"`

### Task 21: agent-browser screenshots attach to the bash result

**Model:** opus (path security)

**Files:**
- Create: `internal/service/tools/screenshot.go`
- Modify: `internal/service/tools/bash.go` (`NewBash` gains `shots *Screenshots`), `internal/app/registry.go`
- Test: `internal/service/tools/screenshot_test.go`, `internal/service/tools/bash_test.go`

**Interfaces:**
- Produces:
  ```go
  package tools
  type Screenshots struct{ /* img Imager; fs FS; tempDir string */ }
  func NewScreenshots(img Imager, fs FS, tempDir string) *Screenshots
  // Attach returns the image named by an `agent-browser screenshot` run's
  // output, as Media, or nil. The command must start (after TrimSpace) with
  // "agent-browser screenshot". The path is the last match of
  // `(\S+\.(?i:png|jpe?g|webp))` in output, trimmed of quotes and trailing
  // `.,;:)`; relative paths resolve against rc.WorkDir. Allowed iff
  // pathid.Key(path) is under pathid.Key(rc.WorkDir), or under
  // pathid.Key(tempDir) with a base name starting "screenshot" (R13). Size ≤ 20 MiB.
  func (s *Screenshots) Attach(rc ext.RunContext, command, output string) []core.Media
  func NewBash(sh Shell, tempDir string, ids IDSource, shots *Screenshots) ext.Tool  // shots nil = off
  ```
  bash calls `Attach` only for exit code 0 and not timed out, and sets `res.Media`. A failure inside `Attach` is silent: the bash output already names the file. `internal/app` passes `NewScreenshots(media, OSFS(), os.TempDir())` only when the integration is enabled (Task 20).

- [ ] **Step 1: Write the failing tests** (`tempDir` is a `t.TempDir()`, not the real one).
  - `TestScreenshots_AttachesWorkdirAndTempShots`: `Screenshot saved to <tmp>/screenshot-1.png` → 1 media. `agent-browser screenshot shots/page.png`, with output naming `shots/page.png` → 1 media.
  - `TestScreenshots_RefusesEscapes`, a table:
    - an absolute path outside both roots;
    - `../outside.png`;
    - a workdir symlink `link.png → <outside>/x.png`;
    - `<tmp>/other.png` (not named `screenshot*`);
    - `<tmp>/sub/../../x/screenshot.png` escaping `tmp`;
    - a directory named `x.png`;
    - a > 20 MiB file (fake FS size).

    Each → nil.
  - `TestScreenshots_OnlyForScreenshotCommand`: `agent-browser snapshot` whose output names a png → nil; `echo agent-browser screenshot` → nil.
  - `TestBash_ScreenshotMediaOnSuccessOnly`: a fake Shell exiting 1 → no Media; `shots == nil` → no Media.
- [ ] **Step 2: Run** `go test ./internal/service/tools/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Add an Invariant: "bash attaches a screenshot only from the workdir or `screenshot*` files in the OS temp dir, after resolving symlinks."
- [ ] **Step 6: Commit.** `git commit -am "feat(bash): attach agent-browser screenshots as media"`

## Finishing Plan 2a

- [ ] Run `make check` locally. Then `git push origin main`, and confirm both the ubuntu and macOS CI jobs pass (`gh run watch --exit-status`). Fix and push until both are green.
- [ ] Run the final whole-branch review (opus). Point the reviewer at the Review Focus list, and ask them to probe these failure modes on purpose:
  - `Tighten`/`Restrict` with adversarial patterns;
  - blob refs and screenshot paths with symlinks and `..`;
  - image bombs;
  - escape sequences through `Sanitize`;
  - `RootID` on every event path, including cancellation.
- [ ] Use `superpowers:finishing-a-development-branch`, then start Plan 2b.

