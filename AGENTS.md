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
`Compact` summarizes history behind the Runner's busy exclusion),
`core.SessionService` (implemented by `service/session`, covering listing,
`Rename`, and `Configure`), and `core.PermissionService` (implemented by
`permission.BusAsker`). `chat.Send` and `chat.Compact` failures caused by
configuration or input (including a missing session) are `*chat.ConfigError`
(headless exit 2).

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
internal/client/catalog/                catwalk catalog
internal/client/llm/                    fantasy adapter + model Source
internal/client/llm/jigtest/            scripted provider (build tag jigtest)
internal/client/shell/                  process execution
internal/client/search/                 rg + Go fallback glob/grep
internal/service/agents/                builtins, merge, model resolution, tool filtering
internal/service/permission/            rules, evaluation, ToolHook, askers
internal/service/tools/                 read/write/edit/bash/glob/grep/todo
internal/service/prompt/                ContextTransforms
internal/service/skills/                skills service, list transform, skill tool
internal/service/agent/                 Runner (agent loop)
internal/service/task/                  task tool (subagents)
internal/service/session/               sessions, history, compaction, titles
internal/service/chat/                  ChatService facade the UIs call
internal/ui/plain/                      headless renderer (io.Writer)
internal/ui/transcript/                 (Plan 2) transcript projection, core-only
internal/bubbles/                       (Plan 2) Bubble Tea widgets and helpers
internal/bubbles/ansi/                  sanitize untrusted text; ANSI-safe width/wrap/highlight
internal/bubbles/overlay/               center a box over a dimmed background (ported from slk)
internal/bubbles/scrollbar/             1-column proportional scrollbar gutter (ported from slk)
internal/bubbles/wintree/               window split tree, pure geometry (ported from slk)
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
  when the command has shell metacharacters. Session grants apply only
  to `ask`.
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
  styling escapes reach the terminal.
- `core.Media.Data` is never persisted; only `client/llm` fills it, from
  blobs, for a single request.
- `GenerateTitle` never overwrites a non-placeholder title.
- Stored `Session.Cwd` is `pathid.Key(workDir)`.

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
| Build a `core.ToolResult` for a tool's `Run` | `core.ToolError(call, msg)` (sets `IsError`) / `core.ToolOK(call, output)` |
| Resolve a tool's `path` input against `rc.WorkDir` | `resolvePath(workDir, path)` in `service/tools` (also backs `subjectPath` for `ext.Subjecter`) |
| Permission subject for a search tool's `path` input | `searchSubject(rc, input)` in `service/tools` |
| Resolve a model string (ref or alias) | `agents.ParseRef(s, cfg.ModelAliases)` / `(*agents.Service).ResolveRef(s)` |
| Session events/messages → display blocks | `transcript.New(root)`, `Load`, `Apply` |
| Golden-frame assertion | `golden.Assert(t, name, got)`; update with `JIG_UPDATE_GOLDEN=1` |
| Make untrusted text (model/tool/file/store) safe to render | `ansi.Sanitize(s)` in `internal/bubbles/ansi` (keeps `\n`, `\t`) |
| Same, for single-line contexts (titles, paths, list rows) | `ansi.SanitizeLine(s)` (`\n`/`\t` → space) |
| Highlight a search query in styled text without touching escapes | `ansi.Highlight(s, query, on, off)` |
| Wrap styled text to a width, hard-breaking long words | `ansi.Wrap(s, width)` (also `ansi.Width`/`Truncate`/`Cut`) |
| Center a modal box over a dimmed background | `overlay.Center(background, width, height, box, dim)` in `internal/bubbles/overlay` |
| Overlay a proportional scrollbar gutter onto rendered rows | `scrollbar.Overlay(visible, width, total, yOffset, visibleHeight, bg, trackFg, thumbFg)` / `scrollbar.Visible(total, visibleHeight)` in `internal/bubbles/scrollbar` |
| Vim-style window split tree (layout, split/close/navigate) | `wintree.New()` / `(*Tree).Split`, `.Close`, `.Only`, `.Cycle`, `.NavigateDir`, `.SetFixed`, `.Layout`, `.ComputeRects` in `internal/bubbles/wintree` |

## Adding a tool, transform, or hook

Everything a built-in registers goes through `internal/app/registry.go`'s
`buildRegistry`, in one of four `addX(r *ext.Registry, d registryDeps) error`
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
