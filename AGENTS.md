# jig — agent notes

## Build, test, lint

```
make build      # go build -o bin/jig ./cmd/jig
make test       # go test ./... -race
make lint       # golangci-lint run
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
`core.ChatService` (implemented by `service/chat`), `core.SessionService`
(implemented by `service/session`), and `core.PermissionService`
(implemented by `permission.BusAsker`). `chat.Send` failures caused by
configuration or input are `*chat.ConfigError` (headless exit 2).

```
cmd/jig/main.go                         entry: os.Exit(app.Run(...))
internal/clock/                         Clock interface, Real, Fake
internal/ids/                           sortable ID generator
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
internal/app/                           composition root + CLI
e2e/                                    end-to-end tests against the built binary
```

Dependency rules (spec §3.2), enforced by `internal/archtest`:

- `ui/...` imports only `core/...` and `ui/...`. It does no I/O of its own:
  no `os/exec`, `net/http`, `database/sql`, file writes, `client/...`, or
  `data/...`.
- `service/...` imports `core/...` and declares small interfaces for the
  client and data capabilities it uses. It never imports concrete
  `client/...` or `data/...` types.
- `client/...` and `data/...` never import `service/...` or `ui/...`.
- Only `internal/app` imports concrete implementations from every layer.

## Architecture tests

`internal/archtest` parses every `.go` file in the repo and enforces the
layer-import rules and size limits above, plus a no-package-mutable-vars
and no-`time.Sleep`/`time.Now`-in-tests hygiene check. Exceptions go in
`internal/archtest/allowlist.go` with a justification.

## Invariants

- No I/O in `ui`.
- The extension registry is frozen after startup: it is populated once and
  then read without locks.
- All built-ins register through `ext.Registry`; runtime code depends on
  `ext.View`.
- All time comes from `internal/clock`; no `time.Now()` or `time.Sleep` in
  `_test.go` files.
- Tools return `IsError` results, not Go errors, except for ctx
  cancellation.
- The agent Runner writes exactly one assistant message per model step; the
  results of that step's tool calls are parts on the same message.
- The model sees `session.Service.History`: the last message holding a
  `PartCompaction` and everything after it (all messages if none).
  Compaction and title requests run on `agents.SmallModel(resolved model)`.
- Session read-modify-writes (`Touch`, title save) go through
  `session.Service`'s mutex; don't `Get`+`Update` a session concurrently
  with a run.
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

## Shared code — check here before writing a helper

| Need | Use |
|---|---|
| Time / sleeping / timeouts | `clock.Clock`, `clock.NewFake` |
| Sortable, prefixed IDs | `ids.Gen` (`ids.New(clk, rnd)`, `g.Next("ses")`) |
| Value types shared across layers (Message, Session, ModelRef, Agent, Config, Rule, ...) | `internal/core` |
| Service ports the UIs call (`ChatService`, `SessionService`, `PermissionService`) | `internal/core` (`ports.go`) |
| Fake LLM for service tests | `llmtest.New(llmtest.Text(...), ...)` |
| One-shot, tool-less LLM call returning joined text | `agent.Complete(ctx, llm, system, user)` |
| Session title placeholder (first line, ≤50 runes) | `session.PlaceholderTitle(text)` |
| Expand a leading `~`/`~/` in a config path | `paths.ExpandHome(p, home)` |
| Find a project's git root from a directory | `fsroot.GitRoot(dir)` |
| Walk root→leaf ancestor directories for context/skill discovery | `fsroot.Chain(root, dir)` |
| Parse a `---\n<yaml>\n---\n<body>` file | `frontmatter.Parse(src, &meta)` |
