# jig Plan 2b — The TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `jig`, the interactive vim-style TUI, on top of Plan 2a. It covers:
- the block transcript with a details split;
- INSERT, NORMAL, and PICKER modes;
- the `ctrl+p` picker with drill-down, and the `@` file picker;
- the prompt, with history, `$EDITOR`, paste chips, and queued sends;
- permission cards, the status bar, the sidebar, and themes;
- inline images;
- the trust dialog;
- the browser-aware tool lines and sidebar.

It ends with a pty smoke test driving the real binary.

**Architecture:** Widgets live in `internal/bubbles/<widget>` and follow the slk#236 conventions (spec §3.2):
- Each widget owns its `Styles`/`DefaultStyles()`, its `KeyMap`/`DefaultKeyMap()`, and its size (`SetSize`).
- `View()` is pure.
- Required dependencies are named func types returning `tea.Cmd`, and widgets do no I/O.

`internal/ui` holds the jig-specific parts:
- `theme`: palettes → widget `Styles`;
- `actions`: the action catalogue and keymap;
- the `App`, which bridges bus events to `tea.Msg`, arbitrates modes, reconciles widgets, and owns the palette.

`internal/ui` reaches services only through `core` ports. `internal/app` builds the ports, the trust dialog, and the program.

**Tech Stack:** New in 2b:
- `charm.land/bubbletea/v2` v2.0.10
- `charm.land/bubbles/v2` v2.2.1
- `charm.land/glamour/v2` v2.0.1
- `github.com/alecthomas/chroma/v2` v2.27.0
- `github.com/sahilm/fuzzy` v0.1.3
- `github.com/aymanbagabas/go-udiff` v0.4.1
- `github.com/creack/pty` v1.1.24 (tests only)

**Spec:** `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md`. **Prerequisite:** Plan 2a (`docs/superpowers/plans/2026-09-27-jig-plan-2a-services.md`), including its Rulings R1–R18, which still apply.

**Research notes:** `.superpowers/handoff/research/lib-apis.md` has the verified bubbletea v2, bubbles v2, lipgloss v2, glamour, chroma, fuzzy, udiff, and pty signatures. The facts that matter most:
- `View()` returns `tea.View`.
- `lipgloss.Color` is a function.
- Alt screen and keyboard enhancements are `tea.View` fields.
- `tea.Raw(string) Cmd` exists.
- `textarea` has `DynamicHeight`.

`.superpowers/handoff/research/slk.md` covers the code to port.

## Global Constraints

- Every Plan 1 and Plan 2a constraint holds, including the size limits. `App` is a struct like any other: ≤ 15 fields and ≤ 20 methods.
- `internal/bubbles/<widget>` may not import another widget, `internal/core`, `internal/service`, `internal/ui`, `internal/clock`, or `internal/ids` (archtest, Plan 2a Task 1). A widget has no `func(tea.Msg)` field, no package-level vars, and no parameters on `View`.
- Widget construction is `New(requiredDeps..., opts ...Option)`. `Option` is `func(*Model)`, and every widget has `WithStyles(Styles)` and `WithKeyMap(KeyMap)` options where it has styles or keys. No global theme state exists anywhere.
- Every widget has behavior tests driven through `Update` and golden frames at fixed sizes with pinned `Styles` (`golden.Assert`, `t.Parallel()`).
- **Untrusted text:** every model, tool, file, session, and path string passes `ansi.Sanitize`/`SanitizeLine` in `ui` before it reaches a widget or renderer. Only jig's own styling escapes, kitty placeholder cells produced by `imgrender`, and `tea.Raw` payloads produced by `imgrender` reach the terminal.
- **No slash commands, ever.** Every action goes through the `ctrl+p` picker (and keys).
- Exact numbers from the spec:
  - sidebar shown at ≥ 120 cols; 32% of the width, clamped to 30–50 cols;
  - details split 50% of the width (full width when narrow);
  - minimum usable size 80×24;
  - prompt grows 1–8 lines;
  - paste chip threshold > 500 chars;
  - prompt history: last 100 per project;
  - recent actions: 5;
  - picker width `min(80, 70% of terminal)`, height ≤ 60%;
  - streaming re-render ≤ every 80 ms;
  - blocklist keeps lines cached within 2 screen heights of the viewport.
- Exact copy from the spec:
  - placeholder `Message <agent>…  (ctrl+p actions · @ files)`;
  - `⚠ permission pending · esc gp`;
  - `a allow · A always (this exact command) · d deny · D deny with message`;
  - `⏳ queued`;
  - `untrusted`.
- Subagent models: `anthropic/claude-sonnet-5` by default; `anthropic/claude-opus-5-5` for tasks marked **(opus)**.

## Rulings (in addition to Plan 2a's R1–R18)

- **R19 — The file-list port returns `[]core.ProjectFile{Path string; Modified bool}`**, not `[]string`, so the file picker can rank git-modified files without a second port call. Paths are workdir-relative and slash-separated.
- **R20 — `$EDITOR` goes through a port.** `core.EditorService.Edit(text) (core.ExecCommand, func() (string, error), error)`. `core.ExecCommand` has the method set of `tea.ExecCommand`, so `ui` runs it with `tea.Exec` and never imports `os/exec`.
- **R21 — Remappable action IDs** are the spec's list plus `picker.open`. These keys are fixed in the mode handlers and cannot be remapped: `enter`, `shift+enter`/`alt+enter`, `esc`, `tab`/`shift+tab`, `@`, `↑`/`↓`, `ctrl+c`, `ctrl+d`, and NORMAL's `j k gg G ctrl+d ctrl+u q n N i a A d D gp ctrl+e ctrl+y`. The default bindings (registered through `ext.Keybind`) are:
  - `picker.open`: `ctrl+p`, `ctrl+t` (both modes);
  - `view.sidebar`: `ctrl+b` (both);
  - `prompt.editor`: `ctrl+e` (insert);
  - in normal mode: `transcript.details` `enter`, `transcript.search` `/`, `transcript.yank` `y`, `help.keys` `?`, `run.cancel` `ctrl+c`.
- **R22 — Tool durations are measured by the App** when it receives `ToolCallStarted`/`ToolCallFinished` (injected clock). Blocks loaded from history show no duration.
- **R23 — The details for an `edit`** read the edited file through `ProjectService.ReadFile`. If the file contains `new_string` exactly once, the diff is (file with `new_string` → `old_string`) vs. file, with 3 lines of context. Otherwise it is a bare `old_string` vs. `new_string` diff.
- **R24 — Image cells default to 8×16 px.** Detection order:
  1. the `JIG_IMAGES` override;
  2. `tea.TerminalVersionMsg` (`kitty`/`ghostty` → kitty, `foot`/`mlterm` → sixel);
  3. the env (slk's rules, without the tmux shell-out: under tmux → blocks);
  4. otherwise blocks.
- **R25 — `jig` with no subcommand is the TUI**, taking the flags `--cwd DIR`, `--session ID`, and `--trust-project`. If stdout is not a terminal, it exits 2 with `jig: the interactive UI needs a terminal; use: jig run "prompt"`.
- **R26 — The user block shows attachments** as a dim second line: `  + a.go, shot.png`.
- **R27 — The pty test matches on screen text.** It reconstructs the screen with `github.com/charmbracelet/x/vt` (test-only) if matching on `ansi.Strip`ped raw output proves flaky. Add it only if needed, and record the choice in the ledger.

## Carry-over from Plan 2a (as executed — overrides this plan's text where they conflict)

**API changes Plan 2a made that this plan must use:**
- **Prefs:** `core.PrefsService` is `Get()` + `Update(fn func(*Prefs)) error`. There is no `Save`. Everywhere below that says `Prefs.Save(...)`, use `Prefs.Update(func(p *core.Prefs){ … })`.
- **Transcript:**
  - `transcript.Projection.AddUser(text string, attachments []string) BlockID` appends the live user block (ID `u/pending/<k>`). The App calls it on send.
  - `Block.Title` holds the compaction notice's headline, and `Text` its summary.
  - `Apply` returns nil when nothing visible changed. Re-read `Pending()` after every permission event.
  - `Load` keeps the subagent owner map and unanswered permission requests.
- **agent-browser preset:** it is not a config layer. `permission.NewHook(cfg, asker, permission.WithPreset(rules))` consults it only for an ask that came from the Default. The screenshot allow is exactly `agent-browser screenshot`, and `$` counts as a shell metacharacter.
- **Trust:**
  - `trustfs.Store.Get(project, hash)` checks whether `hash` is in the project's set of granted hashes (≤ 16 per project).
  - Effects print `base_url` as `scheme://host[:port]/path` and print `{env:}`/`{file:}` tokens raw. A token-valued permission is listed in the effects and counts as dropped.
  - `config.Load` takes a 4th `config.Options{SubstituteProject bool}`.
- **Media:**
  - Only the newest 20 media per request are sent.
  - The image cap is 5 MiB after base64 encoding.
  - `Media` errors never include paths.
- **Rendering:** `ui/plain` and `internal/app` output are sanitized. `internal/app` has `printLine` for this.

**Must-do items folded into the tasks named below:**
- Task 1 / Task 17 (session Configure, model picker): `SessionService.Configure` must validate the model with `agents.ResolveRef` (aliases), and accept primary agents only.
- Task 13 (render): make tool block IDs collision-proof against `n/<k>`/`u/…`, for example by prefixing `t/` inside `ui/transcript` with a test. Also don't spin a Streaming indicator for a block that was superseded.
- Task 15 (App core):
  - Call `projection.Load` only while idle, or right after `StepFinished`/`RunFinished`/`RunFailed`. A mid-step `Load` drops unsaved blocks.
  - Normalize `ChangedFiles` paths by giving the projection the workdir, or with `path.Clean`/`Join(workDir, p)`.
  - `chat.Compact` after `Close` must return `ErrClosed`.
- Task 17: the `session.compact` action handles `core.ErrBusy` and `ErrClosed`.
- Task 19 (trust dialog):
  - Skip the dialog when there are zero effects.
  - The keybinds of an untrusted project must not rebind the permission-card keys, `run.cancel`, or `app.quit`. Filter them in `actions.Resolve`, or drop project keybinds in `trust.Restrict`; pick one and test it.

**Deferred minors from Plan 2a:** a reviewer may pick these up. None of them blocks 2b.
- `config`/`agentfs`/`contextfs`/`skillfs` read project files with unbounded `os.ReadFile`, so a symlink to `/dev/zero` hangs. Add a regular-file check and a size cap.
- Effects: the `jigtoken` placeholder can collide with a literal, which could spoof a displayed host.
- `trustfs`: a Stat→Open swap to a FIFO is possible.
- `optionTokens` skips arrays of tables.
- The archtest `func(tea.Msg)` check misses `[]func`, `map` values, and a local `Msg` alias.
- Verify the argument surface of agent-browser `read*`/`snapshot*`.
- `media`: peak memory near `MaxPixels`. Lowering `MaxPixels` would help.
- Blob loads ignore ctx cancellation.
- On macOS, screenshot attachment depends on agent-browser writing to the same `$TMPDIR` that jig sees. Verify on a Mac.
- Agent-browser detection runs for every subcommand.
- `ListRootsByCwd` duplicates `ListSessions` boilerplate.
- The 20 MiB image cap is duplicated in read, chat, and media.

## Review Focus

1. **The terminal shrinks below 80×24**, even to 20×5 or a single row, or the width changes mid-stream. There is no panic, no negative sizes, and no line wider than the terminal; the sidebar hides below 120 cols and the details split goes full width. Tested in Task 15 (`TestApp_TinySizesDoNotPanic`, table over sizes) and Task 16 (golden `narrow_details`).
2. **A permission request arrives while the user is typing.** The text is untouched, focus stays in INSERT, and the status bar shows the pending hint. With an empty prompt, focus jumps to the card. Tested in Task 18 (`TestApp_PermissionDoesNotStealFocusWhileTyping`, `TestApp_PermissionFocusesWhenPromptEmpty`).
3. **A theme is switched (or previewed and then cancelled with `esc`) mid-stream.** Every widget re-renders with the new palette, no stale cached lines survive, and `esc` restores the old theme exactly. Tested in Task 7 (`TestBlocklist_StylesVersionInvalidatesCache`) and Task 17 (`TestApp_ThemePreviewEscRestores`).
4. **A 2,000-block session is loaded and then scrolled and streamed into.** It stays within the recorded budget and holds only O(visible) rendered lines in memory. Tested in Task 7 (`BenchmarkBlocklist_View2000`, `BenchmarkBlocklist_Update2000`, `TestBlocklist_EvictsFarLines`).
5. **`ctrl+c` with a queued send, a running run, a non-empty prompt, or nothing.** The spec's exact ladder applies, and a queued message is dropped before the run is cancelled, never sent after it. Tested in Task 15 (`TestApp_CtrlCLadder`).

## File Map

```
internal/bubbles/{mdrender,coderender,imgrender,blocklist,picker,prompt,details,permcard,statusbar,sidebar,confirm}/
internal/ui/theme/        palettes (ported), Palette → widget Styles, Set + Version
internal/ui/actions/      action catalogue, default bindings, keymap resolution
internal/ui/app.go        App: New/Init/Update/View, program-level msgs
internal/ui/bridge.go     bus Subscription → tea.Msg
internal/ui/layout.go     wintree layout (sidebar/details/narrow)
internal/ui/session.go    sessionState: projection, usage, cost, todos, run, queue (reconciliation)
internal/ui/mode_insert.go  mode_normal.go  mode_picker.go
internal/ui/render.go     RenderFunc + tool one-line formatters (§5.3)
internal/ui/details.go    details content builder (§5.4)
internal/ui/pickerlevels.go  root/sessions/models/agents/themes/rename/files levels
internal/ui/cmds.go       tea.Cmd wrappers around core ports
internal/data/themefs/    custom theme TOML loading
internal/app/tui.go  trustdialog.go  ports.go
e2e/tui_test.go
```

---

### Task 1: Read-only ports and their `internal/app` adapters

**Model:** sonnet

**Files:**
- Modify: `internal/core/ports.go`, `internal/client/llm/source.go` (`HasCredentials`), `internal/client/search/` (new `git.go`)
- Create: `internal/app/ports.go`
- Test: `internal/app/ports_test.go`, `internal/client/search/git_test.go`, `internal/client/llm/source_test.go`

**Interfaces:**
- Produces:
  ```go
  package core
  type ProviderStatus struct{ Info ProviderInfo; Configured bool }   // Configured: credentials resolvable now
  type CatalogService interface{ Providers() []ProviderStatus }      // sorted by provider ID
  type AgentService   interface{ Primary() []Agent }
  type ProjectFile    struct{ Path string; Modified bool }           // R19
  type ProjectService interface {
      Files(ctx context.Context) ([]ProjectFile, error)               // ≤ 20000, gitignore-aware
      ReadFile(ctx context.Context, path string) ([]byte, error)     // workdir, spill dir, or blob dir only; ≤ 10 MiB
  }
  type BlobService   interface{ Open(ref string) ([]byte, string, error) }   // bytes, MIME (http.DetectContentType)
  type ExecCommand   interface{ Run() error; SetStdin(io.Reader); SetStdout(io.Writer); SetStderr(io.Writer) }
  type EditorService interface{ Edit(text string) (ExecCommand, func() (string, error), error) }  // R20
  package llm
  func (s *Source) HasCredentials(providerID string) bool
  package search
  func GitModified(ctx context.Context, dir string) ([]string, error)   // `git status --porcelain -z`, workdir-relative; not a repo → nil, nil
  package app
  // ports.go: catalogPort, projectPort, blobPort, editorPort; agents.Service already satisfies AgentService.
  ```
  **Rules:**
  - `projectPort.ReadFile` makes a relative path absolute against the workdir, then takes `k := pathid.Key(p)`. `k` must equal a root, or have `root + "/"` as a prefix, where the roots are `pathid.Key` of the workdir, the spill dir, and the blob dir. Otherwise it returns the error `path <p> is outside the project`.
  - `editorPort.Edit` writes the text to `os.CreateTemp(spillDir, "prompt-*.md")`. The command is `$VISUAL`, else `$EDITOR`, else `vi`, split with `strings.Fields`, plus the file. The result func reads the file back, trims one trailing newline, and removes it.
  - `execCmd{*exec.Cmd}` implements `ExecCommand`.

- [ ] **Step 1: Write the failing tests.**
  - `TestProjectPort_ReadFileConfinement`: allowed — a workdir file, a spill-dir file, and a blob. Refused:
    - `../x`;
    - an absolute path elsewhere;
    - a workdir symlink pointing outside;
    - a sibling dir sharing the prefix (`/w` vs `/w2`);
    - a file over 10 MiB (fake size).
  - `TestProjectPort_FilesMarksModified`: a real `git init` in a temp dir (skip if `git` is missing), one committed file edited → `Modified: true`.
  - `TestEditorPort_RoundTrip`: `EDITOR` = a shell script that appends `!` → the result has the text plus `!`, and the temp file is gone.
  - `TestCatalogPort_Configured`: a provider with its env key set → true; unset → false.
  - `TestGitModified_NotARepo`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** List the new ports in "Architecture in one screen". Add an Invariant: "`ProjectService.ReadFile` is confined to the workdir, spill dir, and blob dir, after resolving symlinks."
- [ ] **Step 6: Commit.** `git commit -am "feat(ports): catalog, agents, project, blobs, editor ports for the TUI"`

### Task 2: Theme palettes and custom theme loading

**Model:** sonnet

**Files:**
- Create: `internal/ui/theme/{palette.go,builtin_a.go,builtin_b.go,builtin_c.go}`, `internal/data/themefs/themefs.go`
- Test: `internal/ui/theme/palette_test.go`, `internal/data/themefs/themefs_test.go`
- Port from: `~/local_code/slk/internal/ui/styles/themes.go` (the data) and `tint.go` (`mixColors`)

**Interfaces:**
- Produces:
  ```go
  package theme
  type Palette struct {
      Name string
      Primary, Accent, Warning, Error, Background, Surface, SurfaceDark, Text, TextMuted, Border string
      SidebarBackground, SidebarText, SidebarTextMuted string
      SelectionBackground, SelectionForeground, SearchHighlightBg, SearchHighlightFg string
      ComposeInsertBG, SelectionBgFocused, SelectionBgUnfocused string
  }   // hex or ANSI index strings, as slk's ThemeColors (minus Rail)
  func Builtin() []Palette                        // slk's 56 themes, alphabetical by Name
  func Default() Palette                          // "dark"
  func Lookup(name string, custom []Palette) (Palette, bool)   // case-insensitive; custom wins
  // Complete fills empty fields: from "dark" for base colors, then slk's
  // derivation rules (sidebar/selection/search fall back; tints via mix).
  func Complete(p Palette) Palette
  func Custom(name string, colors map[string]string) (Palette, []string)  // snake_case keys; unknown keys → warnings
  package themefs
  type Theme struct{ Name string; Colors map[string]string }
  // Load reads dir/*.toml (top level only): name = the file's "name" key, else the file stem; [colors] table.
  func Load(dir string) ([]Theme, []string, error)   // missing dir → nil, nil, nil; per-file errors → warnings
  ```
  `ui/theme` imports no third-party package (`image/color` and the lipgloss conversion happen in the per-widget mapping functions Tasks 3–11 add, which use `charm.land/lipgloss/v2`).

- [ ] **Step 1: Write the failing tests.**
  - `TestBuiltin_Count56AndComplete`: every builtin palette, after `Complete`, has every field non-empty, and every value parses with `lipgloss.Color` without panicking.
  - `TestLookup_CaseInsensitiveCustomWins`.
  - `TestCustom_UnknownKeyWarns`.
  - `TestThemefs_LoadsTopLevelTOML`: a subdir is ignored; a bad file becomes a warning.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Port** the palette data, split across files that each stay under 500 lines.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui/theme): palettes ported from slk; custom theme files"`

### Task 3: `bubbles/mdrender`

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/mdrender/{mdrender.go,styleconfig.go}`, `internal/ui/theme/widgets.go` (first mapping func + `Set`)
- Test: `internal/bubbles/mdrender/mdrender_test.go` (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package mdrender
  type Styles struct{ Text, Muted, Heading, Link, Code, CodeBg, Quote, Rule color.Color }
  func DefaultStyles() Styles
  type Renderer struct{ /* styles; renderers map[int]*glamour.TermRenderer */ }
  func New(opts ...Option) *Renderer
  func WithStyles(Styles) Option
  func (r *Renderer) SetStyles(st Styles)          // drops cached TermRenderers
  // Render renders markdown at width. Leading and trailing blank lines are
  // trimmed; a glamour error falls back to ansi.Wrap of the input.
  func (r *Renderer) Render(md string, width int) []string
  package theme
  type Set struct{ Version int; Markdown mdrender.Styles /* + one field per later widget */ }
  func Build(p Palette, version int) Set
  ```
  The `glamour` `ansi.StyleConfig` is built from `Styles`: colors become `#rrggbb` `*string`s. Code blocks use chroma via `WithChromaFormatter("terminal16m")`. There is one `TermRenderer` per width, created lazily.

- [ ] **Step 1: Write the failing tests.**
  - `TestRender_Golden`: a heading, a list, inline code, a fenced Go block, and a link, at widths 40 and 80 → goldens `md_40`, `md_80`.
  - `TestRender_WidthRespected`: every line has `ansi.Width(line) <= width`.
  - `TestRender_CachesPerWidth`: two renders at the same width reuse the renderer (white-box `len(r.renderers) == 1`).
  - `TestSetStyles_ChangesOutput`.
  - `TestBuild_MarkdownFromPalette`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Add glamour v2.0.1.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles/mdrender): width-aware markdown rendering"`

### Task 4: `bubbles/coderender` — highlight and diff

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/coderender/{highlight.go,diff.go}`
- Modify: `internal/ui/theme/widgets.go` (`Set.Code`)
- Test: `internal/bubbles/coderender/{highlight_test.go,diff_test.go}` (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package coderender
  type Styles struct{ Plain, Keyword, Type, Name, Func, String, Number, Comment, Operator, Added, Removed, Hunk, Gutter lipgloss.Style }
  func DefaultStyles() Styles
  // Highlight tokenizes with lexers.Match(path) (fallback lexers.Analyse, then
  // plaintext), maps token categories to Styles, expands tabs to 4 spaces, and
  // returns one string per source line.
  func Highlight(path, code string, st Styles) []string
  // Diff returns a unified diff of before→after (udiff, context lines) as
  // styled lines: "@@" hunk headers, then "+"/"-"/" " lines whose content
  // is highlighted and whose background is Added/Removed.
  func Diff(path, before, after string, context int, st Styles) (lines []string, hunks int)
  func DiffText(before, after string, context int) string   // plain unified text (for yank)
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestHighlight_GoKeywordsStyled`: with `Keyword` = bold+red pinned, `func` renders with that SGR.
  - `TestHighlight_UnknownExtensionPlain`.
  - `TestHighlight_LineCount`: the output has as many lines as the input's `\n`-split lines.
  - `TestDiff_HunksAndContext`: a 20-line file with changes at lines 3 and 15, context 3 → `hunks == 2`, and the first hunk header is `@@ -1,7 +1,7 @@`.
  - `TestDiffText_MatchesUdiff`.
  - golden `diff_go`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Add chroma v2.27.0 and go-udiff v0.4.1. Use `udiff.Lines` plus `udiff.ToUnified` to get the hunks.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles/coderender): syntax highlighting and unified diffs"`

### Task 5: `bubbles/imgrender` — detection, decode, half-blocks

**Model:** opus (image pipeline)

**Files:**
- Create: `internal/bubbles/imgrender/{detect.go,decode.go,blocks.go,imgrender.go}`
- Test: `internal/bubbles/imgrender/{detect_test.go,blocks_test.go}` (+ goldens)
- Port from: `~/local_code/slk/internal/image/capability.go` (detection rules only)

**Interfaces:**
- Produces:
  ```go
  package imgrender
  type Protocol int   // Off, Blocks, Sixel, Kitty
  func (p Protocol) String() string   // "off","blocks","sixel","kitty"
  type Env struct{ Term, TermProgram, KittyWindowID, TMUX, Override string }   // Override = $JIG_IMAGES
  func Detect(env Env, terminalName string) Protocol   // R24
  func Decode(data []byte) (image.Image, error)          // png/jpeg/gif/webp
  type Result struct {
      Lines  []string   // exactly Rows lines, each of display width Cols
      Upload string     // kitty: APC upload sequence (send once via tea.Raw); "" otherwise
      Sixel  string     // sixel payload to place over the reserved cells; "" otherwise
  }
  type Renderer struct{ /* protocol, cell size, kitty ids by key, uploaded set */ }
  func New(p Protocol, opts ...Option) *Renderer
  func WithCellSize(w, h int) Option                     // default 8×16 (R24)
  func (r *Renderer) Protocol() Protocol
  // Render fits img into maxCols×maxRows cells, keeping its aspect ratio.
  func (r *Renderer) Render(key string, img image.Image, maxCols, maxRows int) Result
  // Place returns the tea.Raw payload drawing res.Sixel at screen cell (x, y), 0-based; "" if none.
  func Place(res Result, x, y int) string
  ```
  In this task only `Blocks` and `Off` render: half-blocks `▀` with the top pixel as fg and the bottom pixel as bg, truecolor SGR. `Off` gives one line, `[image WxH]`. `Kitty` and `Sixel` fall back to `Blocks` until Task 6.

- [ ] **Step 1: Write the failing tests.**
  - `TestDetect_Table`:
    - `Override` `kitty`/`sixel`/`blocks`/`off` wins;
    - terminal name `kitty(0.36)` → Kitty, `ghostty 1.1` → Kitty, `foot` → Sixel;
    - env `KITTY_WINDOW_ID` → Kitty; `TERM_PROGRAM=ghostty` → Kitty; `TERM=foot` → Sixel; `TERM_PROGRAM=iTerm.app` → Sixel;
    - `TMUX` set without an override → Blocks;
    - otherwise → Blocks.
  - `TestBlocks_2x4Golden`: a 2×4 px image with known colors → 2 cols × 2 rows, exact SGR bytes (golden `blocks_2x4`).
  - `TestRender_FitsAspect`: a 1000×500 image in 40×40 cells at 8×16 px → cols ≤ 40, rows ≤ 40, and cols/rows ≈ 4 ±1.
  - `TestDecode_Webp`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Scale with `x/image/draw.ApproxBiLinear`.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles/imgrender): protocol detection, decode, half-block rendering"`

### Task 6: `bubbles/imgrender` — kitty placeholders and sixel

**Model:** opus (image pipeline)

**Files:**
- Create: `internal/bubbles/imgrender/{kitty.go,diacritics.go,sixel.go}`
- Test: `internal/bubbles/imgrender/{kitty_test.go,sixel_test.go}`
- Port from: `~/local_code/slk/internal/image/kitty.go` (placeholder protocol, diacritics table, chunking, tmux wrap)

**Interfaces:**
- Consumes/produces: Task 5's API; `Kitty` and `Sixel` now render natively.
- **Kitty:**
  - Image IDs are allocated per `key`, from 1 upward (24-bit).
  - `Lines` are rows of `ansi.PlaceholderRune` plus a row diacritic and a column diacritic, wrapped in fg SGR `38;2;R;G;B` encoding the id, then reset.
  - `Upload` is set only the first time a `(key, cols, rows)` is rendered. It is the PNG base64 chunked at 4096 bytes: `\x1b_Ga=T,f=100,t=d,i=<id>,U=1,c=<cols>,r=<rows>,q=2,m=<0|1>;<chunk>\x1b\\`.
  - Under `TMUX` (a `WithTmux(bool)` option, set by `ui` from the env), each sequence is wrapped as `\x1bPtmux;` + ESC-doubled seq + `\x1b\\`.
- **Sixel:**
  - A hand-rolled encoder (no new dep). Quantize with `draw.FloydSteinberg` onto `palette.WebSafe`, then emit `\x1bPq`, the raster attributes, the palette defs, and six-row bands, then `\x1b\\`.
  - `Lines` are blank cells of width Cols, reserving the space.
  - `Place` returns `"\x1b7" + CUP(y+1, x+1) + Sixel + "\x1b8"`.

- [ ] **Step 1: Write the failing tests.**
  - `TestKitty_IDEncodedInForeground`: id 0x010203 → lines contain `38;2;1;2;3m`.
  - `TestKitty_UploadOnce`: the second `Render` with the same key and size has `Upload == ""`; a different size uploads again.
  - `TestKitty_Chunking`: a 100×100 noise image → every chunk ≤ 4096 base64 bytes; `m=1` on all but the last.
  - `TestKitty_TmuxWrap`.
  - `TestKitty_DiacriticsRowCol`: the cell at row 2, col 3 carries `diacritics[2]` then `diacritics[3]`.
  - `TestSixel_TwoByTwoGolden` (exact bytes).
  - `TestPlace_CursorSaveRestore`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Add an Invariant: "Only `imgrender` produces kitty placeholder cells and raw image payloads; `ui` sends payloads with `tea.Raw`, never inside `View`."
- [ ] **Step 6: Commit.** `git commit -m "feat(bubbles/imgrender): kitty unicode placeholders and sixel"`

### Task 7: `bubbles/blocklist`

**Model:** opus (the core scrolling/caching widget)

**Files:**
- Create: `internal/bubbles/blocklist/{blocklist.go,cache.go,scroll.go,search.go,keys.go}`
- Modify: `internal/ui/theme/widgets.go` (`Set.Blocklist`)
- Test: `internal/bubbles/blocklist/{blocklist_test.go,scroll_test.go,search_test.go,bench_test.go}` (+ goldens)
- Reference: slk `internal/ui/messages/model.go`'s cache, `entryOffsets`, and `yOffset` shape (research/slk.md §4)

**Interfaces:**
- Produces:
  ```go
  package blocklist
  type Item struct{ ID string; Version int; Data any }         // Data is opaque to the widget
  type RenderFunc func(item Item, width int, st Styles) []string
  type Styles struct {
      Bar, SelectedBg lipgloss.Style                           // left bar (accent) + subtle background on the selected item
      MatchOn, MatchOff string                                 // search highlight SGR on/off
      Track, Thumb, ScrollBg color.Color
      Gap int                                                  // blank lines between items (default 1)
  }
  type KeyMap struct{ Down, Up, Bottom, HalfDown, HalfUp, NextMatch, PrevMatch key.Binding }  // j k G ctrl+d ctrl+u n N
  func DefaultStyles() Styles
  func DefaultKeyMap() KeyMap
  func New(render RenderFunc, opts ...Option) Model
  func (m *Model) SetSize(w, h int)
  func (m *Model) SetStyles(st Styles, version int)           // version = App stylesVersion
  func (m *Model) SetItems(items []Item)                     // full replace; keeps the selection by ID (else the last item)
  func (m *Model) Upsert(items ...Item)                      // replace by ID, or append
  func (m Model) Len() int
  func (m Model) Selected() (Item, bool)
  func (m *Model) Select(id string) bool
  func (m *Model) Top()
  func (m *Model) Bottom()
  func (m *Model) SetSearch(query string) int                // number of matching items; "" clears
  func (m Model) Update(msg tea.Msg) (Model, tea.Cmd)        // KeyMap only; no async messages
  func (m Model) View() string                               // exactly h lines of width w
  ```
  **Cache:** a `*cache` pointer field holds everything below, so `View` on a value can memoize.
  - Per item: `key{ID, Version, width, stylesVersion}` → lines (evictable) plus height (kept).
  - Heights for all items are prefix-summed into offsets.
  - After each `View`, lines for items entirely outside `[yOffset − 2h, yOffset + 3h)` are evicted; heights stay.
  - An evicted item re-renders on demand.

  **Scrolling:**
  - Movement changes the selection. The view scrolls the minimum needed to keep the selected item's first line visible, or its whole height if it fits.
  - Pinning: if the selected item is the last one, the view is bottom-aligned after every `SetItems`/`Upsert`/`SetSize`.
  - `HalfDown`/`HalfUp` move the selection to the item at `yOffset ± h/2`.

  **Search:** an item matches if its rendered lines, stripped of escapes (`xansi.Strip`), contain the query case-insensitively. Visible lines of matching items are drawn through `ansi.Highlight(line, query, MatchOn, MatchOff)`. `n`/`N` select the next/previous matching item, wrapping.

  **Selection drawing:** each selected line is prefixed `Bar.Render("▌")` and styled with `SelectedBg`. Unselected lines get a 1-space prefix. The item width passed to `RenderFunc` is `w − 2` (prefix + scrollbar column). The scrollbar is `scrollbar.Overlay`.

- [ ] **Step 1: Write the failing tests** (a `RenderFunc` returning `Data.(string)` split on `\n`).
  - `TestNav_JKGgBottom`: j/k and G; `Top()`/`Bottom()`.
  - `TestView_ExactSize`: any item mix gives exactly h lines, each exactly w wide.
  - `TestScroll_KeepsSelectionVisible`: a tall item taller than h shows its first line.
  - `TestPinned_StaysAtBottomOnUpsert`: the selection on the last item stays pinned while the last item grows; with the selection elsewhere, `yOffset` doesn't move when the last item grows.
  - `TestSetItems_PreservesSelectionByID`.
  - `TestHalfPage`.
  - `TestBlocklist_EvictsFarLines`: 500 items, scroll to the middle → the cached line entries are ≤ items within 5h lines (white-box count); scrolling back re-renders them (a render counter increments).
  - `TestBlocklist_StylesVersionInvalidatesCache`: `SetStyles(st, 2)` re-renders every visible item.
  - `TestSearch_HighlightsAndNavigates`: 3 of 10 items match; n/N cycle among them; the highlight is present in `View`.
  - `TestSearch_DoesNotMatchInsideEscapes`: an item containing `\x1b[31m` with the query `31m` → no match.
  - goldens: `list_selected`, `list_search`.
- [ ] **Step 2: Write the benchmarks** (`bench_test.go`): 2,000 items of 1–12 lines, 120×40.
  - `BenchmarkBlocklist_View2000`: warm cache, `View` after one `j`.
  - `BenchmarkBlocklist_Update2000`: an `Upsert` of the last item (streaming) plus `View`.
  - `BenchmarkBlocklist_Load2000`: `SetItems` + the first `View`.
- [ ] **Step 3: Run** `go test ./internal/bubbles/blocklist/`. Expected: FAIL.
- [ ] **Step 4: Implement.** Add bubbletea v2.0.10 and bubbles v2.2.1.
- [ ] **Step 5: Run** `go test -race ./internal/bubbles/blocklist/` and `go test -bench . -benchmem ./internal/bubbles/blocklist/`.
  - Required on the dev machine: View2000 < 2 ms/op, Update2000 < 3 ms/op, Load2000 < 250 ms/op.
  - If a budget is missed, fix the algorithm; do not raise the budget.
  - Record the measured numbers and the budget in `AGENTS.md` under a new "Performance budgets" section.
- [ ] **Step 6: Run** `make check`. Expected: PASS.
- [ ] **Step 7: Commit.** `git commit -m "feat(bubbles/blocklist): block cursor list with render cache, pinning, search"`

### Task 8: `bubbles/picker`

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/picker/{picker.go,match.go,keys.go,view.go}`
- Modify: `internal/ui/theme/widgets.go` (`Set.Picker`)
- Test: `internal/bubbles/picker/{picker_test.go,match_test.go}` (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package picker
  type Level struct {
      ID, Title string
      Arg       string   // opaque payload for the loader (e.g. a provider ID)
      Multi     bool     // tab marks items
      Input     bool     // single-line text entry instead of a list (rename)
      Initial   string   // Input levels: prefilled text
      Actions   bool     // root action list: Recent group + recency tie-break
  }
  type Item struct {
      ID, Title, Detail, Group string
      Disabled, Current        bool     // Current shows ●; Disabled is dimmed and cannot be chosen
      Drill                    *Level   // enter opens this level
  }
  type LoadFunc func(level Level) tea.Cmd                 // must yield ItemsMsg
  type PreviewFunc func(level Level, item Item) tea.Cmd   // optional (themes)
  type ItemsMsg struct{ Level string; Items []Item; Err error }
  type ChosenMsg struct{ Level Level; Items []Item }      // enter on a non-drill item; Multi: marked, else current
  type InputMsg  struct{ Level Level; Text string }
  type ClosedMsg struct{ Level Level }                    // esc
  func New(load LoadFunc, opts ...Option) Model
  func WithPreview(PreviewFunc) Option
  func (m *Model) Open(root Level) tea.Cmd
  func (m *Model) Close()
  func (m Model) IsOpen() bool
  func (m *Model) SetRecent(ids []string)                 // most recent first
  func (m *Model) SetSize(termW, termH int)              // box: width min(80, 70% termW), height ≤ 60% termH
  func (m Model) Update(msg tea.Msg) (Model, tea.Cmd)
  func (m Model) View() string                           // the box only; the App centers it with overlay.Center
  ```
  **Behavior (spec §6.4, §7.1):**
  - Typing filters with `fuzzy.FindFrom` over `Title + " " + Detail`, ranked by score. Ties go to recency (for `Actions` levels), then original order. Matched runes are styled `Match`.
  - With an empty query, items are grouped by `Group` in first-appearance order, under headers. An `Actions` level gets a leading `Recent` group, built from `SetRecent` IDs that exist, ≤ 5.
  - `↑`/`↓` and `ctrl+n`/`ctrl+p` move. `enter` chooses or drills. `tab` marks (Multi only). `backspace` on an empty query pops a level. `esc` closes.
  - `ItemsMsg` for a level that is not on top is ignored.
  - A preview is emitted whenever the highlighted item changes on a level where `WithPreview` is set.

- [ ] **Step 1: Write the failing tests.**
  - `TestPicker_FilterRanksAndTieBreaks`.
  - `TestPicker_RecentGroupAtRoot`.
  - `TestPicker_DrillAndBackspacePops`.
  - `TestPicker_MultiMark`.
  - `TestPicker_DisabledNotChosen`.
  - `TestPicker_InputLevelEmitsInputMsg`.
  - `TestPicker_StaleItemsIgnored`.
  - `TestPicker_PreviewOnMove`.
  - `TestPicker_SizeClamp` (at 200×50, the box width is 80; at 60×20, it is 42).
  - goldens: `picker_root`, `picker_filtered`, `picker_drill`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Add sahilm/fuzzy v0.1.3. The input uses `bubbles/v2/textinput`.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles/picker): fuzzy drill-down picker with groups, recents, marks, preview"`

### Task 9: `bubbles/prompt`

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/prompt/{prompt.go,chips.go,history.go,keys.go}`
- Modify: `internal/ui/theme/widgets.go` (`Set.Prompt`)
- Test: `internal/bubbles/prompt/{prompt_test.go,chips_test.go,history_test.go}` (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package prompt
  type EditFunc func(text string) tea.Cmd   // must yield EditedMsg
  type EditedMsg  struct{ Text string; Err error }
  type SubmitMsg  struct{ Text string }     // enter with non-blank text; paste chips expanded
  type MentionMsg struct{}                  // '@' typed; nothing inserted
  type KeyMap struct{ Submit, Newline, Editor, HistoryPrev, HistoryNext key.Binding }
  func New(edit EditFunc, opts ...Option) Model
  func (m *Model) SetWidth(w int)
  func (m Model) Height() int                // 1..8 content lines + border
  func (m *Model) SetAgent(name string)      // placeholder "Message <agent>…  (ctrl+p actions · @ files)"
  func (m *Model) SetQueued(q bool)          // border title "⏳ queued"
  func (m *Model) SetHistory(entries []string)   // oldest first
  func (m *Model) Insert(s string)
  func (m Model) Value() string              // chips expanded
  func (m *Model) Reset()
  func (m *Model) Focus() tea.Cmd
  func (m *Model) Blur()
  func (m Model) Update(msg tea.Msg) (Model, tea.Cmd)
  func (m Model) View() string
  ```
  **Behavior (spec §7.3):**
  - `textarea` with `DynamicHeight`, `MinHeight 1`, and `MaxContentHeight 8`.
  - `enter` → `SubmitMsg`, unless blank. `shift+enter`/`alt+enter` → newline. `ctrl+e` → `edit(Value())`; `EditedMsg` replaces the text.
  - `↑` on the first line and `↓` on the last line walk the history. The current draft is restored at the end.
  - A `tea.PasteMsg` over 500 chars inserts the token `[pasted N chars]`. A backspace with the cursor right after a chip token removes the whole token and its text.
  - `@` → `MentionMsg`.

- [ ] **Step 1: Write the failing tests.**
  - `TestPrompt_SubmitAndBlank`.
  - `TestPrompt_NewlineKeys`.
  - `TestPrompt_GrowsToEightLines` (`Height` for 1, 5, and 12 lines).
  - `TestChips_PasteCollapsesAndExpands`.
  - `TestChips_BackspaceRemovesWholeChip`.
  - `TestHistory_WalkAndRestoreDraft`.
  - `TestPrompt_EditorRoundTrip` (the fake `EditFunc` yields `EditedMsg{"new"}`).
  - `TestPrompt_AtEmitsMention`.
  - goldens: `prompt_placeholder`, `prompt_queued`, `prompt_chip`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles/prompt): growing prompt with history, paste chips, editor, queue state"`

### Task 10: `bubbles/details` and `bubbles/permcard`

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/details/details.go`, `internal/bubbles/permcard/permcard.go`
- Modify: `internal/ui/theme/widgets.go`
- Test: `internal/bubbles/details/details_test.go`, `internal/bubbles/permcard/permcard_test.go` (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package details
  type Content struct{ Header string; Lines []string }   // lines pre-rendered (text, code, or image cells)
  func New(opts ...Option) Model
  func (m *Model) SetSize(w, h int)
  func (m *Model) SetContent(c Content)                  // resets the scroll to the top
  func (m *Model) ScrollBy(n int)                        // clamped
  func (m Model) BodyOrigin() (x, y int)                 // cell offset of the first body line inside the pane (for sixel placement)
  func (m Model) View() string                           // header line + border + body; exactly w×h

  package permcard
  type ReplyKind string   // "once", "always", "deny"
  type Reply struct{ Kind ReplyKind; Message string }
  type ReplyFunc func(requestID string, r Reply) tea.Cmd
  type Request struct{ ID, Tool, Subject, Subagent string }  // Subagent "" for the root agent
  type KeyMap struct{ Allow, Always, Deny, DenyMsg, Send, Cancel key.Binding }  // a A d D enter esc
  func New(reply ReplyFunc, opts ...Option) Model
  func (m *Model) SetWidth(w int)
  func (m *Model) Set(req *Request)                    // nil clears
  func (m Model) Request() *Request
  func (m Model) Typing() bool                         // the D input is open
  func (m Model) Version() int                         // +1 on every visible change
  func (m Model) Update(msg tea.Msg) (Model, tea.Cmd)
  func (m Model) View() string                         // the card lines; "" when there is no request
  ```
  **Card copy (spec §7.4):**
  - Line 1: `⚠ <tool> wants to run:  <subject>`, or `⚠ <subagent> (subagent) wants to run <tool>: <subject>`. The subject is truncated to the width with `…`.
  - Line 2: `  a allow · A always (this exact command) · d deny · D deny with message`.
  - `D` opens a one-line textinput; `enter` sends `Reply{deny, text}`; `esc` closes the input.

- [ ] **Step 1: Write the failing tests.**
  - details: `TestDetails_ScrollClamp`; golden `details_code`.
  - permcard: `TestPermcard_KeysReply` (each of a/A/d yields the reply via the fake `ReplyFunc` with the request ID); `TestPermcard_DenyWithMessage`; `TestPermcard_SubagentCopy`; `TestPermcard_VersionBumps`; goldens `card_root`, `card_typing`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles): details pane and permission card"`

### Task 11: `bubbles/statusbar`, `bubbles/sidebar`, `bubbles/confirm`

**Model:** sonnet

**Files:**
- Create: `internal/bubbles/statusbar/statusbar.go`, `internal/bubbles/sidebar/sidebar.go`, `internal/bubbles/confirm/confirm.go`
- Modify: `internal/ui/theme/widgets.go`
- Test: one `_test.go` per package (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package statusbar
  type State struct {
      Mode        string          // "INSERT", "NORMAL", "PICKER"
      Agent, Model string
      Running     bool
      Elapsed     time.Duration
      Frame       int             // spinner frame index
      CtxUsed, CtxLimit int64
      CostUSD     float64
      Pending     int
      Queued, Untrusted bool
      Hint        string          // e.g. "⚠ permission pending · esc gp"
  }
  func New(opts ...Option) Model
  func (m *Model) SetWidth(w int)
  func (m *Model) Set(s State)
  func (m Model) View() string
  // Layout, left to right:
  //   [MODE] agent · model  ⠋ running 12s | idle  ctx 12k/200k · $0.42  ⚠ N ⏳ untrusted  <Hint> … ctrl+p (dim, right-aligned).
  // Truncated from the middle segments first when narrow.

  package sidebar
  type Tone int   // Normal, Muted, Accent, Success, Warning, Error
  type Row struct{ Icon, Text string; Tone Tone; Gauge *Gauge }
  type Gauge struct{ Used, Limit int64 }
  type Section struct{ Title string; Rows []Row }        // an empty section is not drawn
  func New(opts ...Option) Model
  func (m *Model) SetSize(w, h int)
  func (m *Model) SetSections(s []Section)
  func (m Model) View() string                           // clips to h lines

  package confirm
  type Choice struct{ Key, Label string }
  type ChosenMsg struct{ Key string }
  func New(opts ...Option) Model
  func (m *Model) Set(title string, lines []string, choices []Choice)
  func (m *Model) SetSize(termW, termH int)
  func (m Model) Update(msg tea.Msg) (Model, tea.Cmd)    // a key equal to a Choice.Key → ChosenMsg; "esc" matches a Choice with Key "esc"
  func (m Model) View() string                           // a centered box; lines scroll with j/k when they overflow
  ```

- [ ] **Step 1: Write the failing tests.**
  - statusbar: `TestStatus_SegmentsAndTruncation` at 120 and 60 cols; goldens `status_idle`, `status_running`.
  - sidebar: `TestSidebar_HidesEmptySections`; `TestSidebar_Gauge` (50% fill); golden `sidebar_full`.
  - confirm: `TestConfirm_Keys`; golden `confirm_trust`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(bubbles): status bar, sidebar, confirm dialog"`

### Task 12: `ui/actions` — catalogue, default bindings, keymap resolution

**Model:** sonnet

**Files:**
- Create: `internal/ui/actions/{actions.go,keymap.go}`
- Modify: `internal/core/ext/ext.go` (doc comment: `Command` is a picker action, never a slash command), `internal/app/registry.go` (two new steps: `addCommands`, whose slice is empty for now and is the place for future `ext.Command`s, and `addKeybinds`, which registers `actions.DefaultBindings()`)
- Test: `internal/ui/actions/{actions_test.go,keymap_test.go}`, `internal/app/registry_test.go`

**Interfaces:**
- Produces:
  ```go
  package actions
  type ID string
  const (SessionNew ID = "session.new"; SessionOpen = "session.open"; SessionRename = "session.rename";
         SessionCompact = "session.compact"; AgentSwitch = "agent.switch"; ModelSwitch = "model.switch";
         PromptAttach = "prompt.attach"; PromptEditor = "prompt.editor"; TranscriptSearch = "transcript.search";
         TranscriptYank = "transcript.yank"; TranscriptDetails = "transcript.details"; RunCancel = "run.cancel";
         ViewSidebar = "view.sidebar"; ViewTheme = "view.theme"; HelpKeys = "help.keys"; AppQuit = "app.quit";
         PickerOpen = "picker.open")
  type Action struct{ ID ID; Title, Group string; Drill bool; Command ext.Command }   // Command set for ext.<name>
  type Catalogue struct{ /* ordered actions + index */ }
  func NewCatalogue(cmds []ext.Command) *Catalogue     // builtins (spec order, grouped Session/Agent & model/Prompt/Transcript/View/App) + "ext.<name>" (group "Extensions")
  func (c *Catalogue) All() []Action
  func (c *Catalogue) Get(id ID) (Action, bool)
  func DefaultBindings() []ext.Keybind                  // R21
  type Keymap struct{ /* mode → key → ID */ }
  // Resolve applies binds (registration order; later wins), then config
  // entries "<mode>.<key>" = "<action id>". An unknown mode or action is
  // skipped with a warning: `warning: keybinds."<k>": unknown action "<v>"` / `unknown mode "<m>"`.
  func Resolve(binds []ext.Keybind, config map[string]string, c *Catalogue) (Keymap, []string)
  func (k Keymap) Lookup(mode, key string) (ID, bool)
  func (k Keymap) Keys(mode string, id ID) []string     // sorted, for the help list
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestCatalogue_BuiltinsAndExt`.
  - `TestResolve_DefaultsAndOverride`: config `"normal.ctrl+b" = "view.sidebar"` binds it, and `"insert.ctrl+x" = "prompt.editor"` adds a key.
  - `TestResolve_Warnings` (exact strings).
  - `TestRegistry_RegistersDefaultKeybinds` (app).
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.**
  - Add a section "Adding a picker action": an `ext.Command` registered in a new `addCommands` step becomes `ext.<name>`; a key is an `ext.Keybind`.
  - Restate the rule: no slash commands.
- [ ] **Step 6: Commit.** `git commit -am "feat(ui/actions): action catalogue, default keybinds, config remapping"`

### Task 13: Block rendering and the tool one-line formatters

**Model:** sonnet

**Files:**
- Create: `internal/ui/render.go`, `internal/ui/toolline.go`
- Test: `internal/ui/toolline_test.go`, `internal/ui/render_test.go` (+ goldens)

**Interfaces:**
- Consumes: `transcript.Block`; `mdrender`; `blocklist.Item`/`Styles`; `theme.Set`; `ansi.Sanitize`/`SanitizeLine`.
- Produces:
  ```go
  package ui
  // blockData is blocklist.Item.Data. The App bumps the Item.Version whenever
  // any field changes (R22: Duration; spinner Frame; Card from permcard.View()).
  type blockData struct {
      Block    transcript.Block
      Card     string
      Duration time.Duration
      Frame    int
  }
  type renderer struct{ /* md *mdrender.Renderer; set *theme.Set */ }
  func newRenderer(set *theme.Set) *renderer
  func (r *renderer) render(it blocklist.Item, width int, st blocklist.Styles) []string   // the blocklist.RenderFunc
  // toolLine formats a Tool block per spec §5.3 (no styling; render applies colors).
  func toolLine(b transcript.Block, dur time.Duration) (icon, name, summary string)
  ```
  **Formatter rules (spec §5.3; `icon` is `▸`, except `🌐` for agent-browser):**
  - `read`: `<path> · N lines` (N = output lines matching `^\d+: `); for an image, `<path> · image WxH`, parsed from `image WxH (`.
  - `write`: `<path> +N`, where N = the content's line count.
  - `edit`: `<path> +A -D`, where A/D = the line counts of `new_string`/`old_string`.
  - `bash`: the command's first line, truncated to 60 runes with `…`, then `✓ exit 0 · 1.2s`. Exit N comes from a trailing `[exit code N]` → `✗ exit N`. `[timed out after Ns]` → `✗ timed out`. The duration part is omitted when `dur == 0`.
  - `bash` whose command starts with `agent-browser `: name `🌐`, summary = the subcommand and its args, with global flags (`--session X` and other `--flag value` pairs before the subcommand) removed. For example: `open localhost:3000`, `click @e2`, `screenshot`.
  - `glob`/`grep`: `"<pattern>" · N matches` (`0` for `no matches`; the `[truncated at 100 results]` line is not counted).
  - `todo`: `N items (M in progress)`.
  - `skill`: `<id>`.
  - unknown: `<name>  <compact input JSON, first 60 runes>`.
  - States add a suffix icon and color:
    - running: spinner frame from `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`;
    - ok: the tool color;
    - error: `✗`, red;
    - denied: `⊘`, red;
    - cancelled: `⊘`, dim;
    - awaiting: `⚠`, yellow.

  **Other kinds:**
  - `User`: `› you  <text>` wrapped (+ R26).
  - `Text`: `md.Render(Sanitize(text), width)`.
  - `Reasoning`: `∴ thinking · N words`.
  - `Subagent`: `↳ <agent>  <description>  <spinner> N tools · <current>`, or `✓`/`✗` when done.
  - `Notice`: dim, or red for `LevelError`.
  - `Card` lines are appended under the block.
  - Every string from a Block goes through `SanitizeLine` (one-liners) or `Sanitize` (bodies).

- [ ] **Step 1: Write the failing tests.**
  - `TestToolLine_Table`: one row per rule above, including agent-browser with `--session s1 open x`, a bash with exit 3, a timeout, a read of an image, and an unknown tool.
  - `TestRender_SanitizesHostileOutput`: a Text block containing `"\x1b]52;c;aGk=\x07hi"` and a bash command containing `\x1b[2J` → rendered lines contain no `]52;` and no `\x1b[2J`, and do contain `hi`.
  - `TestRender_Golden`: goldens `render_user`, `render_tools` (all states), `render_subagent`, `render_notice`, each at width 80 with the "dark" palette.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui): block rendering and tool one-line formatters"`

### Task 14: Details content builder

**Model:** sonnet

**Files:**
- Create: `internal/ui/details.go`, `internal/ui/cmds.go` (first port-wrapping commands)
- Test: `internal/ui/details_test.go`

**Interfaces:**
- Produces:
  ```go
  package ui
  type detailsMsg struct{ Block transcript.BlockID; Content details.Content; Image *imgrender.Result }
  // buildDetails returns immediate content, plus a Cmd for anything needing a port
  // (file context, blob bytes, child messages) that yields a detailsMsg.
  func buildDetails(b transcript.Block, width, height int, r *renderer, p Ports, img *imgrender.Renderer) (details.Content, tea.Cmd)
  ```
  **Content per kind (spec §5.4). Header: `<kind> · <subject> · <extra>`:**
  - `edit`: immediately, a bare diff. The Cmd then does `Project.ReadFile(path)` and rebuilds it with context (R23). Header `edit · <base> · N hunks`.
  - `write`: highlighted content; `write · <base> · N lines`.
  - `read`:
    - text → the numbered lines, with the code portion highlighted;
    - image → Cmd `Blobs.Open(ref)` → `imgrender.Decode` → `img.Render(ref, width, height-2)`, returned as `Image`.
  - `bash`: `$ <command>`, a blank line, the sanitized output, `exit N`, and `spill: <path>` when the output starts `[output truncated; full output: <path>]`. An agent-browser screenshot shows the image of `Result.Media[0]` like `read`.
  - `grep`/`glob`: the result lines.
  - `Subagent`: Cmd `Sessions.Messages(child)` → the child's tool one-liners (`toolLine`) then its final text. The App re-requests while the block is running and details are open (Task 18).
  - `Reasoning`, `User`, `Text`: the full text (`Text` via markdown).

- [ ] **Step 1: Write the failing tests** (fake ports).
  - `TestDetails_EditWithContext`: the file contains `new_string` once → a 3-line-context diff whose header names `1 hunks`.
  - `TestDetails_EditFileUnreadableFallsBack`.
  - `TestDetails_ReadImageCmd`: a fake blob with PNG bytes → `detailsMsg.Image` has Rows > 0 (Blocks protocol).
  - `TestDetails_BashSpillPath`.
  - `TestDetails_SubagentSummary`.
  - `TestDetails_SanitizesFileContent`.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui): details content for every block kind"`

### Task 15: App core — bridge, layout, INSERT, send/queue/cancel, streaming

**Model:** opus (App/reconciliation)

**Files:**
- Create: `internal/ui/{app.go,bridge.go,layout.go,session.go,mode_insert.go,apptest_test.go}` (extend `cmds.go`)
- Test: `internal/ui/{app_test.go,bridge_test.go,layout_test.go,insert_test.go}` (+ goldens)

**Interfaces:**
- Produces:
  ```go
  package ui
  type Ports struct {
      Chat core.ChatService; Sessions core.SessionService; Perms core.PermissionService
      Catalog core.CatalogService; Agents core.AgentService; Project core.ProjectService
      Blobs core.BlobService; Prefs core.PrefsService; Editor core.EditorService
      Subscribe func() *event.Subscription
  }
  type Options struct {
      Session    core.SessionID      // resume; "" = new
      WorkDir, ProjectKey string
      Untrusted  bool
      Actions    *actions.Catalogue
      Keymap     actions.Keymap
      Themes     []theme.Palette     // custom palettes
      Theme      string
      Images     imgrender.Env
      Tmux       bool
      Aliases    map[string]string   // for alias display in the status bar
      Clock      clock.Clock
  }
  func New(p Ports, o Options) *App            // implements tea.Model
  // bridge.go
  type eventMsg struct{ ev event.Event }
  func waitEvent(sub *event.Subscription) tea.Cmd   // yields eventMsg, or nil when closed
  // layout.go
  type rects struct{ Transcript, Side, Prompt, Status wintree.Rect; Narrow, SideVisible, DetailsOpen bool }
  func computeLayout(w, h, promptH int, sidebarPref *bool, detailsOpen bool) rects
  // apptest_test.go
  func newTestApp(t *testing.T, opts ...testOpt) *testApp   // fake ports, a fake clock, a fixed size, and Update helpers that run returned Cmds synchronously (skipping ticks and waitEvent)
  ```
  **App fields (≤ 15):**
  - `ports`, `opts`;
  - `w` (a `widgets` struct: list, prompt, picker, details, card, status, side, confirm);
  - `sess *sessionState`;
  - `lay rects`;
  - `mode`;
  - `theme *themeState` (set, version, palettes, preview-restore);
  - `img *imgrender.Renderer`;
  - `sub`;
  - `keyPrefix`;
  - `width`, `height`.

  Keep the `App` method count ≤ 20 by putting per-mode handlers on `sessionState` / `themeState` / small handler types in their own files.

  `sessionState` (reconciliation) owns:
  - the `transcript.Projection`;
  - `core.Session`;
  - agent and model;
  - run state (`running`, `startedAt`);
  - `queued`;
  - the last `StepFinished` usage;
  - the summed cost;
  - todos;
  - tool durations;
  - item versions;
  - dirty streaming items.

  **Rules:**
  - **Layout.** Sidebar visible iff width ≥ 120 and (`sidebarPref == nil`, or `*sidebarPref`). Its width is `clamp(32% w, 30, 50)`. The details split takes the side slot at 50% width; when narrow (< 120) it takes the full width of the transcript region. Prompt height = `prompt.Height()`, and the status bar is 1 row. The layout is built with `wintree` (`SetFixed`).
  - **New session adoption.** With `Options.Session == ""`, the first `SessionCreated` whose `RootID == SessionID`, arriving while a send is in flight, becomes the root.
  - **Send.** `SubmitMsg` while idle:
    - a `Chat.Send` Cmd, with Agent/Model on a new session;
    - reset the prompt;
    - append to history (prefs, ≤ 100 per `ProjectKey`).

    While running: `queued = true`, `prompt.SetQueued(true)`, and the text stays in the prompt. On `RunFinished`/`RunFailed` for the root, a queued text is sent.
  - **Streaming.** Deltas mark items dirty. `streamTick` (`tea.Tick(80ms)`) runs only while running: it `Upsert`s dirty items and advances the spinner frame. On `StepFinished` for a message, that message's text blocks get a final render.
  - **`ctrl+c`** (INSERT and NORMAL), in order:
    1. queued → clear the queue, then `Chat.Cancel(root)`;
    2. running → `Chat.Cancel`;
    3. a non-empty prompt → clear it;
    4. otherwise → quit.
  - **`ctrl+d`** on an empty prompt → quit. `tab`/`shift+tab` cycle `Agents.Primary()`; with a session, a `Sessions.Configure` Cmd. `esc` → NORMAL.
  - **Status bar.** Rebuilt after every message from `sessionState`. `CtxUsed` = last step `Usage.Input + CacheRead`; `CtxLimit` = the catalog `ContextWindow` of the current model. The model name shows its alias when one maps to it.
  - **View.** `tea.View{Content: …, AltScreen: true}`. Regions are joined with lipgloss, and overlays (picker, confirm) are composited with `overlay.Center`. A resume `Init`:
    - calls `Sessions.Get` + `Messages` + `Todos` and `projection.Load`;
    - returns `tea.RequestTerminalVersion` (via `tea.Batch`) and `waitEvent`.

- [ ] **Step 1: Write the failing tests.**
  - `TestBridge_DeliversAndStopsOnClose`.
  - `TestLayout_Table`: 200×50 (sidebar 50), 150×40 (48), 120×30 (38), 119×30 (none), with details open at 150 (75/75) and at 100 (narrow full width), and a sidebar pref false at 200 (none).
  - `TestApp_TinySizesDoNotPanic`: sizes {80×24, 40×10, 20×5, 1×1, 0×0}. `View` has no line wider than w, and no more than h lines.
  - `TestApp_SendNewSessionAdoptsRoot`.
  - `TestApp_QueueSendsAfterRun`.
  - `TestApp_CtrlCLadder`: all four rungs, and "queued + running" → the queue cleared and Cancel called in one press; after `RunFailed`, nothing is sent.
  - `TestApp_StreamingCoalescesTo80ms`: 10 deltas then one tick → exactly one `Upsert` (count via a white-box hook).
  - `TestApp_TabCyclesAgentsAndConfigures`.
  - `TestApp_StatusContextAndCost`.
  - goldens `app_idle` (120×30) and `app_streaming`.
- [ ] **Step 2: Run** `go test ./internal/ui/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS (archtest includes the App size).
- [ ] **Step 5: `AGENTS.md`.** Add an Invariant: "`ui.App` is the only owner of widget state; widgets communicate by returning Cmds that yield their own message types; the App reconciles."
- [ ] **Step 6: Commit.** `git commit -m "feat(ui): App core — bus bridge, layout, insert mode, send queue, streaming"`

### Task 16: NORMAL mode — navigation, details split, search, yank

**Model:** sonnet

**Files:**
- Create: `internal/ui/mode_normal.go`, `internal/ui/yank.go`
- Test: `internal/ui/normal_test.go` (+ goldens)

**Interfaces:**
- Consumes: Tasks 7, 10, 12, 14, 15.
- Produces: no exported names. Behavior per spec §6.3 and R21:
  - `j`/`k`/`G`/`ctrl+d`/`ctrl+u`/`n`/`N` go to the blocklist.
  - `g` sets `keyPrefix`: `gg` → `Top()`; `gp` → Task 18.
  - `enter` (`transcript.details`) toggles the split for the selected block. `buildDetails` runs, and a `detailsMsg` for the still-selected block replaces the content; one for another block is ignored.
  - `ctrl+e`/`ctrl+y` → `details.ScrollBy(±1)`.
  - `q`/`esc` close the split, else clear the search.
  - `/` opens a one-line search input (`bubbles/v2/textinput`, owned by the App) in place of the status bar. `enter` applies `list.SetSearch`; `esc` cancels.
  - `y` (`transcript.yank`) → `tea.SetClipboard(yankText(block))`, and the status hint `yanked`. `yankText` returns:
    - Text/User/Reasoning → the text;
    - bash → the command;
    - edit → `coderender.DiffText(old, new, 3)`;
    - read/write → the path;
    - other tools → the output;
    - Subagent → its description.
  - `i`/`a` → INSERT (except `a` on a card, Task 18).
  - `?` → the picker at the `keys` level (Task 17).
  - Moving the selection while the details are open rebuilds the details for the new block.

- [ ] **Step 1: Write the failing tests.**
  - `TestNormal_GGAndG`.
  - `TestNormal_DetailsToggleAndFollowsSelection`.
  - `TestNormal_StaleDetailsMsgIgnored`.
  - `TestNormal_SearchInputAndClear`.
  - `TestNormal_YankTable` (the Cmd yields tea's clipboard message; assert on the text via `yankText` directly plus one Cmd smoke check).
  - `TestNormal_EscClosesDetailsFirst`.
  - goldens: `app_details_open` (150×40) and `narrow_details` (100×30).
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui): NORMAL mode — block navigation, details split, search, yank"`

### Task 17: The picker in the App — actions, drill-downs, the file picker

**Model:** opus (many reconciliation paths)

**Files:**
- Create: `internal/ui/{mode_picker.go,pickerlevels.go,files.go}`
- Test: `internal/ui/{picker_test.go,files_test.go}` (+ goldens)

**Interfaces:**
- Produces: `picker.LoadFunc` = `(*App).loadLevel` (a method value wrapping port calls in Cmds). Level IDs: `root`, `sessions`, `models`, `agents`, `themes`, `rename`, `files`, `keys`.
  - **`root`** (`Actions: true`): `Catalogue.All()`. Titles are the action titles, Detail the bound keys (`Keymap.Keys`). `session.open`, `model.switch`, `agent.switch`, `view.theme`, `prompt.attach`, and `help.keys` drill; `session.rename` drills into an `Input` level with `Initial` = the title.
  - **`sessions`**: `Sessions.ListForCwd(WorkDir, 30)`. Detail = relative age from `Options.Clock` (`just now`, `5m ago`, `3h ago`, `2d ago`) · `$0.42`, where cost = the sum of `Messages` `CostUSD`, loaded in the same Cmd. `Current` = the open session. Choose → `resume`: close the details, `projection.Load`, and set the agent and model.
  - **`models`**: `Catalog.Providers()`, grouped by provider name. Detail = `200k ctx · $3/$15`; `Disabled` = !Configured; `Current` = the current model. Choose → set the model, plus `Sessions.Configure` when a session exists.
  - **`agents`**: `Agents.Primary()`. Choose → like tab.
  - **`themes`**: the builtin palettes plus custom ones. `WithPreview` applies the palette temporarily (`themeState` remembers the original). `ClosedMsg` on `themes` restores it. Choose → keep it, and `Prefs.Update` setting `Theme`.
  - **`rename`**: `InputMsg` → a `Sessions.Rename` Cmd.
  - **`files`** (`Multi: true`): `Project.Files()`, ranked:
    1. paths the session touched (a read, or in `ChangedFiles`), in last-touched order;
    2. `Modified`;
    3. the rest, in port order.

    Choose → `prompt.Insert("@<path> ")` for each item. The App records each inserted path in `sessionState.attach`.
  - **`keys`**: every action with its keys, `Disabled: true` (a read-only list; filterable).
  - **Other actions:**
    - `session.new`: clear the projection and session; the next send creates one.
    - `session.compact`: a `Chat.Compact` Cmd. `core.ErrBusy` → the hint `session is busy`; `ErrNothingToCompact` text → the hint `nothing to compact`.
    - `run.cancel`, `view.sidebar` (toggle + `Prefs.Update`), `prompt.editor`, `app.quit`, and the `transcript.*` actions reuse Task 15/16 paths.
    - `ext.<name>` → a Cmd running `Command.Run(ctx, nil)`; an error becomes a status hint.
    - Every executed action is pushed onto `Prefs.Recent` (deduped, ≤ 5) and saved.
  - **`@` in INSERT (`MentionMsg`)** opens the picker directly at `files`. `ClosedMsg` from a `files` level opened this way inserts a literal `@`.
  - **On send**, `SendRequest.Attachments` = the recorded paths whose `@<path>` token still occurs in the text (R19). They are joined to `WorkDir` by chat.

- [ ] **Step 1: Write the failing tests.**
  - `TestPicker_RootListsActionsWithKeysAndRecent`.
  - `TestPicker_ModelSwitchConfigures`.
  - `TestPicker_DisabledProviderNotChosen`.
  - `TestPicker_SessionsOpenResumes`.
  - `TestApp_ThemePreviewEscRestores`: preview "nord", then esc → the palette name and the version-bumped styles equal the original's.
  - `TestPicker_RenameCallsPort`.
  - `TestFiles_RankingTouchedModifiedRest`.
  - `TestFiles_AtEscInsertsLiteral`.
  - `TestSend_AttachmentsOnlyForSurvivingTokens`.
  - `TestPicker_RecentSaved`.
  - `TestPicker_CompactBusyHint`.
  - goldens: `picker_root`, `picker_models`, `picker_files` (120×30, overlaid).
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui): picker actions, drill-down levels, @ file picker"`

### Task 18: Permissions, sidebar, images in details, theme pushes

**Model:** opus (App/reconciliation)

**Files:**
- Create: `internal/ui/{permissions.go,sidebar.go,themestate.go,images.go}`
- Test: `internal/ui/{permissions_test.go,sidebar_test.go,images_test.go}` (+ goldens)

**Interfaces:**
- Produces: no exported names. Behavior:
  - **Permission card.**
    - On `PermissionRequested` (after `projection.Apply`), the card shows the first entry of `projection.Pending()`, or the one whose block is selected: `card.Set(&permcard.Request{…})`.
    - The owning block's item carries `Card: card.View()` and its version bumps on `card.Version()` changes.
    - The card's `ReplyFunc` → a `Perms.Reply` Cmd, mapping `permcard.Reply` → `core.PermissionReply`.
    - When the last pending request resolves, the card clears.
  - **Focus rule (spec §6.5).** With an empty prompt → NORMAL, and select the card's block (a descendant request selects the owning Subagent block). With a non-empty prompt, stay put, and the status `Hint` = `⚠ permission pending · esc gp`.
  - **NORMAL on a block with a card:** `a`, `A`, `d`, and `D` (and typing plus `enter`/`esc` while `card.Typing()`) go to the card; `i` still → INSERT. `gp` selects the next pending block (wrapping). The status bar's `Pending` = `len(Pending())`.
  - **Sidebar sections**, rebuilt on any change, with rows sanitized:
    - Session: title, gauge (ctx used/limit), `$cost`, `agent · model`.
    - Todos (✓ done, ● in progress, ○ pending), from `TodosUpdated` for the root plus the initial `Sessions.Todos`.
    - Files: `A`/`M` + path, from `ChangedFiles`.
    - Subagents: the agent, description, and state icon of each Subagent block.
    - Browser: `LastBrowserURL()` and the session name, from the latest root `agent-browser` command's `--session <name>`, else `default`.
  - **Subagent details** stay current: while the details show a running Subagent block, child events re-issue the `Sessions.Messages(child)` Cmd, at most once per `streamTick`.
  - **Images.** A `detailsMsg.Image` sets the details lines. Its `Upload` is sent with `tea.Raw` once. `Sixel` is placed with `tea.Raw(imgrender.Place(res, x, y))`, where `(x, y)` = the details rect origin + `details.BodyOrigin()`, and re-placed after any relayout while it is shown. The renderer's protocol is `Detect(Options.Images, "")` at `New`, then re-detected on `tea.TerminalVersionMsg`.
  - **Theme push.** `themeState.apply(p)`:
    - `version++`, `set = theme.Build(p, version)`;
    - push each widget's styles (`list.SetStyles(set.Blocklist, version)`, `md.SetStyles`, …);
    - bump every item version.

- [ ] **Step 1: Write the failing tests.**
  - `TestApp_PermissionFocusesWhenPromptEmpty`.
  - `TestApp_PermissionDoesNotStealFocusWhileTyping`: the prompt text is unchanged, the mode is INSERT, and the hint is present.
  - `TestApp_CardKeysReply`.
  - `TestApp_SubagentPermissionSelectsOwner`.
  - `TestApp_GPCyclesPending`.
  - `TestSidebar_SectionsFromState`.
  - `TestSidebar_BrowserSessionName`.
  - `TestImages_KittyUploadSentOnce`.
  - `TestImages_SixelReplacedAfterResize`.
  - `TestTheme_PushBumpsAllVersions`.
  - goldens:
    - `app_permission_card`;
    - `app_subagent_permission`;
    - `app_sidebar` (150×40);
    - `app_image_details` (the blocks protocol, a 4×4 test PNG).
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(ui): permission cards and focus, sidebar, images in details, theme pushes"`

### Task 19: `jig` launches the TUI — flags, trust dialog, themes, keybinds, prefs

**Model:** opus (wiring)

**Files:**
- Create: `internal/app/{tui.go,trustdialog.go}`
- Modify: `internal/app/{app.go,cli.go,services.go,env.go}`
- Test: `internal/app/{tui_test.go,trustdialog_test.go}`

**Interfaces:**
- Produces:
  ```go
  package app
  type tuiOpts struct{ cwd, session string; trustProject bool }
  func parseTUI(args []string, errw io.Writer) (tuiOpts, error)
  func runTUI(ctx context.Context, args []string, std Stdio, getenv func(string) string) int
  // trustDialog runs a short-lived tea program with bubbles/confirm on std:
  // title "Trust this project's config?", lines = project path, blank, one
  // Effect.String() per effect (sanitized); choices t "trust", n "continue
  // untrusted", esc. Returns true only for "t".
  func trustDialog(std Stdio) trustDecider
  ```
  **Flow:**
  1. `Run` with no args, or with leading flags → `runTUI` (R25).
  2. If `std.Out` is not a terminal (`*os.File` with `ModeCharDevice`) → the R25 message, exit 2.
  3. `loadEnv(cwd, getenv, decider)`, where the decider is `trustDialog(std)`, or always-true with `--trust-project`.
  4. Resolve the keymap from `view.Keybinds()` + `cfg.Keybinds`, and print each warning to stderr before start.
  5. Custom themes: `themefs.Load(ConfigDir/themes)` → `theme.Custom`, with warnings to stderr. The theme is `prefs.Theme`, else `cfg.Theme`, else `dark`; an unknown name → a warning and `dark`.
  6. `newRuntime` with a `permission.BusAsker`, which is also `Ports.Perms`.
  7. Build `ui.Ports` from the runtime and the Task 1 adapters, and `ui.Options` (`Images` from the env, `Tmux` from `$TMUX`, `Untrusted` from the trust state).
  8. `tea.NewProgram(app, tea.WithContext(ctx), tea.WithInput(std.In), tea.WithOutput(std.Out)).Run()`.
  9. Then `chat.Close` with the 3 s timeout, and `rt.close()`.

  Exit 0 on a normal quit. A program error goes to stderr, exit 1.

- [ ] **Step 1: Write the failing tests.**
  - `TestRunTUI_NotATerminalExits2`: `std.Out` is a `bytes.Buffer` → exit 2 with the exact message.
  - `TestParseTUI_Flags`.
  - `TestTrustDialog_KeysDecide`: drive the dialog model through `Update`, not a program. `t` → true; `n` → false; `esc` → false. The rendered lines include an effect and never a secret.
  - `TestRunTUI_KeybindWarningsPrinted`: with an injected program runner that returns immediately (a `runProgram func(tea.Model) error` field in a small `tuiDeps` struct), a bad keybind prints the warning.
- [ ] **Step 2: Run** the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Keep every func ≤ 40 lines.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: `AGENTS.md`.** Update the usage block and the "Architecture in one screen" entry for `internal/app` (TUI + trust dialog). Remove the "interactive UI is not built yet" note.
- [ ] **Step 6: Commit.** `git commit -am "feat(app): jig launches the TUI; trust dialog; themes, keybinds, prefs wiring"`

### Task 20: pty smoke test and final documentation

**Model:** sonnet

**Files:**
- Create: `e2e/tui_test.go` (build tag `jigtest`)
- Modify: `go.mod` (`github.com/creack/pty` v1.1.24; used only by the test), `AGENTS.md`
- Test: `e2e/tui_test.go`

**Interfaces:**
- Consumes: the e2e harness (`newEnv`, `writeScript`, `jigtestConfig`, `jigBin`).
- Produces:
  ```go
  // screen accumulates pty output; waitFor blocks on a sync.Cond until
  // ansi.Strip(output) contains s, or ctx (context.WithTimeout 20s) ends.
  type screen struct{ /* mu, cond, buf */ }
  func (s *screen) waitFor(ctx context.Context, t *testing.T, s string)
  ```

- [ ] **Step 1: Write the failing test** `TestE2E_TUISmoke`:
  - Setup: a project config in `work/.jig/config.toml` (`[permissions]\nread = "ask"`, so the trust dialog appears). Script m1: one turn with text `hello from jig`. Config: `jigtestConfig(script, "")`.
  - Start `jig --cwd work` with `pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})`, env + `TERM=xterm-256color`, `JIG_IMAGES=off`.
  - Then, in order:
    1. `waitFor("Trust this project")` → write `t`.
    2. `waitFor("Message build")` → write `hi\r`.
    3. `waitFor("hello from jig")` → write `\x10` (ctrl+p).
    4. `waitFor("Switch model")` → write `model`, then `\r`.
    5. `waitFor("m2")` → write `m2\r`.
    6. `waitFor("jigtest/m2")` (status bar) → write `\x04` (ctrl+d).
  - `cmd.Wait()` exits 0 within the ctx.
  - If matching on stripped output proves flaky because of partial redraws, follow R27.
- [ ] **Step 2: Run** `make test`. Expected: FAIL until the flow works end to end; fix the real bugs it finds (in their owning packages, with a unit test each).
- [ ] **Step 3: Update `AGENTS.md`:**
  - the performance budgets (from Task 7);
  - the TUI section: modes, actions, and how to add a widget (conventions, `theme.Set` mapping, golden tests);
  - the shared-code rows for `golden`, `overlay.Center`, `imgrender`, and `transcript`.
- [ ] **Step 4: Run** `make check`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "test(e2e): TUI smoke test under a pseudo-terminal"`

## Finishing Plan 2b

- [ ] Run `make check`, push, and confirm the ubuntu and macOS CI jobs are both green (`gh run watch --exit-status`). macOS differs in `/tmp` symlinks and pty behavior; fix it until both pass.
- [ ] Run the final whole-branch review (opus) against the Review Focus, and ask the reviewer to probe on purpose:
  - hostile escape sequences through every render path (Text, tool output, details, sidebar, titles, trust effects, file picker paths);
  - resize storms;
  - permission races (a reply arriving after cancellation);
  - theme-switch cache staleness;
  - kitty and sixel payloads never appearing inside `View`.
- [ ] Use `superpowers:finishing-a-development-branch`. The success criterion (spec §1) is a week of daily use; hand that to Grant.


## Execution notes (after Plan 2b shipped)

Deferred follow-ups from the task and final reviews, none blocking daily use:
- Performance: resizing a 2,000-block session re-renders every block (~1 s, budget 1.5 s); a real fix needs lazy/estimated heights in blocklist. Details build synchronously and aren't cached (~190 ms for a 50 KB file). The sessions picker loads each session's full history to total its cost; live subagent details reload every tick.
- Robustness: runs are not cancelled on SIGTERM/SIGHUP (App base context from Background); trust dialog accepts typed-ahead `t`; `ctrl+c` does nothing inside the picker; paste in NORMAL is dropped; no "compacting…" feedback.
- Rendering: search-highlight off-code drops underlying colors on unselected rows; selection tint only re-applied after exact reset strings; stale details colors after a theme change; sixel not re-placed on details scroll/picker open; the theme picker re-applies an unchanged theme on esc/choose.
- Sizes: `internal/ui/app.go` is at ~495/500 lines and `viewState`/`widgets` at 15 fields; blocklist Model at 19/20 methods; transcript.Projection and sessionState at 20 — the next change there must split.
- Misc: `@path,` (trailing punctuation) isn't an attachment; "nothing to compact" is matched by error text; AGENT_BROWSER_SESSION env isn't reflected in the sidebar; project files read with unbounded `os.ReadFile` in config/agentfs/contextfs/skillfs.
