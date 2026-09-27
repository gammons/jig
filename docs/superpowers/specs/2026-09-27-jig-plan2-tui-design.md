# jig Plan 2: TUI Design

- **Status:** draft, awaiting review
- **Date:** 2026-09-27
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` (Plan 1, implemented). This spec replaces that spec's §6.3 ("TUI").
- **Related:** gammons/slk#236 (self-contained UI components). jig adopts that RFC's end state from the start.

## 1. Intent

**What the author asked for**

- A TUI good enough to replace OpenCode as a daily driver. It should keep OpenCode's strengths while rethinking the interaction model around vim.
- **No slash commands.** Every action goes through one picker opened with `ctrl+p` (with `ctrl+t` as an alias), because that muscle memory is already burned in.
- Vim-style navigation. In NORMAL mode, a cursor moves over *blocks* in the transcript, one selected line at a time, as in slk. Pressing `enter` opens the selected block's details in a vertical split.
- Tool calls are shown as one line each by default.
- A session sidebar, themes (slk's theme system), `@file` mentions through a modal fuzzy picker (like ctrlp.vim), `$EDITOR` for long prompts, markdown with code highlighting, and rich rendering of tool details.
- A trust prompt for project-level config. This settles the security question left open by Plan 1.
- **First-class web development support:** images in tool results (screenshots the model and the user can both see), and a built-in integration with `agent-browser`.

**Assumptions (not stated by the author)**

- Terminals are modern truecolor terminals (kitty, ghostty, wezterm, iTerm2, alacritty, foot). Image support degrades gracefully.
- The minimum usable size is 80×24. The sidebar needs at least 120 columns.

**Success criteria**

The author uses `jig` (the TUI) instead of OpenCode for a week of real work: superpowers workflows, subagents, permission prompts, session resume, model and agent switching, and at least one web-dev task driven through agent-browser with screenshots. A 2,000-block session must stay responsive.

## 2. Scope

**In Plan 2**

- The TUI: layout, INSERT/NORMAL/PICKER modes, the block transcript, the details split, the picker with drill-down, the prompt (with `@` and `$EDITOR`), the status bar, the sidebar, permission cards, the trust dialog, and themes.
- The service additions the TUI needs (§8), including fixes carried over from the Plan 1 review.
- Project-config trust.
- Images in tool results, image `read`, image attachments, and inline image rendering.
- The agent-browser integration: skill discovery, a permission preset, browser-aware tool lines, and a sidebar Browser section.

**Deferred (Plan 3 and later)**

- **Plan 3:** vim-style subagent panes, focus via `ctrl+w`, and zoom.
- An MCP client (its own plan, after Plan 3).
- Undo/revert of file changes, mouse support beyond wheel scrolling, session export/share, LSP, skill packs, external plugins, and automatic compaction.
- Slash commands will never exist.

## 3. Architecture

### 3.1 Packages

```
internal/bubbles/            domain-agnostic widgets (slk#236 conventions)
  ansi/ overlay/ scrollbar/ wintree/     substrate: ANSI-safe slicing, boxed overlays, scrollbars, window tree
  mdrender/                  markdown → ANSI (glamour v2), width-aware
  coderender/                code + unified diff → ANSI (chroma)
  imgrender/                 image → terminal (kitty graphics, sixel, half-block fallback); ported from slk
  blocklist/                 block-cursor list: items, selection, yOffset scrolling, per-item render cache, search highlight
  picker/                    fuzzy list, drill-down stack, groups, recents, multi-mark, live-preview callback
  prompt/                    multi-line input: @ trigger, $EDITOR request, history, paste chips, queued-send state
  details/                   scrollable read-only preview pane (text and images)
  permcard/                  inline permission card (a/A/d/D, deny-with-message input)
  statusbar/  sidebar/  confirm/
internal/ui/
  transcript/                pure projection: session events → []Block (jig-specific; no bubbletea)
  theme/                     palettes (ported from slk) → per-widget Styles; custom theme loading is done by app
  actions/                   picker action catalogue: built-ins + ext Commands; keymap resolution
  app*.go, mode_*.go         App: bus→tea.Msg bridge, mode arbitration, reconciliation, palette owner
internal/service/            additions in §8
internal/data/trustfs/, internal/data/blobfs/, internal/data/prefsfs/
internal/app/                builds the TUI (`jig` with no args) and the trust dialog
```

### 3.2 Widget conventions (slk#236)

- **Location.** Every widget in `internal/bubbles` is self-contained. It owns its `Styles` (with `DefaultStyles()`), its `KeyMap` (with `DefaultKeyMap()`), and its own size (via `SetSize`).
- **View.** `View()` is pure and takes no geometry parameters.
- **Construction.** `New(requiredDeps..., opts ...Option)`:
  - Required dependencies are **named function types that the widget declares** and that return `tea.Cmd`. Examples: `permcard.ReplyFunc`, `picker.LoadFunc`, `prompt.EditFunc`.
  - Optional configuration is passed as bubbles-style options.
  - A widget never performs I/O. It returns commands.
- **What stays in `App`.** Only *reconciliation* (keeping several widgets consistent after one event) and *mode arbitration*. If an interaction touches a single widget, it lives in that widget.
- **Theme.** There is no global theme state. `ui/theme` maps the active palette to each widget's `Styles`. `App` pushes that snapshot at construction and whenever the theme changes, and each push increments a `stylesVersion` used as a cache key.
- **Scrolling.** Scrolling uses a hand-rolled `yOffset` with per-item line offsets. `bubbles/viewport` is not used (see slk#236, Phase 3).

### 3.3 Dependency rules

These are enforced by `internal/archtest`, in addition to the Plan 1 rules.

- **`internal/bubbles/...`**
  - May import: stdlib, `charm.land/...`, chroma, `golang.org/x/image`, and the substrate packages (`ansi`, `overlay`, `scrollbar`, `wintree`).
  - May not import: other widgets, `internal/core`, `internal/service`, `internal/ui`, `internal/clock`, or `internal/ids`.
  - No struct field may have type `func(tea.Msg)`.
  - No package-level `Styles` or theme variables.
  - No `View` method may take parameters.
- **`internal/ui/transcript`** imports only stdlib and `internal/core` (including `core/event`).
- **`internal/ui`** does no I/O (the Plan 1 rule still applies). It reaches services only through `core` ports and bus events. Its widget callables are method values that wrap port calls in `tea.Cmd`s.

## 4. Layout

```
┌────────────────────────────────────────────┬──────────────────────┐
│ transcript (blocklist)                     │ sidebar  — or —      │
│                                            │ details split        │
│                                            │ (when open)          │
├────────────────────────────────────────────┴──────────────────────┤
│ prompt (1–8 lines, grows)                                          │
├────────────────────────────────────────────────────────────────────┤
│ status bar                                                         │
└────────────────────────────────────────────────────────────────────┘
```

- **Window tree.** Layout is computed by `bubbles/wintree`. In Plan 2 the tree has three leaf kinds (transcript, sidebar/details, prompt), and only the transcript and prompt can take focus. Plan 3 adds more transcript leaves.
- **Sidebar.** Shown by default when the terminal is at least 120 columns wide, and hidden below that. `ctrl+b` toggles it, and the choice is saved in prefs. It takes 32% of the width, clamped to 30–50 columns.
- **Details split.** While it is open, it takes the sidebar's slot at 50% of the width. On a narrow terminal, it takes the full width and the transcript is hidden until the split is closed.
- **Overlays.** The picker, the trust dialog and the keybinding list render as centered overlays via `bubbles/overlay`.

## 5. Transcript

### 5.1 Blocks (`ui/transcript`)

A session projects to an ordered `[]Block`. Every block has a stable `ID` and a `Version`, which increments each time the block changes.

| Kind | Content | Collapsed rendering |
|---|---|---|
| `User` | prompt text, attachment names | `› you  <text>` (wrapped in full) |
| `Text` | assistant markdown | rendered markdown (full height) |
| `Reasoning` | thinking text | `∴ thinking · N words` |
| `Tool` | call, result, state (`pending`, `awaiting-permission`, `running`, `ok`, `error`, `denied`, `cancelled`), media refs | `▸ <tool>  <summary>` (per-tool formatter, §5.3) |
| `Subagent` | child session, agent, description, state, tool count, current tool, pending permission | `↳ <agent>  <description>  ⠋ N tools · <current>` |
| `Notice` | run failed, cancelled, max steps, compaction summary | a dim line, or red for errors |

**API**

- `New(root core.SessionID) *Projection`
- `Load(msgs []core.Message)` rebuilds the blocks from stored messages. It handles interrupted runs, and it rebuilds `Subagent` blocks from `task` tool calls, whose `task_result` carries the child session id.
- `Apply(ev event.Event) []BlockID` returns the IDs of the blocks that changed.
- `Blocks() []Block`
- `ChangedFiles() []FileChange`
- `Pending() []PendingPermission`
- `LastBrowserURL() string`

**Rules**

- **Streaming.** `TextDelta` and `ReasoningDelta` append to the open block of the same kind for that message. A new block opens when the kind changes or a new `MessageStarted` arrives.
- **Tool lifecycle.** `ToolCallStarted` creates a `Tool` block in the `running` state. `ToolCallFinished` sets the result and the final state: `error` if `IsError`; `denied` when the output starts with `denied by` or `user denied`; `cancelled` when the output is `cancelled`.
- **Descendants.** A session is a descendant if its `RootID` equals the projection's root and its `SessionID` differs. Descendant events update the owning `Subagent` block, found via `SubagentSpawned.Child` and walked up through nested spawns. A descendant's own tool calls never create blocks.
- **Permissions.**
  - A `PermissionRequested` for the root session sets the target `Tool` block to `awaiting-permission` and attaches the request.
  - One for a descendant sets `pending permission` on the owning `Subagent` block.
  - `PermissionResolved` clears either.
- **Changed files.** Successful `write` and `edit` results contribute `FileChange{Path, Kind: added|modified}`.

### 5.2 Rendering (`bubbles/blocklist`)

- **Renderer callback.** `blocklist.New(render RenderFunc)`, where `RenderFunc(item Item, width int, st Styles) []string` is supplied by `ui` and dispatches on block kind to `mdrender`, `coderender`, or the one-line formatters.
- **Cache.** Rendered lines are cached per item, keyed by `(ID, Version, width, stylesVersion)`. Only items within 2 screen heights of the viewport stay cached; the rest are evicted and re-rendered on demand.
- **Streaming markdown.** A `Text` block that is still receiving deltas re-renders at most every 80 ms. A `streamTick` message drives this and runs only while a run is in progress. When the step ends, the block gets a final full render.
- **Scrolling.** Scrolling uses `yOffset` plus per-item line offsets. If the cursor is on the last block, the view stays pinned to the bottom as new content arrives. Otherwise the view stays where it is.
- **Selection.** The selected block is drawn with a left bar in the accent color and a subtle background. Search matches are highlighted in an ANSI-safe way.

### 5.3 Tool one-line formatters

| Tool | Format |
|---|---|
| `read` | `▸ read  <path> · N lines`, or `· image WxH` |
| `write` | `▸ write  <path> +N` |
| `edit` | `▸ edit  <path> +A -D` (counted from `old_string`/`new_string`) |
| `bash` | `▸ bash  <command, first line, truncated> ✓ exit 0 · 1.2s`, or `✗ exit N` |
| `bash` running `agent-browser …` | `🌐 <subcommand> <args>`, e.g. `🌐 open localhost:3000`, `🌐 click @e2`, `🌐 screenshot` |
| `glob` / `grep` | `▸ grep  "<pattern>" · N matches` |
| `todo` | `▸ todo  3 items (1 in progress)` |
| `skill` | `▸ skill  <id>` |
| `task` | rendered as a `Subagent` block instead |
| unknown / plugin | `▸ <name>  <input JSON, first 60 runes>` |

States are shown with an icon and a color: running `⠋` (animated spinner), ok (tool color), error `✗` (red), denied `⊘` (red), cancelled `⊘` (dim), awaiting permission `⚠` (yellow).

### 5.4 Details content

Details are built on demand, and only for the selected block.

- **`edit`:** a unified diff of `old_string` against `new_string`, with 3 lines of context when the file is readable (it is read through a `ui` port), syntax-highlighted by extension.
- **`write`:** the content, highlighted.
- **`read`:** the returned text, highlighted, or the image.
- **`bash`:** the command, then the output, the exit code, and the spill path when the output was truncated. For `agent-browser screenshot`, the image at the path named in the output.
- **`grep` / `glob`:** the match list.
- **`Subagent`:** a live summary of the child session, loaded with `SessionService.Messages(child)` inside a Cmd and then kept current from events. It shows the child's tool lines (in the one-line format) and its final text.
- **`Reasoning`, `User`, `Text`:** the full text.
- **Header:** `<kind> · <subject> · <extra>` (for example `edit · store_test.go · 2 hunks`).
- **Images:** rendered through `imgrender`, scaled to fit the pane.

## 6. Modes, keys, and actions

### 6.1 Actions

Every action has a stable ID (`session.new`, `session.open`, `session.rename`, `session.compact`, `agent.switch`, `model.switch`, `prompt.attach`, `prompt.editor`, `transcript.search`, `transcript.yank`, `transcript.details`, `run.cancel`, `view.sidebar`, `view.theme`, `help.keys`, `app.quit`). The picker lists actions by name and group. Keys bind to action IDs. `ext.Command` entries from the registry become actions (with the ID `ext.<name>`), and `ext.Keybind` entries bind keys. The default keymap is registered through the same path.

**Remapping:** `[keybinds]` in config, for example `"normal.ctrl+b" = "view.sidebar"` or `"insert.ctrl+x" = "prompt.editor"`. A binding with an unknown action or mode prints a startup warning and is ignored.

### 6.2 INSERT (default at startup; stays in INSERT after sending)

| Key | Action |
|---|---|
| `enter` | send. While a run is in progress, the message is queued and sent when the run finishes |
| `shift+enter`, `alt+enter` | newline |
| `esc` | switch to NORMAL |
| `tab` / `shift+tab` | cycle primary agents |
| `@` | open the file picker (§7.2) |
| `ctrl+e` | edit the prompt in `$VISUAL`/`$EDITOR` |
| `↑` / `↓` on the first/last line | prompt history (per project) |
| `ctrl+c` | running → cancel the run. Otherwise, a non-empty prompt → clear it. Otherwise → quit. A queued send is removed before the run is cancelled |
| `ctrl+d` | empty prompt → quit |
| `ctrl+p`, `ctrl+t` | open the picker |
| `ctrl+b` | toggle the sidebar |

### 6.3 NORMAL

| Key | Action |
|---|---|
| `j` / `k`, `gg` / `G` | next/previous block, first/last block |
| `ctrl+d` / `ctrl+u` | half page down/up |
| `enter` | toggle the details split |
| `ctrl+e` / `ctrl+y` | scroll the details split by one line |
| `q`, `esc` | close the details split, or clear the search |
| `/`, `n` / `N` | search blocks, next/previous match |
| `y` | yank the block: text, command, diff, or file path (via OSC 52) |
| `i`, `a` | switch to INSERT. On a block with a pending permission card, `a` answers the card instead (§7.4) |
| `A`, `d`, `D` | on a pending permission card only (§7.4) |
| `gp` | jump to the next pending permission |
| `?` | keybindings (a searchable list in the picker chrome) |
| `ctrl+c` | cancel the run while one is in progress |
| `tab`, `ctrl+p`/`ctrl+t`, `ctrl+b` | same as INSERT |

### 6.4 PICKER

Typing filters the list. `↑`/`↓` and `ctrl+n`/`ctrl+p` move the selection. `enter` selects or drills in. `tab` marks an item, in multi-select lists only. `⌫` on an empty query goes back one level. `esc` closes the picker.

### 6.5 Permission focus

When a `PermissionRequested` event arrives:

- **Prompt empty:** switch to NORMAL and select the block that has the card.
- **Prompt not empty:** do not steal focus. The status bar shows `⚠ permission pending · esc gp`.

## 7. Widgets

### 7.1 `picker`

- `picker.New(load LoadFunc, opts...)`.
  - `LoadFunc(level Level) tea.Cmd` returns the items for a drill-down level. It is used for "Open session…", "Switch model…", files, and similar lists, and it runs asynchronously (items arrive as a message).
- **Items:** `{ID, Title, Detail, Group, Disabled, Current}`. `Current` shows as ● and `Disabled` is dimmed but still visible (for example, a provider with no credentials).
- **Matching:** fuzzy, ranked by match score. Ties are broken by recent use (actions only) and then by original order. Matched characters are highlighted.
- **Root level:** a "Recent" group (the last 5 actions used, from prefs), followed by the action groups.
- **Levels for drill-down actions:**
  - **sessions:** those for this project (`ListForCwd`), showing title, relative age, and cost.
  - **models:** grouped by provider, showing credential status, context size, and price.
  - **agents:** primary agents only.
  - **themes:** all themes, with a live preview callback as the selection moves. `esc` restores the previous theme.
  - **files:** see §7.2.
  - **rename:** a single-line text input.
- **Sizing:** width `min(80, 70% of terminal)`, height up to 60%.

### 7.2 File picker (`@`)

- **Opening.** Typing `@` in INSERT opens the same `picker` at its files level, as a centered modal (like ctrlp.vim).
- **Source.** `ProjectService.Files(ctx)`, which lists files with `rg --files` (falling back to a Go walker) and respects `.gitignore`.
- **Ranking.** Files the session has touched come first (read or changed), then git-modified files, then everything else.
- **Selection.** `enter` inserts `@<relative path> ` at the cursor. `tab` marks several files and `enter` inserts them all. `esc` inserts a literal `@`.
- **On send.** The `@path` tokens stay in the text, and the resolved paths go in `SendRequest.Attachments` (§8.1).

### 7.3 `prompt`

- A multi-line textarea (bubbles v2) that grows to 8 lines, then scrolls.
- **Placeholder:** `Message <agent>…  (ctrl+p actions · @ files)`.
- **Paste chips.** A paste longer than 500 characters shows as a `[pasted N chars]` chip. The full text is sent, and `backspace` on the chip removes the whole paste.
- **External editor.** `ctrl+e` issues `EditFunc(text) tea.Cmd`. `ui` implements it with `tea.ExecProcess` on a temp file, and the edited text is loaded back.
- **Queued send.** When a send is queued, the border shows `⏳ queued` and the text is held until the run finishes.
- **History.** Stored per project in prefs, with the last 100 entries.

### 7.4 `permcard`

The card renders under its block:

```
⚠ bash wants to run:  git push origin main
  a allow · A always · d deny · D deny with message
```

- `permcard.New(reply ReplyFunc)`, where `ReplyFunc(requestID string, r Reply) tea.Cmd`, and `ui` maps it to `PermissionService.Reply`.
- `D` opens a one-line input inside the card; `enter` sends the message.
- **Subagent requests** read `⚠ general (subagent) wants to run bash: …`.
- **"Always"** grants the exact subject for the root session, which is the Plan 1 semantics. The card labels this: `A always (this exact command)`.

### 7.5 `statusbar`

One line, left to right:

- mode badge;
- agent · model (alias if one is defined, else a short model ID);
- run state (`⠋ running 12s` or `idle`);
- context used/limit (from the latest `StepFinished` input tokens against the model's context window) and session cost;
- indicators: `⚠ N` pending permissions, `⏳` queued send, `untrusted` project;
- a dim `ctrl+p` hint on the right.

### 7.6 `sidebar`

Sections are shown only when they have content:

- **Session:** title, a context gauge, cost, agent/model.
- **Todos:** `✓` done, `●` in progress, `○` pending.
- **Files:** paths with `A` (added) or `M` (modified).
- **Subagents:** running and finished children, with state.
- **Browser:** the last opened URL and the `agent-browser` session name.

### 7.7 `details`

- Renders text lines or images, and scrolls with `ScrollBy(n)`.
- Header line, then a body. Width and height come from `SetSize`.
- The details pane never takes focus. `ui` routes `ctrl+e`/`ctrl+y` to it in NORMAL mode.

### 7.8 Trust dialog (`confirm`)

- **When.** Shown before the main TUI starts, as a short-lived tea program, whenever untrusted project config exists (§8.3).
- **Content.** The project path, then one line per project-layer effect (`permissions.bash → allow`, `providers.anthropic.base_url → https://…`, `agents.reviewer (from .claude/agents/reviewer.md) permissions → allow edit`, `instructions += docs/rules.md`).
- **Keys.** `t` trusts; `n` or `esc` continues untrusted.

## 8. Services, data, and core changes

### 8.1 Ports and events

**`SessionService` additions**

- `ListForCwd(ctx, cwd, limit)`
- `Todos(ctx, id)`
- `Rename(ctx, id, title)`
- `Configure(ctx, id, agent, model string)`

All of these go through the `session.modify` mutex.

**New read-only ports** (implemented in `internal/app` over existing services)

- `CatalogService.Providers() []core.ProviderStatus`: providers with credential status and models.
- `AgentService.Primary() []core.Agent`
- `ProjectService.Files(ctx) ([]string, error)`
- `ProjectService.ReadFile(ctx, path) ([]byte, error)`: used for diff context and screenshots; limited to files under the workdir or in the spill and blob directories.
- `BlobService.Open(ref) ([]byte, string, error)`: image bytes and MIME type.
- `PrefsService`: `Get() Prefs`, `Save(Prefs) error` (theme, sidebar, recent actions, per-project prompt history).

**`ChatService` changes**

- `SendRequest` gains `Attachments []string`. Chat reads each attachment through a `FileReader` interface:
  - text files: at most 50 KB each; binary files are refused;
  - images: through the image pipeline (§9.1).
- Chat stores each attachment as `PartAttachment{Path, Content | BlobRef}` on the user message, and marks text attachments as read in the tool tracker via a `ReadMarker` interface.
- `Compact(ctx, id)` moves here from `SessionService` and returns `ErrBusy` while a run is in progress.

**Events**

- `event.Base` gains `RootID`.
- New `StepFinished{MessageID, Usage, CostUSD}`, published after each model step.
- New `SessionUpdated{Session}`, published on title changes, `Rename`, and `Configure`.

**Carried-over fixes from the Plan 1 review**

- `GenerateTitle` saves only if the title is still the placeholder.
- The note `read` adds when it truncates output is budgeted so the executor's cap cannot cut it off.

### 8.2 Data

- **`data/blobfs`:** a content-addressed store at `$XDG_DATA_HOME/jig/blobs/<sha256[:2]>/<sha256>`, written with atomic rename. It has no garbage collection in Plan 2.
- **`data/prefsfs`:** `$XDG_STATE_HOME/jig/prefs.json`, written atomically.
- **`data/trustfs`:** `$XDG_DATA_HOME/jig/trust.json`, which maps an absolute project path to `{hash, grantedAt}`.
- **Store:** `core.Part` gains `KindAttachment`, and `core.ToolResult` gains `Media []core.Media{MIME, BlobRef}`. This is a JSON-shape addition in `parts.data_json`, so no schema migration is needed.

### 8.3 Project-config trust

- **Separate layers.** `config.Load` returns `Global` and `Project` layers separately, plus `Effects []Effect` (a human-readable list of what the project layer changes). The agent loaders (`agentfs`) do the same for project agent files.
- **The trust hash** is a SHA-256 over the sorted list of project config and agent file paths plus their contents.
- **Trusted:** the layers merge exactly as in Plan 1.
- **Untrusted:** a **tighten-only** merge applies:
  - A permission from the project layer is applied only if it is at least as restrictive as the result without it (deny > ask > allow), per tool and per pattern.
  - `providers.*`, `instructions`, and `skills.paths` from the project layer are ignored.
  - Project agent definitions are loaded, but their permissions go through the same tighten-only merge.
  - Project `model_aliases`, `default_model`, and `small_model` are allowed, since they cannot redirect credentials.
- **TUI:** the trust dialog decides. **Headless:** `--trust-project` grants trust; otherwise the project is untrusted and jig prints `warning: project config not trusted; loosening settings ignored (use --trust-project)`.

## 9. Images and browser integration

### 9.1 Images

- **Type.** `core.Media{MIME string; Ref string}`. `Ref` is the blob's SHA-256.
- **Pipeline** (`service/media`, used by `read` and by attachments):
  1. Decode png, jpeg, gif, or webp (via `golang.org/x/image`).
  2. Scale down so the long edge is at most 1568 px.
  3. Re-encode as PNG, or as JPEG for photos over 1 MB.
  4. Refuse anything over 5 MB after encoding.
  5. Store it in blobfs.
- **`read`** on an image extension returns the image in `Media`, with the output `image WxH (<bytes>)`.
- **Converter.**
  - If the catalog says the model supports images, each medium is sent as a `fantasy.ToolResultOutputContentMedia` (tool results) or as a file part (attachments).
  - Otherwise, the text `[image omitted: <model> does not accept images]` is sent instead.
  - Media bytes are loaded through a `BlobReader` given to `llm.Source`.
- **TUI.** `bubbles/imgrender` (ported from slk) picks kitty graphics, then sixel, then half-blocks. Detection uses `$TERM`, `$TERM_PROGRAM`, and terminal queries at startup, and `JIG_IMAGES=kitty|sixel|blocks|off` overrides it.

### 9.2 agent-browser integration

- **Config.** `[integrations.agent_browser] enabled = "auto" | true | false`. The default is `"auto"`, meaning enabled when `agent-browser` is found on `PATH`.
- **Skill discovery.** At startup, jig runs `agent-browser skills path`, with a 3 s timeout, cached in prefs by binary path and mtime. The returned directory is added as the lowest-precedence skills source. The model therefore sees agent-browser's own skill, matched to the installed version.
- **Permission preset.** This is a layer below the user config.
  - **allow:** `bash` patterns `agent-browser snapshot*`, `agent-browser screenshot*`, `agent-browser console*`, `agent-browser errors*`, `agent-browser get *`, `agent-browser is *`, `agent-browser tab`, `agent-browser a11y*`, `agent-browser vitals*`, `agent-browser read*`.
  - **ask:** `agent-browser *`.
  - The shell-metacharacter downgrade from Plan 1 still applies.
- **Screenshots.** When the output of an `agent-browser screenshot` bash call names an image path, the bash tool attaches that image as `Media` through the image pipeline. This works for any `bash` tool whose command starts with `agent-browser screenshot`, so the model sees the screenshot right away. It depends on the `agent-browser` path being detected, and applies only when the image lies under the workdir or the agent-browser temp directory.
- **TUI:** browser tool lines (§5.3), screenshots shown in the details split, and the sidebar Browser section.

## 10. Testing

- **`ui/transcript`:** table-driven tests of `Load`/`Apply` covering streaming, the tool lifecycle, descendants, subagent permissions, resume after interrupted runs, changed files, and the last browser URL.
- **Each `internal/bubbles` widget:**
  - behavior tests driven through `Update`;
  - golden frames at fixed sizes with pinned `Styles` (`testdata/golden/*.ansi`, `-update`), using `t.Parallel()`;
  - benchmarks for `blocklist` `Update` and `View` at 2,000 blocks, with a regression budget recorded in AGENTS.md.
- **`ui` App:**
  - golden frames for idle, streaming, details open, picker root, picker drill-down, the file picker, a permission card, a subagent permission, narrow mode, and an image in details (using the blocks renderer);
  - reconciliation tests through the real `Update` chain, with fake ports, built via `newTestApp(t, opts...)`.
- **Services:** real SQLite for the new ports; `-race` for `Configure`, `Rename`, and title saves; table tests for the tighten-only merge; blob dedup; image scaling; converter media vs. placeholder.
- **E2E, headless:**
  - `TestE2E_UntrustedProjectCannotLoosen`, `TestE2E_TrustProjectFlag`
  - `TestE2E_ImageAttachment`, `TestE2E_ReadImageReachesModel`
  - `TestE2E_AgentBrowserPresetAndSkill`, using a fake `agent-browser` script on `PATH`
- **E2E, TUI:** a smoke test under a pseudo-terminal (`creack/pty`, test-only): start `jig`, answer the trust dialog, type a prompt, get the scripted reply, open the picker, switch the model, and quit with `ctrl+d`.
- **Archtest:** the §3.3 rules. Plan 1's size limits apply to `App` as well (at most 15 fields and 20 methods).

## 11. Technology

- `charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, and `glamour/v2` (all at the versions current at plan time).
- `github.com/alecthomas/chroma/v2`
- `golang.org/x/image` (webp decoding and scaling)
- `creack/pty` (tests only)
- Ported from slk (MIT, same author): `imgrender`, `wintree`, the theme palettes, `overlay`, and `scrollbar`, adapted to the slk#236 conventions.
