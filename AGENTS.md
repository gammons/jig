# jig — agent notes

## Build, test, lint

```
make build      # go build -o bin/jig ./cmd/jig
make test       # go test ./... -race, then the jigtest-tagged e2e + jigtest tests
make lint       # golangci-lint run, with and without --build-tags jigtest
make fmt-check  # gofmt -l . must be empty
make check      # all of the above
```

Every commit must pass all four (`make check`) before it lands.

## Architecture in one screen

Four layers (UI, service, client, data) around `internal/core` ports and an
extension registry that all built-ins register through. Services talk to the
outside world only through small interfaces they declare. The UI learns about
progress through an in-process event bus. `internal/app` is the only package
that knows the concrete types.

The UIs call services only through three ports in `internal/core/ports.go`:
`core.ChatService` (implemented by `service/chat`; `Send` drives a turn,
resolving any `SendRequest.Attachments` against its `WorkDir` first,
`Compact` summarizes history behind the Runner's busy exclusion),
`core.SessionService` (implemented by `service/session`, covering listing,
`Rename`, and `Configure`), and `core.PermissionService` (implemented by
`permission.BusAsker`). Persisted UI preferences go through
`core.PrefsService` (`internal/core/prefs.go`, implemented by
`prefsfs.Store`): `Get` and `Update(fn)`, never a whole-struct save —
`Update` re-reads the file under the store mutex and applies `fn` to the
fresh copy. `chat.Send` and `chat.Compact` failures caused by
configuration or input (including a missing session) are `*chat.ConfigError`
(headless exit 2).

The TUI also calls five read-only ports, all implemented in
`internal/app/ports.go`: `core.CatalogService` (`catalogPort`: every
catalog provider paired with whether its credentials resolve now, via
`llm.Source.HasCredentials`), `core.AgentService` (`agents.Service`
satisfies it directly with `Primary`), `core.ProjectService`
(`projectPort`: gitignore-aware `Files` with each file's git-modified
status, and a `ReadFile` confined to the workdir, the runtime's spill
dir, and the blob store's directory), `core.BlobService` (`blobPort`:
`Open` returns a blob's bytes and detected MIME type), and
`core.EditorService` (`editorPort`: `Edit` opens `$VISUAL`/`$EDITOR`/`vi`
over a temp file in the spill dir via `core.ExecCommand`, the method set
of `tea.ExecCommand`).

```
cmd/jig/main.go                         entry: os.Exit(app.Run(...))
internal/clock/                         Clock interface, Real, Fake
internal/ids/                           sortable ID generator
internal/pathid/                        path identity key (symlinks resolved)
internal/archtest/                      architecture tests + allowlist
internal/core/                          value types + ports (no services)
internal/core/event/                    events + Bus
internal/core/ext/                      extension points + Registry
internal/core/llmtest/                  scripted fake core.LLM
internal/data/paths/                    XDG path resolution
internal/data/fsroot/                   git root + ancestor walking
internal/data/frontmatter/              YAML frontmatter parsing
internal/data/config/                   TOML load/merge/substitute
internal/data/store/                    SQLite store (one file per table)
internal/data/skillfs/                  SKILL.md discovery
internal/data/agentfs/                  markdown agent discovery
internal/data/contextfs/                AGENTS.md / instructions files
internal/data/atomicfile/               temp-file-then-rename atomic writes
internal/data/blobfs/                   content-addressed blob store
internal/data/prefsfs/                  core.Prefs JSON persistence
internal/data/trustfs/                  trust grants + project config hash
internal/data/themefs/                  custom theme *.toml discovery (ported from slk)
internal/client/agentbrowser/           agent-browser detection + skills path exec
internal/client/catalog/                catwalk catalog
internal/client/llm/                    fantasy adapter + model Source
internal/client/llm/jigtest/            scripted provider (build tag jigtest)
internal/client/shell/                  process execution
internal/client/search/                 rg + Go fallback glob/grep
internal/service/agents/                builtins, merge, model resolution, tool filtering
internal/service/permission/            rules, evaluation, ToolHook, askers, Tighten
internal/service/trust/                 untrusted project config: Restrict, Effects
internal/service/tools/                 read/write/edit/bash/glob/grep/todo
internal/service/prompt/                ContextTransforms
internal/service/skills/                skills service, list transform, skill tool
internal/service/agent/                 Runner (agent loop)
internal/service/task/                  task tool (subagents)
internal/service/session/               sessions, history, compaction, titles
internal/service/chat/                  ChatService facade the UIs call
internal/service/media/                 image decode/scale/re-encode into blobs
internal/ui/plain/                      headless renderer (io.Writer)
internal/ui/transcript/                 (Plan 2) transcript projection, core-only
internal/ui/theme/                      theme palettes + Complete/Custom (ported from slk); Set/Build maps a Palette into every widget's Styles
internal/bubbles/                       (Plan 2) Bubble Tea widgets and helpers
internal/bubbles/ansi/                  sanitize untrusted text; ANSI-safe width/wrap/highlight
internal/bubbles/overlay/               center a box over a dimmed background (ported from slk)
internal/bubbles/scrollbar/             1-column proportional scrollbar gutter (ported from slk)
internal/bubbles/wintree/               window split tree, pure geometry (ported from slk)
internal/bubbles/mdrender/              width-aware Markdown rendering via glamour, one TermRenderer cached per width
internal/bubbles/coderender/            chroma syntax highlighting + go-udiff unified diffs as styled lines
internal/bubbles/imgrender/             image protocol detection (R24), bounded decode, fitted half-block / kitty-placeholder / sixel rendering
internal/bubbles/blocklist/             transcript list: block cursor, per-item render cache, yOffset scrolling, bottom pinning, search
internal/bubbles/picker/                ctrl+p picker: fuzzy drill-down list, groups, recents, multi-mark, text-input level, preview callback
internal/bubbles/prompt/                growing 1-8 line prompt: history walk, paste chips, $EDITOR round trip, queued state
internal/golden/                        golden-frame test assertion
internal/app/                           composition root + CLI
e2e/                                    end-to-end tests against the built binary
```

Dependency rules (spec §3.2, §3.3), enforced by `internal/archtest`:

- `ui/...` imports only `core/...`, `ui/...`, and `bubbles/...`. It does no
  I/O of its own: no `os/exec`, `net/http`, `database/sql`, file writes,
  `client/...`, or `data/...`. Its third-party imports are `charm.land/...`
  only. `internal/ui/transcript` is further restricted to stdlib and
  `internal/core/...` only (no `ui` or `bubbles`).
- `service/...` imports `core/...` and declares small interfaces for the
  client and data capabilities it uses. It never imports concrete
  `client/...` or `data/...` types.
- `client/...` and `data/...` never import `service/...` or `ui/...`.
- Only `internal/app` imports concrete implementations from every layer.
- `bubbles/...` imports stdlib and, among its own subpackages, only
  `internal/bubbles/ansi`, `internal/bubbles/overlay`,
  `internal/bubbles/scrollbar`, and `internal/bubbles/wintree` (never its
  own package). Its third-party imports are `charm.land/...`,
  `github.com/charmbracelet/x/ansi`, `github.com/alecthomas/chroma/v2`,
  `golang.org/x/image/...`, `github.com/sahilm/fuzzy`, and
  `github.com/aymanbagabas/go-udiff`. No struct field in `bubbles/...` has
  type `func(tea.Msg)`, and every `View` method takes zero parameters.

## Architecture tests

`internal/archtest` parses every `.go` file in the repo and enforces the
layer-import rules and size limits above, plus a no-package-mutable-vars
and no-`time.Sleep`/`time.Now`-in-tests hygiene check. Exceptions go in
`internal/archtest/allowlist.go` with a justification.

## Invariants

- No I/O in `ui`.
- `ProjectService.ReadFile` is confined to the workdir, spill dir, and
  blob dir, after resolving symlinks.
- Every piece of mutable state has exactly one owner that serializes
  access to it: `session.Service`'s mutex owns session read-modify-writes,
  `ext.Registry` owns extension registration (only until `Freeze`),
  `permission.Hook` owns its per-root-session grants, `agent.Proxy` owns
  the runner late-binding. Nothing else mutates them directly.
- The extension registry is frozen after startup: it is populated once and
  then read without locks.
- All built-ins register through `ext.Registry`; runtime code depends on
  `ext.View`, an immutable, read-only snapshot of the registry taken at
  `Freeze` — every `View` method returns a fresh copy of its backing
  slice, so callers cannot mutate it.
- All time comes from `internal/clock`; no `time.Now()` or `time.Sleep` in
  `_test.go` files.
- Tools return `IsError` results, not Go errors, except for ctx
  cancellation.
- The executor caps every tool result at 50 KB (after the After hooks),
  so a tool never needs its own cap for context safety; `read`, `grep`,
  and `bash` still bound their own output more tightly.
- `ToolHook.After` runs only when the tool's `Run` ran (including errors,
  panics, and cancellation), never for blocked, unknown, or invalid
  calls.
- Permissions: `permission.Hook` evaluates the current agent's rules and
  every entry of `rc.Ancestors` (each merged over config) and takes the
  most restrictive action; `task` appends the parent's rules to the
  child's `Ancestors`. A `bash` allow from a pattern is downgraded to ask
  when the command has shell metacharacters (including any `$`). Session
  grants apply only to `ask`.
- An untrusted project layer goes through `trust.Restrict` before any
  merge; `permission.Tighten` never keeps an `allow` pattern, and keeps an
  `ask` pattern only where the baseline has no `deny`. A top-level project
  `ask` pattern is also dropped when any global agent denies that tool.
  Trust effects never print secrets: a literal `api_key`/`options` →
  `(set)`, a `{env:}`/`{file:}` token prints raw (it is a name, not a
  secret), and `base_url` prints as `scheme://host[:port]/path` with
  userinfo dropped and any query shown as `?…`.
- Trust is decided once in `loadEnv`, before any service is built;
  `e.cfg()` is already the trusted or restricted merge. Project config
  files are loaded without `{env:}`/`{file:}` substitution until the
  project is trusted (a permission action holding a token is then not
  applied, but is listed in the trust effects by its raw token and counts
  as dropped),
  and agent sources come from `e.layers` (post-trust), never the raw
  project layer. The trust hash (`trustfs.HashOptional`) covers every
  project config file, agent file, and in-tree `{file:}` include (a
  missing include hashes as absent). It reads regular files only, up to
  1 MiB each: a non-regular file or an unreadable include hashes as an
  "unreadable" record, and a bigger file by its size. After the trusted
  re-load the hash is recomputed; a mismatch makes the run untrusted. Grants are keyed by
  git root (or workdir) and hold a set of accepted hashes (most recent
  first, at most 16), so monorepo subdirs with different configs keep
  their own grants; `trustfs.Store.Get(project, hash)` checks membership.
- Model strings go through `agents.ParseRef`/`Service.ResolveRef`
  (`provider/model` or a `[model_aliases]` name) everywhere: `--model`,
  `default_model`, `small_model`, agent `model` fields, startup
  validation.
- `chat.Send` resumes a session only from its own `Cwd`; only a missing
  session (`session.ErrNotFound`, via `Store.IsNotFound`) is a
  `ConfigError` on resume.
- Bash spill files live in the per-process 0700 dir `internal/app`
  creates (`runtime.spillDir`, removed on close), are named from
  `ids.Gen` (never the call ID), and are created `O_EXCL` at 0600.
- The agent Runner writes exactly one assistant message per model step; the
  results of that step's tool calls are parts on the same message.
- Every event's `Base.RootID` is the root session of the run that produced
  it; the UI routes descendant events by it.
- The model sees `session.Service.History`: the last message holding a
  `PartCompaction` and everything after it (all messages if none).
  Compaction and title requests run on `agents.SmallModel(resolved model)`.
- Session read-modify-writes (`Touch`, title save) go through
  `session.Service`'s mutex; don't `Get`+`Update` a session concurrently
  with a run.
- The Runner's `running` map is the one owner of per-session busyness: `Run`
  registers id there for the turn, and `chat.Compact` registers the same id
  there via `Runner.Exclusive` for the compaction, so a run and a
  compaction on the same session always exclude each other. Either returns
  `core.ErrBusy` when the other holds id; `Cancel(id)` cancels whichever one
  does.
- `agent.Proxy` is the only setter-style late binding (for the task tool);
  `Set` panics if called twice.
- `llm.Source` resolves provider factories through a providers-only
  frozen view (`app.providerView`): the session service needs the Source,
  and the task tool in the main registry needs the session service.
- `event.Subscription.Close` discards undelivered events. Headless drains
  by publishing an app-local marker event and waiting for the renderer to
  reach it before closing the subscription.
- The catalog refresh uses `$CATWALK_URL`, defaulting to
  `https://catwalk.charm.land`; it runs in the background and is never
  awaited.
- Every string from a model, a tool, a file, or the store passes
  `ansi.Sanitize` (or `SanitizeLine`) before it is rendered; only jig's own
  styling escapes reach the terminal. This includes `ui/plain`'s output
  (streamed text via `Sanitize`, stderr lines via `SanitizeLine`) and
  `internal/app`'s warnings, errors, and listings (via `printLine`).
- Only `imgrender` produces kitty placeholder cells and raw image payloads;
  `ui` sends payloads with `tea.Raw`, never inside `View`.
- `core.Media.Data` is never persisted; only `client/llm` fills it, from
  blobs, for a single request.
- Media bytes are loaded only in `client/llm`'s `For` wrapper, per
  request; unsupported models get the `[image omitted: …]` text instead.
  Only the newest 20 media of a request (`maxRequestImages`) are loaded;
  older ones get an `[image omitted: earlier image; …]` note.
- `GenerateTitle` never overwrites a non-placeholder title.
- Stored `Session.Cwd` is `pathid.Key(workDir)`.
- The agent-browser preset is not a config layer: `internal/app` passes
  it to `permission.NewHook` via `WithPreset` only when the integration is
  enabled (an untrusted project cannot enable it). The Hook consults it
  only when an agent's action is an `ask` from the tool's Default (no user
  pattern matched); a preset allow then applies, still subject to the
  metachar downgrade. It never overrides a deny or a user pattern, and
  never affects `ToolsFor`.
- bash attaches a screenshot only from the workdir or `screenshot*` files
  in the OS temp dir, after resolving symlinks.

## Shared code — check here before writing a helper

| Need | Use |
|---|---|
| Time / sleeping / timeouts | `clock.Clock`, `clock.NewFake` |
| Sortable, prefixed IDs | `ids.Gen` (`ids.New(clk, rnd)`, `g.Next("ses")`) |
| Value types shared across layers (Message, Session, ModelRef, Agent, Config, Rule, ...) | `internal/core` |
| Service ports the UIs call (`ChatService`, `SessionService`, `PermissionService`) | `internal/core` (`ports.go`) |
| Fake LLM for service tests | `llmtest.New(llmtest.Text(...), ...)` |
| Scripted model for e2e tests (tag `jigtest`) | `jigtest.Script` + `writeScript`/`jigtestConfig` in `e2e/harness_test.go` |
| One-shot, tool-less LLM call returning joined text | `agent.Complete(ctx, llm, system, user)` |
| Session title placeholder (first line, ≤50 runes) | `session.PlaceholderTitle(text)` |
| Compare two paths for identity (symlinks, macOS `/var` → `/private/var`) | `pathid.Key(p)` (`EvalSymlinks`, falling back to `Abs`) |
| Expand a leading `~`/`~/` in a config path | `paths.ExpandHome(p, home)` |
| Find a project's git root from a directory | `fsroot.GitRoot(dir)` |
| Walk root→leaf ancestor directories for context/skill discovery | `fsroot.Chain(root, dir)` |
| Parse a `---\n<yaml>\n---\n<body>` file | `frontmatter.Parse(src, &meta)` |
| Resolve XDG base directories | `paths.Resolve(getenv)` (`ConfigDir`/`DataDir`/`CacheDir`/`StateDir`) |
| Write a file atomically (temp file, fsync, rename) | `atomicfile.Write(path, data, perm)` |
| Content-addressed blob storage (SHA-256 refs) | `blobfs.New(dir)` / `(*Store).Put`, `.Open` |
| Turn image bytes into a bounded, stored `core.Media` (+ `core.ImageInfo`) | `media.New(blobs).Process(data)`; `media.IsImagePath(p)` |
| Build a `core.ToolResult` for a tool's `Run` | `core.ToolError(call, msg)` (sets `IsError`) / `core.ToolOK(call, output)` |
| Resolve a tool's `path` input against `rc.WorkDir` | `resolvePath(workDir, path)` in `service/tools` (also backs `subjectPath` for `ext.Subjecter`) |
| Permission subject for a search tool's `path` input | `searchSubject(rc, input)` in `service/tools` |
| Is this file binary? | `tools.LooksBinary(b)` |
| Overlay permission rules per tool / keep only tightening entries | `permission.Overlay(lo, hi)` / `permission.Tighten(baseline, add)` |
| A built-in agent by name | `agents.Builtin(name)` / `agents.BuiltinNames()` |
| Resolve a model string (ref or alias) | `agents.ParseRef(s, cfg.ModelAliases)` / `(*agents.Service).ResolveRef(s)` |
| Session events/messages → display blocks | `transcript.New(root)`, `Load`, `Apply` |
| Golden-frame assertion | `golden.Assert(t, name, got)`; update with `JIG_UPDATE_GOLDEN=1` |
| Make untrusted text (model/tool/file/store) safe to render | `ansi.Sanitize(s)` in `internal/bubbles/ansi` (keeps `\n`, `\t`) |
| Same, for single-line contexts (titles, paths, list rows) | `ansi.SanitizeLine(s)` (`\n`/`\t` → space) |
| Highlight a search query in styled text without touching escapes | `ansi.Highlight(s, query, on, off)` |
| SGR on/off strings for a fg/bg pair (e.g. a search highlight) | `ansi.SGR(fg, bg) (on, off)` |
| A scrolling list of variable-height blocks with a cursor, cache, and search | `blocklist.New(render, opts...)` / `SetItems`, `Upsert`, `SetSearch`, `View` in `internal/bubbles/blocklist` |
| The ctrl+p picker: fuzzy drill-down list, groups, recents, multi-mark, a text-entry level, a preview callback | `picker.New(load, opts...)` / `Open(root)`, `Close`, `SetRecent`, `SetSize`, `Update`, `View` in `internal/bubbles/picker` |
| The action catalogue (built-ins + registered `ext.Command`s) / resolve the keymap from default binds + `[keybinds]` config | `actions.NewCatalogue(cmds)` / `.All()`, `.Get(id)`; `actions.Resolve(binds, config, c)` / `Keymap.Lookup`, `.Keys` in `internal/ui/actions` |
| The growing prompt textarea: history walk, paste-collapse chips, an `$EDITOR` round trip, and a queued border title | `prompt.New(edit, opts...)` / `SetWidth`, `Height`, `SetAgent`, `SetQueued`, `SetHistory`, `Insert`, `Value`, `Reset`, `Focus`, `Blur`, `Update`, `View` in `internal/bubbles/prompt` |
| Wrap styled text to a width, hard-breaking long words | `ansi.Wrap(s, width)` (also `ansi.Width`/`Truncate`/`Cut`) |
| Center a modal box over a dimmed background | `overlay.Center(background, width, height, box, dim)` in `internal/bubbles/overlay` |
| Overlay a proportional scrollbar gutter onto rendered rows | `scrollbar.Overlay(visible, width, total, yOffset, visibleHeight, bg, trackFg, thumbFg)` / `scrollbar.Visible(total, visibleHeight)` in `internal/bubbles/scrollbar` |
| Vim-style window split tree (layout, split/close/navigate) | `wintree.New()` / `(*Tree).Split`, `.Close`, `.Only`, `.Cycle`, `.NavigateDir`, `.SetFixed`, `.Layout`, `.ComputeRects` in `internal/bubbles/wintree` |
| Render Markdown to width-wrapped terminal lines | `mdrender.New(opts...)` / `(*Renderer).Render(md, width)`, `.SetStyles(Styles)` in `internal/bubbles/mdrender` |
| Syntax-highlight source code / render a styled unified diff | `coderender.Highlight(path, code, st)` / `coderender.Diff(path, before, after, context, st)`, `coderender.DiffText(before, after, context)` in `internal/bubbles/coderender` |
| Pick the terminal image protocol / decode untrusted image bytes (bomb-guarded) / render an image into a cell box | `imgrender.Detect(env, terminalName)` / `imgrender.Decode(data)` / `imgrender.New(p, WithCellSize(w, h), WithTmux(on)).Render(key, img, maxCols, maxRows)` (send `Result.Upload` / `Place(res, x, y)` via `tea.Raw`) in `internal/bubbles/imgrender` |
| Map a theme `Palette` into every widget's `Styles` | `theme.Build(p, version) theme.Set` |

## Performance budgets

Binding: if a benchmark misses its budget, fix the algorithm; never raise
the budget. Run with `go test -run XXX -bench . -benchmem <pkg>`.

| Benchmark (`internal/bubbles/blocklist`, 2,000 items of 1–12 lines, 120×40) | Budget | Measured (AMD Ryzen AI 9 HX 370) |
|---|---|---|
| `BenchmarkBlocklist_View2000` — warm cache, `View` after one `j` | < 2 ms/op | 0.18 ms/op |
| `BenchmarkBlocklist_Update2000` — `Upsert` of the last item (streaming) + `View` | < 3 ms/op | 0.44 ms/op |
| `BenchmarkBlocklist_Load2000` — `SetItems` + first `View` | < 250 ms/op | 31 ms/op |

The blocklist renders only new, changed, or restyled items (cache key:
ID, Version, width, stylesVersion); a warm `View` renders nothing and
touches only the visible rows. After each `View`, the lines of items
entirely outside `[yOffset−2h, yOffset+3h)` are evicted; heights and
search-match results stay, so offsets never need a re-render.

## Adding a tool, transform, or hook

Everything a built-in registers goes through `internal/app/registry.go`'s
`buildRegistry`, in one of six `addX(r *ext.Registry, d registryDeps) error`
steps. Add your new extension's constructor to the relevant step's slice;
`registryDeps` already carries the collaborators (store, bus, agents,
skills, ...) most extensions need.

**A tool** (`ext.Tool`, optionally `ext.Subjecter` if it takes a
permission-relevant path or command):
1. Implement it in the right `service/...` package (existing tools live in
   `service/tools`, `service/skills`, `service/task`; a new capability
   gets its own package if it needs its own state).
2. Give it a permission default in `permission.Defaults()`
   (`internal/service/permission/rules.go`) if it should ever be
   allowed/denied by name rather than falling back to `ask`.
3. Construct it and append it to `addTools`'s `all` slice in
   `internal/app/registry.go`.

**A `ContextTransform`** (mutates the outgoing `core.LLMRequest`, e.g. to
inject prompt sections):
1. Implement `Priority()` and `Transform(ctx, rc, req)` in a
   `service/...` package (see `service/prompt` for the existing ones).
2. Pick a `Priority()` relative to the others in `addTransforms` — lower
   runs first.
3. Append it to `addTransforms`'s `all` slice in
   `internal/app/registry.go`.

**A `ToolHook`** (observes or intercepts every tool call's `Before`/`After`):
1. Implement `ext.ToolHook` in a `service/...` package (see
   `service/permission` for the existing one).
2. Register it in `addHooks` in `internal/app/registry.go` via
   `r.AddToolHook(...)`. Hooks run in registration order; a `Block`
   verdict from an earlier hook short-circuits later ones.
3. If it needs to remember state across calls, give it its own mutex —
   it may run concurrently with other tool calls in the same run.

## Adding a picker action

jig has no slash commands: every action is reached through the ctrl+p
picker or a key bound to it. `internal/ui/actions` is the catalogue —
jig's built-in actions (`actions.ID`, spec order, grouped Session /
Agent & model / Prompt / Transcript / View / App) plus one entry per
registered `ext.Command`, shown as `ext.<name>` in the "Extensions"
group.

1. An `ext.Command` (`Name`, `Description`, `Run`) becomes a picker
   action automatically: implement it in a `service/...` package and
   append it to `addCommands`'s `all` slice in
   `internal/app/registry.go`. It never becomes a slash command.
2. A key is an `ext.Keybind{Mode, Key, Command}` (`Command` names an
   `actions.ID`, either a built-in or an `ext.<name>`). Built-in default
   bindings live in `actions.DefaultBindings()`, registered through
   `addKeybinds`; config remaps them via `[keybinds]` (`"<mode>.<key>" =
   "<action id>"`), resolved by `actions.Resolve`.
