# jig v1 — Core Design

- **Status:** draft, awaiting review
- **Date:** 2026-09-27
- **Module:** `github.com/gammons/jig` · **Binary:** `jig`

## 1. Intent

jig is a terminal AI coding harness in Go, in the spirit of OpenCode, built on
the Charm stack. It is meant to be shared publicly.

**What the author asked for**

- An OpenCode-quality UX without JavaScript/npm, with low memory use.
- A single process. No client/server split, and no re-implementing things like
  tmux.
- First-class skills (superpowers-style), and later skill packs that can be
  installed and updated.
- A different model per agent (e.g. haiku for explore, opus for plan), picked
  from the catwalk catalog.
- Vim-like keybindings for navigation, and ctrl+d to exit.
- Later, a vim-style way to watch subagents.
- A strict four-layer, service-oriented architecture (UI, service, client,
  data), with SQLite for persistence.
- Extensibility: plugins that change behavior at runtime (add tools, intercept
  tool calls, change prompts/context, react to events, add commands/keybinds,
  add providers). v1 does **not** ship a plugin system, but the architecture
  must make adding one straightforward.
- No god objects (slk's `cmd/slk/main.go` reached ~4.8k lines). Well tested and
  test-driven from the first commit.

**Assumptions** (not stated by the author)

- Linux and macOS terminals come first. Windows is not a v1 target.
- Anthropic is the main provider. Others arrive through fantasy with no extra
  design work.

**Success criteria for v1**

The author can use jig daily, with superpowers, for real work:

- Chat with an agent that reads, edits, and runs commands under permission
  control.
- Delegate to an `explore` subagent running on a cheaper model.
- Switch between build and plan agents, and resume past sessions.
- Do all of this with vim-style navigation.

## 2. Scope

**In v1**

- Anthropic, plus any other provider fantasy supports, configured by catwalk
  ID.
- Model catalog and picker backed by catwalk.
- Tools: `read`, `write`, `edit`, `bash`, `glob`, `grep`, `todo`, `task`,
  `skill`.
- Allow / ask / deny permissions for each tool and each agent.
- SQLite sessions: create, list, resume, automatic titles, manual `/compact`.
- Agents defined in TOML and markdown, each with its own model, plus subagents
  through the `task` tool.
- Skill discovery, the `skill` tool, AGENTS.md loading, and `instructions`
  files.
- A TUI with vim normal/insert modes, a scrollable transcript, subagents shown
  inline, and the pickers.
- Headless mode: `jig run "prompt"`.
- The internal extension registry, with every built-in feature registered
  through it.

**Deferred** (later sub-projects, each with its own spec)

1. Skill packs: `jig pack add|update|remove <git-url>`, per-pack tool-name
   mappings.
2. A vim-style subagent viewer (panes/buffers subscribing to subagent events).
3. External plugins (Lua, WASM, or RPC adapters onto the registry).
4. MCP client and LSP integration.
5. Automatic compaction, the `fetch` tool, OAuth/subscription login, and UI
   extension points.

## 3. Architecture

### 3.1 Layers and packages

```
cmd/jig/                 main.go: parse flags, call app.Run (≤ 60 lines)
internal/app/            composition root: one file per subsystem, construction only
internal/core/           ports (service interfaces) and value types; no logic
internal/core/ext/       extension registry and extension-point interfaces
internal/core/event/     typed events and the in-process pub/sub bus

UI layer
  internal/ui/           App router, reducer chain, per-mode key tables
  internal/ui/<region>/  transcript, input, statusbar, sidebar (own model + reducer each)
  internal/ui/picker/    the single shared picker widget (model/agent/session/command)
  internal/ui/styles/    theme

Service layer
  internal/service/agent/       Runner (agent loop), subagent spawning
  internal/service/agents/      agent definitions, model resolution
  internal/service/session/     session lifecycle, titles, compaction
  internal/service/permission/  rules engine + ToolHook
  internal/service/skills/      discovery merge, list transform, skill tool
  internal/service/prompt/      ContextTransforms: env, AGENTS.md, instructions
  internal/service/tools/       built-in tools (one file per tool)
  internal/service/command/     slash commands

Client layer
  internal/client/llm/          llm.Client interface + fantasy adapter
  internal/client/llm/llmtest/  scripted fake client
  internal/client/catalog/      catwalk catalog (embedded snapshot + cached refresh)
  internal/client/shell/        process execution for bash
  internal/client/search/       ripgrep execution for grep/glob, with a Go fallback

Data layer
  internal/data/store/          SQLite (sqlc + migrations), modernc.org/sqlite
  internal/data/config/         TOML load/merge into an immutable snapshot
  internal/data/skillfs/        SKILL.md discovery and parsing
  internal/data/agentfs/        markdown agent discovery and parsing
  internal/data/contextfs/      AGENTS.md / instructions file reading

Cross-cutting
  internal/archtest/            architecture tests (section 7)
  internal/clock/               Clock interface; real and fake
```

### 3.2 Dependency rules

These are enforced by `internal/archtest`.

- `ui/...` imports only `core/...` and `ui/...`. It does no I/O of its own:
  no `os/exec`, `net/http`, `database/sql`, file writes, `client/...`, or
  `data/...`.
- `service/...` imports `core/...` and declares small interfaces for the
  client and data capabilities it uses. It never imports concrete
  `client/...` or `data/...` types.
- `client/...` and `data/...` never import `service/...` or `ui/...`.
- Only `internal/app` imports concrete implementations from every layer.

### 3.3 Events

- Services publish typed events on `core/event.Bus`. Events include
  `SessionCreated`, `MessageStarted`, `TextDelta`, `ReasoningDelta`,
  `ToolCallStarted`, `ToolCallFinished`, `PermissionRequested`,
  `PermissionResolved`, `SubagentSpawned`, `RunFinished`, and `RunFailed`.
- Every event carries `SessionID` (and `ParentSessionID` where it applies).
- The UI turns a subscription into `tea.Msg`s. Services never call the UI.
- Delivery to subscribers uses bounded buffered channels. When a delta
  subscriber falls behind, adjacent deltas are merged rather than dropped.
  Lifecycle events are never merged or dropped.

### 3.4 Extension registry (`core/ext`)

| Point | Shape | v1 registrants |
|---|---|---|
| `Tool` | `Name()`, `Schema()`, `Concurrent() bool`, `Run(ctx, Call) (Result, error)` | built-in tools, `task`, `skill` |
| `ToolHook` | `Before(ctx, Call) (Call, Decision, error)`, `After(ctx, Call, Result) Result` | permission service |
| `ContextTransform` | `Transform(ctx, *Request) error`, ordered by priority | env info, agent prompt, instructions, AGENTS.md, skill list |
| `EventSubscriber` | `Handle(ctx, event.Event)` | none (reserved) |
| `Command` | `Name()`, `Description()`, `Run(ctx, args) error` | `/new`, `/sessions`, `/model`, `/agent`, `/compact`, `/quit` |
| `Keybind` | `(mode, key) → command name` | default vim keymap |
| `Provider` | catwalk provider ID → `llm.Client` factory | fantasy-backed providers |

- Built-ins register with `builtin.Register(reg)` through the same public API
  a future plugin adapter will use.
- The registry is populated once at startup and then frozen, so it can be read
  concurrently without locks. Runtime-loadable plugins will later add a
  copy-on-write swap.

## 4. Agents, models, and the agent loop

### 4.1 Agent definition

```toml
[agents.explore]
description = "Fast read-only codebase search"
mode        = "subagent"                     # primary | subagent | all
model       = "anthropic/claude-haiku-4-5"   # catwalk provider/model
prompt      = "{file:prompts/explore.md}"
max_steps   = 40
can_spawn   = false                           # may call `task`
[agents.explore.permissions]
write = "deny"
edit  = "deny"
bash  = "ask"
```

- Markdown agents use the same fields in their frontmatter, and the body is the
  prompt. They are loaded from `~/.config/jig/agents/*.md`,
  `.jig/agents/*.md`, and `.claude/agents/*.md`.
- Claude Code's `model: haiku|sonnet|opus` values, and its `tools:` list, are
  mapped through `[model_aliases]` and a tool allow-list.
- Where an agent is defined in more than one place, later sources win: built-in
  defaults < global TOML < global markdown < project TOML < project markdown.
- **Built-in agents:**
  - Primary: `build` (all tools) and `plan` (write, edit, and bash are `ask`).
  - Subagents: `explore` (read-only) and `general` (all tools except `task`).
  - Hidden: `title` and `compaction`.

### 4.2 Model resolution

The first match wins:

1. The agent's `model`.
2. For a subagent, the model its parent is running.
3. The model chosen for the session with `/model`.
4. `default_model` from config.

If none of these resolves, or the provider has no credentials, the run fails
with an actionable error before any request is sent. For `title` and
`compaction` the fallback is `small_model`, then the resolved model.

### 4.3 The Runner loop (`service/agent`)

For each user message, the loop runs until it stops:

1. **Build the request.** Start from the agent prompt, run the
   `ContextTransform`s, then add the message history.
2. **Stream from `llm.Client`.**
   - Publish deltas as they arrive.
   - Persist parts as they complete.
   - Record token usage and compute cost from catwalk pricing.
3. **Execute tool calls.**
   - Each call goes through `Before` hooks, then `Run`, then `After` hooks.
   - Consecutive calls whose tool is `Concurrent()` run in parallel (the
     read-only tools and `task`). Non-concurrent calls run one at a time, in
     order.
   - Results are appended to the history in the order the model issued the
     calls.
4. **Continue or stop.** Loop back to step 1 while the model made tool calls.
   Stop when there are no tool calls, on `max_steps` (a final message says so),
   on cancellation, or on an error.

**Cancellation.** Each run owns a `context.Context`. `Runner.Cancel(sessionID)`
cancels that run and its descendant subagent runs. Any partial output is kept
and marked `interrupted`.

**Errors.**

- Provider errors are retried with backoff when they are retryable (rate
  limits, overloads, and 5xx responses; 3 attempts, and a server
  `retry-after` is honored). Other errors surface as `RunFailed`.
- A tool error becomes the tool result with `is_error=true`, so the model can
  recover.
- A panic inside a tool is recovered and converted into a tool error.

### 4.4 Subagents (`task` tool)

- **Input:** `agent`, `description`, `prompt`, and an optional `session_id` to
  continue an earlier run.
- A new call creates a child session with `parent_id` set and runs a `Runner`
  for the subagent's agent. It returns the final assistant text and the child
  session ID.
- The child's permission prompts surface through the same event bus, tagged
  with the child's session.
- Subagents may call `task` only when `can_spawn = true`. Depth is capped at 3.

## 5. Tools, permissions, skills, and context

### 5.1 Tools

Each tool is one file in `service/tools` and depends on narrow interfaces
(`FS`, `Shell`, `Searcher`, `TodoStore`).

| Tool | Behavior |
|---|---|
| `read` | Reads a line range, prefixed with line numbers. Refuses binary files. Lists directory entries. |
| `write` | Creates or overwrites a file. Requires the file to have been `read` first in this session if it already exists. |
| `edit` | Exact-string replace with a unique-match rule and optional `replace_all`. Same read-first rule as `write`. |
| `bash` | Runs in the session working directory. Default timeout 2 min (configurable per call up to 10 min). Output is truncated to the last 30 KB, and the full output is saved to a temp file whose path is returned. |
| `glob` | ripgrep `--files` with a glob; falls back to a Go walk. Respects `.gitignore`. |
| `grep` | ripgrep with a regex and an include filter; falls back to a Go implementation. |
| `todo` | Replaces the session's todo list. Stored in SQLite and shown in the sidebar. |
| `task` | See section 4.4. |
| `skill` | See section 5.3. |

### 5.2 Permissions (`service/permission`)

- **Rules** map `tool → allow | ask | deny`. For `bash`, rules are ordered glob
  patterns matched against the command (e.g. `"git status*" = "allow"`,
  `"*" = "ask"`).
- **Precedence:** agent rules > project config > global config > defaults.
  The defaults are `allow` for read, glob, grep, todo, task, and skill, and
  `ask` for write, edit, and bash.
- **Implementation:** the `permission` service is a `ToolHook`. For `ask`, it
  publishes `PermissionRequested` and blocks on the reply (or on the context
  being cancelled).
- **Replies:** allow once, always allow this pattern for this session, or
  deny, optionally with a message. A deny becomes a tool error whose text is
  the user's message, so the model can adjust.
- **Headless mode:** `ask` becomes `deny` unless `--yes` is passed.

### 5.3 Skills

- **Discovery** (`data/skillfs`):
  - Global: `~/.config/jig/skills`, `~/.agents/skills`, `~/.claude/skills`.
  - Project: `.jig/skills`, `.agents/skills`, `.claude/skills`, searched from
    the working directory up to the git root.
  - Plus any `skills.paths`.
  - Each skill is a directory containing `SKILL.md` with `name` and
    `description` frontmatter.
  - On a name clash the closer directory wins. Invalid skills are reported as
    warnings, not fatal errors.
- **Listing:** a `ContextTransform` adds an `<available_skills>` block (name,
  description, location) to the system prompt.
- **`skill` tool:** takes an `id`. It returns the SKILL.md body, the skill's
  base directory, and a list of up to 50 files in that directory.
- **Instructions:** `instructions = [paths/globs]` in config inlines those
  files into the system prompt. This is how superpowers' `using-superpowers`
  bootstrap works in v1.

### 5.4 Context (`service/prompt`)

The transforms are applied in this order:

1. Agent prompt.
2. Environment: working directory, platform, date, whether it is a git repo.
3. `instructions` files.
4. AGENTS.md: the global `~/.config/jig/AGENTS.md`, then project files from
   the git root down to the working directory. `CLAUDE.md` is used when no
   AGENTS.md exists at that level.
5. Skill list.

## 6. Data, config, and the TUI

### 6.1 SQLite (`data/store`)

- **Location:** `$XDG_DATA_HOME/jig/jig.db`. WAL mode. Migrations are embedded
  and applied at startup.
- **Tables:**
  - `sessions(id, parent_id, title, agent, model, cwd, created_at, updated_at)`
  - `messages(id, session_id, role, agent, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd, status, created_at)`
  - `parts(id, message_id, seq, kind, data_json)`, where `kind` is one of
    `text`, `reasoning`, `tool_call`, `tool_result`, `compaction`.
  - `todos(session_id, seq, content, status)`
- **Compaction:** `/compact` runs the `compaction` agent over the history and
  stores a `compaction` part. When history is rebuilt, everything before the
  latest compaction part is replaced by its summary.

### 6.2 Config (`data/config`)

- **Files:** `$XDG_CONFIG_HOME/jig/config.toml`, merged with
  `.jig/config.toml` from the git root down to the working directory. Closer
  files win.
- Loaded into an immutable `core.Config` snapshot.
- `{env:VAR}` and `{file:path}` substitutions are allowed in string values.
- **Keys:** `default_model`, `small_model`, `providers.<id>` (`api_key`,
  `base_url`, `options`), `agents.<name>`, `model_aliases`, `permissions`,
  `skills.paths`, `instructions`, `keybinds`, `theme`.
- **Credentials:** when no `api_key` is set, the environment variables catwalk
  lists for that provider are used (e.g. `ANTHROPIC_API_KEY`).
- **Catalog:** catwalk's catalog is embedded at build time. At most once a day
  it is refreshed in the background into `$XDG_CACHE_HOME/jig/catalog.json`.
  If the refresh fails, the cached or embedded copy is used.

### 6.3 TUI (`ui`)

- **Layout:**
  - Transcript: scrollable, markdown via glamour, collapsible tool calls, and
    subagent runs shown inline and collapsed with a live status line.
  - Input box.
  - Status bar: mode, agent, model, context tokens used / limit, session cost.
  - Optional sidebar (ctrl+b): todos and session info.
- **`App`:** a router only. It holds the region models and routes messages
  through a reducer chain. Behavior is added in `reducer_*.go` files and
  per-mode key tables (`mode_*.go`), never by growing `Update`.
- **Modes:**
  - **INSERT** (the default at startup):
    - enter sends; shift+enter or alt+enter inserts a newline.
    - esc switches to NORMAL.
    - tab cycles primary agents.
    - **ctrl+d on an empty input exits**, the way it does in a shell.
  - **NORMAL:**
    - `j`/`k` move between messages; `gg`/`G` go to the top/bottom.
    - `ctrl+u` scrolls up half a page.
    - `ctrl+d` **also exits**, rather than vim's half-page down. Half-page down
      is `ctrl+f`/`space`.
    - `o`/`enter` expands or collapses a tool call or subagent.
    - `i`/`a` return to INSERT.
    - `:` opens a command line (`:q`, `:new`, `:model`, …).
    - `/` searches the transcript.
    - `y` yanks the selected message to the clipboard over OSC 52.
  - **Both modes:**
    - ctrl+c cancels the running turn, or exits if nothing is running.
    - ctrl+p opens the command picker.
- **Permission prompt:** an inline card in the transcript with `a` allow, `A`
  always, `d` deny, and `D` deny with a message.
- **Pickers:** model, agent, session, and command pickers are all
  `ui/picker`. It handles fuzzy filtering, windowed lists, and the modal
  chrome, and each picker supplies only its items and an accept action.
- **Keybinds:** all remappable through `[keybinds]`, and resolved through the
  registry's `Keybind` entries.

### 6.4 Headless

- `jig run [--agent A] [--model M] [--yes] [--session ID] "prompt"` uses the
  same services with a plain-text renderer subscribed to the bus.
- Exit codes: 0 on success, 1 when the run failed, 2 on a config error.

## 7. Code health guardrails

These are enforced by `internal/archtest` (plain `go test`) and golangci-lint.
Exceptions go in `internal/archtest/allowlist.go`, and each one needs a
justification comment.

| Rule | Limit |
|---|---|
| Layer imports | Section 3.2 |
| `cmd/jig/main.go` | ≤ 60 lines |
| `internal/app` | One file per subsystem. Each function ≤ 40 lines. Construction and connection only. No closures capturing mutable variables. |
| Non-test source file | ≤ 500 lines |
| Function | `funlen` ≤ 80 lines; `gocognit` ≤ 30 |
| Struct | ≤ 15 fields, ≤ 20 methods (generated code excluded) |
| Package-level mutable vars | Forbidden outside `main` and generated code |
| Tests | No `time.Sleep`, no `time.Now()`; use `internal/clock` |
| Formatting | `gofmt` clean |

Structural rules:

- Every piece of mutable state has one owner, behind a mutex or confined to a
  single goroutine.
- Prefer deleting redundant state over keeping two copies in sync.
- `AGENTS.md` exists from the first commit. It covers the architecture, the
  invariants, and a "shared code: check here before writing a helper" table,
  and it is updated in the same change as the code it describes.

## 8. Testing strategy

- **TDD.** Every plan task is red → green → refactor. Tests use the standard
  library `testing` package only and are white-box by default.
- **Agent loop.** `llmtest.Client` replays scripted streams: text, reasoning,
  tool calls, usage, errors, and mid-stream failures. These drive tests of the
  Runner, subagents, permissions, cancellation, retries, and `max_steps`.
- **fantasy adapter.** Tested against recorded HTTP fixtures (a stdlib
  `httptest` server replaying captured responses). A live test runs only when
  `JIG_LIVE_TESTS=1`.
- **Store.** Real SQLite in `t.TempDir()`, never mocked.
- **Data loaders.** `testdata/` trees for skills, agents, AGENTS.md, and
  config merging.
- **Tools.** Run against temp dirs. `bash` is tested with real short
  commands; grep/glob are tested both with and without ripgrep available.
- **UI.**
  - Golden-frame tests (`testdata/golden/*.ansi`, refreshed with `-update`)
    with the theme, clock, and size pinned.
  - Behavior tests drive `tea.Msg`s through the real `Update` chain.
  - One shared `newTestApp(t, opts...)` builder.
- **End to end.** Build `jig` and run `jig run` against a fake provider
  selected by config (`provider = "jigtest"`, which exists only in test
  builds). Assert on stdout, the exit code, and the DB contents.
- **CI.** `go test ./... -race`, `golangci-lint run`, `gofmt -l .`.

## 9. Technology

- Go (latest stable).
- UI: `charm.land/bubbletea/v2`, `lipgloss/v2`, `bubbles/v2`, glamour.
- Models: `charm.land/catwalk` (MIT) for the catalog and `charm.land/fantasy`
  (Apache-2.0) for the LLM client, wrapped behind `client/llm`.
- Storage: `modernc.org/sqlite` (pure Go) with sqlc-generated queries.
- Config and frontmatter: `BurntSushi/toml`, `yaml.v3`.
- CLI: stdlib `flag` with subcommands (`jig`, `jig run`, `jig models`,
  `jig sessions`).
- License: MIT.
