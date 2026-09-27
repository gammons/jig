# jig Plan 1 — Foundation and Headless Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build every non-UI layer of jig and ship `jig run "prompt"`: a headless agent with tools, permissions, skills, markdown agents, per-agent models, subagents, and SQLite sessions, verified end to end against a scripted provider.

**Architecture:** Four layers (UI, service, client, data) around `internal/core` ports and an extension registry that all built-ins register through. Services talk to the outside world only through small interfaces they declare. The UI learns about progress through an in-process event bus. `internal/app` is the only package that knows the concrete types. Plan 2 (the TUI, spec §6.3) builds on the ports this plan produces.

**Not in this plan (Plan 2):** the bubbletea TUI, the `Command`/`Keybind` registrants (`/new`, `/model`, and so on, plus the vim keymap), the interactive `BusAsker` UI, and the pickers. This plan defines the extension points and ports those features consume.

**Tech Stack:** Go 1.27, `charm.land/fantasy` v0.45.x (LLM client), `charm.land/catwalk` v0.52.x (model catalog), `modernc.org/sqlite`, `github.com/BurntSushi/toml`, `gopkg.in/yaml.v3`, ripgrep at runtime (optional).

**Spec:** `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md`

## Global Constraints

- Module `github.com/gammons/jig`; binary `jig`; `go 1.27`; license MIT.
- Allowed third-party deps in this plan: fantasy, catwalk, modernc.org/sqlite, BurntSushi/toml, yaml.v3. Adding anything else requires a line in the task explaining why.
- Tests: stdlib `testing` only; no testify/gomock; white-box (`package x`, not `x_test`) unless a task says otherwise.
- No `time.Sleep` and no `time.Now()` in any `_test.go`; time comes from `internal/clock`.
- Layer import rules are spec §3.2 exactly; `internal/archtest` enforces them from Task 2 onward.
- Limits (spec §7): `cmd/jig/main.go` ≤ 60 lines; `internal/app` functions ≤ 40 lines; non-test files ≤ 500 lines; `funlen` 80; `gocognit` 30; structs ≤ 15 fields and ≤ 20 methods; no package-level mutable vars.
- Paths: config `$XDG_CONFIG_HOME/jig/config.toml` (default `~/.config`), DB `$XDG_DATA_HOME/jig/jig.db` (default `~/.local/share`), catalog cache `$XDG_CACHE_HOME/jig/catalog.json` (default `~/.cache`).
- Numbers from the spec: bash timeout 2 min default, 10 min max, output truncated to the last 30 KB; skill file list ≤ 50; subagent depth ≤ 3; provider retries 3; catalog refresh at most once per 24 h.
- Headless exit codes: 0 success, 1 run failed, 2 config error.
- Every commit passes: `go test ./... -race`, `go vet ./...`, `gofmt -l .` (empty), `golangci-lint run`.
- `AGENTS.md` is updated in the same commit as any new shared helper, port, or invariant.

## Review Focus

1. **A cancelled or crashed run leaves a `tool_call` part with no result.** The next request must still be accepted by the provider. The converter synthesizes an error result `"interrupted"` for every call without one. Tested in Task 11 (`TestToFantasy_SynthesizesMissingToolResults`), Task 20 (`TestRunner_CancelledRunPairsEveryToolCall`), and Task 24 (`TestE2E_ResumeAfterCancelledRun`).
2. **The model sends malformed tool-input JSON or an unknown tool name.** This must become an `is_error` tool result that the model can recover from, never a crash or a failed run. Tested in Task 20 (`TestRunner_UnknownToolBecomesErrorResult`, `TestRunner_MalformedInputBecomesErrorResult`).
3. **A file changes on disk (the user is editing it) between `read` and `edit`/`write`.** The edit is refused with a message telling the model to read the file again. Tested in Task 16 (`TestEdit_RefusesWhenFileChangedSinceRead`).
4. **A typo'd model ID, or a provider with no API key.** This fails before any network request, with a message naming the env var or config key, and headless mode exits with 2. Tested in Task 11 (`TestSource_MissingCredentialsNamesEnvVar`) and Task 24 (`TestE2E_MissingCredentialsExits2`).
5. **The run is cancelled while a permission prompt is pending, possibly with parallel tool calls.** The run stops, the waiting hook returns, the results are recorded as cancelled, and no goroutine leaks. Tested in Task 14 (`TestAsk_ContextCancelUnblocks`) and Task 20 (`TestRunner_CancelDuringPermissionPrompt`).

## File Map

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

---

### Task 1: Scaffold, tooling, and clock

**Files:**
- Create: `go.mod`, `cmd/jig/main.go`, `Makefile`, `.golangci.yml`, `.github/workflows/ci.yml`, `LICENSE`, `AGENTS.md`, `.gitignore`
- Create: `internal/clock/clock.go`, `internal/clock/fake.go`
- Test: `internal/clock/fake_test.go`

**Interfaces:**
- Produces:
  ```go
  package clock
  type Clock interface {
      Now() time.Time
      After(d time.Duration) <-chan time.Time
  }
  func Real() Clock
  type Fake struct{ /* mutex-guarded now + waiters */ }
  func NewFake(start time.Time) *Fake
  func (f *Fake) Advance(d time.Duration)       // fires every waiter whose deadline is reached
  func (f *Fake) BlockUntilWaiters(n int)       // blocks until ≥ n After() calls are pending; uses sync.Cond, not polling
  ```

- [ ] **Step 1: Scaffold the repo.** Run `go mod init github.com/gammons/jig`. Set `go 1.27` in `go.mod`. Write `cmd/jig/main.go`, which prints `jig: not implemented` and exits 2 (it is replaced in Task 23). Add the MIT `LICENSE` (Copyright 2026 Grant Ammons) and a `.gitignore` covering `/jig`, `/bin`, `*.db`, and `testdata/**/*.actual`.
- [ ] **Step 2: Add the tooling config.**
  - `.golangci.yml`: v2 schema, `default: standard`, errcheck disabled (as in slk), plus `funlen` (lines: 80, statements: -1) and `gocognit` (min-complexity: 30), with `gofmt` as the formatter.
  - `Makefile` targets: `build`, `test` (`go test ./... -race`), `lint`, `fmt-check`, and `check` (all of them).
  - `.github/workflows/ci.yml`: runs `make check` on ubuntu-latest and macos-latest, and installs `golangci-lint` v2.13.1 and ripgrep.
- [ ] **Step 3: Write the failing tests** in `internal/clock/fake_test.go`:
  - `TestFake_AfterFiresOnAdvance`: `ch := f.After(5*time.Second)`. After `f.Advance(4*time.Second)` the channel is not ready (select with default). After `f.Advance(time.Second)` it receives `start.Add(5*time.Second)`.
  - `TestFake_NowAdvances`: `f.Now()` equals `start + total advanced`.
  - `TestFake_BlockUntilWaiters`: a goroutine calls `After(time.Minute)`. `BlockUntilWaiters(1)` returns, then `Advance(time.Minute)` delivers.
- [ ] **Step 4: Run** `go test ./internal/clock/`. Expected: FAIL (undefined: NewFake).
- [ ] **Step 5: Implement** `clock.go` and `fake.go`. Build the fake on a waiter slice plus `sync.Cond`.
- [ ] **Step 6: Run** `go test ./internal/clock/ -race`. Expected: PASS.
- [ ] **Step 7: Write the `AGENTS.md` skeleton** with these sections:
  - "Build, test, lint" (the Makefile targets).
  - "Architecture in one screen" (paste the File Map above plus the spec §3.2 rules).
  - "Invariants" (no I/O in `ui`, the registry is frozen after startup, all time comes from `clock`).
  - "Shared code — check here before writing a helper", a table with the row `clock.Clock` / `clock.NewFake`.
- [ ] **Step 8: Commit.** `git add -A && git commit -m "chore: scaffold jig module, tooling, and clock"`

### Task 2: Architecture tests

**Files:**
- Create: `internal/archtest/archtest.go` (repo walking + parsing helpers), `internal/archtest/allowlist.go`
- Test: `internal/archtest/layers_test.go`, `internal/archtest/size_test.go`, `internal/archtest/hygiene_test.go`

**Interfaces:**
- Produces:
  ```go
  package archtest
  type File struct{ Path string; Pkg string; AST *ast.File; Lines int; IsTest bool; Generated bool }
  func LoadRepo(t testing.TB) []File                  // finds go.mod upward, parses every .go file except testdata/
  var allowlist = map[string]string{}                   // rule+":"+path → justification (the one sanctioned package var)
  ```

- [ ] **Step 1: Write the failing tests.** Each test iterates `LoadRepo(t)` and reports every violation as `path:line: rule: detail`.
  - `TestLayers_UIImportsOnlyCoreAndUI`: files under `internal/ui/` may import stdlib, `internal/core/...`, `internal/ui/...`, `internal/clock`, and third-party `charm.land/...` UI packages. They may not import `os/exec`, `net/http`, `database/sql`, `internal/client/...`, `internal/data/...`, or `internal/service/...`.
  - `TestLayers_ServiceDoesNotImportConcreteClientOrData`: `internal/service/...` must not import `internal/client/...`, `internal/data/...`, `internal/ui/...`, `charm.land/fantasy`, `database/sql`, or `modernc.org/sqlite`.
  - `TestLayers_ClientAndDataDoNotImportUpward`: `internal/client/...` and `internal/data/...` must not import `internal/service/...`, `internal/ui/...`, or `internal/app`.
  - `TestLayers_OnlyAppImportsEverything`: only `internal/app`, `cmd/jig`, and `e2e` may import both a `client/` package and a `service/` package.
  - `TestSize_MainGo`: `cmd/jig/main.go` is ≤ 60 lines.
  - `TestSize_SourceFiles`: non-test, non-generated files are ≤ 500 lines unless `allowlist["filesize:"+path]` exists.
  - `TestSize_AppFunctions`: every func in `internal/app` is ≤ 40 lines.
  - `TestSize_Structs`: struct types have ≤ 15 fields, and the methods declared on each receiver type across its package number ≤ 20.
  - `TestHygiene_NoPackageMutableVars`: top-level `var` is allowed only for `var _ T = ...` assertions, values created with `errors.New`/`fmt.Errorf` whose name starts with `Err`, `//go:embed` targets, and `allowlist` in archtest itself.
  - `TestHygiene_NoSleepOrNowInTests`: `_test.go` files may not call `time.Sleep` or `time.Now`.
  - `TestAllowlist_EntriesHaveJustification`: every allowlist value is non-empty and every key names a file that exists.
- [ ] **Step 2: Run** `go test ./internal/archtest/`. Expected: FAIL (undefined: LoadRepo).
- [ ] **Step 3: Implement `LoadRepo`** using `go/parser.ParseFile(fset, path, nil, parser.ParseComments)`. A file counts as generated when it has a `// Code generated ... DO NOT EDIT.` comment.
- [ ] **Step 4: Run** `go test ./internal/archtest/ -race`. Expected: PASS on the current tree.
- [ ] **Step 5: Self-check that the tests bite.** Temporarily add `import _ "os/exec"` to a scratch `internal/ui/x.go` and confirm `TestLayers_UIImportsOnlyCoreAndUI` fails. Then delete the scratch file.
- [ ] **Step 6: Update `AGENTS.md`.** Add an "Architecture tests" note: "exceptions go in `internal/archtest/allowlist.go` with a justification".
- [ ] **Step 7: Commit.** `git commit -am "test: add architecture tests enforcing layers and size limits"`

### Task 3: Core value types, ports, and IDs

**Files:**
- Create: `internal/ids/ids.go`, `internal/core/message.go`, `internal/core/session.go`, `internal/core/model.go`, `internal/core/llm.go`, `internal/core/agent.go`, `internal/core/config.go`, `internal/core/skill.go`, `internal/core/ports.go`
- Test: `internal/ids/ids_test.go`, `internal/core/model_test.go`, `internal/core/config_test.go`

**Interfaces:**
- Produces (later tasks use these names exactly):
  ```go
  package ids
  type Gen struct{ /* clock + rand */ }
  func New(clk clock.Clock, rnd io.Reader) *Gen
  func (g *Gen) Next(prefix string) string   // "ses_" + 12 hex of unix-ms + 10 base32 random; lexically sortable by time

  package core
  type SessionID string
  type MessageID string
  type Role string            // RoleUser = "user", RoleAssistant = "assistant"
  type PartKind string        // PartText, PartReasoning, PartToolCall, PartToolResult, PartCompaction ("text","reasoning","tool_call","tool_result","compaction")
  type MessageStatus string   // StatusStreaming, StatusComplete, StatusInterrupted, StatusFailed
  type ToolCall struct{ ID, Name string; Input json.RawMessage }
  type ToolResult struct{ CallID, Name, Output string; IsError bool; Metadata map[string]string }
  type Part struct{ Kind PartKind; Text string; Call *ToolCall; Result *ToolResult }
  type Usage struct{ Input, Output, CacheRead, CacheWrite int64 }
  type Message struct{ ID MessageID; SessionID SessionID; Role Role; Agent, Model string; Parts []Part; Usage Usage; CostUSD float64; Status MessageStatus; CreatedAt time.Time }
  type Session struct{ ID, ParentID SessionID; Title, Agent, Model, Cwd string; CreatedAt, UpdatedAt time.Time }
  type Todo struct{ Content, Status string }   // Status: "pending" | "in_progress" | "completed"

  type ModelRef struct{ Provider, Model string }
  func ParseModelRef(s string) (ModelRef, error)   // "anthropic/claude-haiku-4-5"; splits on the FIRST "/" so "openrouter/anthropic/claude" → {openrouter, anthropic/claude}
  func (m ModelRef) String() string
  func (m ModelRef) IsZero() bool
  type ModelInfo struct{ Ref ModelRef; Name string; ContextWindow, DefaultMaxTokens int64; CostIn, CostOut, CostCacheRead, CostCacheWrite float64; CanReason bool }
  func (m ModelInfo) Cost(u Usage) float64     // per-1M-token prices × usage
  type ProviderInfo struct{ ID, Name, Type, APIKeyEnv, Endpoint string; Models []ModelInfo }

  type ToolSpec struct{ Name, Description string; Schema map[string]any }
  type LLMRequest struct{ Model ModelRef; System []string; Messages []Message; Tools []ToolSpec; MaxOutputTokens int64 }
  type StreamKind string   // StreamText, StreamReasoning, StreamToolCall, StreamFinish
  type StreamEvent struct{ Kind StreamKind; Text string; Call *ToolCall; Usage Usage; FinishReason string }
  type LLM interface{ Stream(ctx context.Context, req LLMRequest) iter.Seq2[StreamEvent, error] }
  type LLMError struct{ Retryable bool; RetryAfter time.Duration; Err error }   // Error(), Unwrap()

  type AgentMode string    // ModePrimary, ModeSubagent, ModeAll
  type Action string       // Allow = "allow", Ask = "ask", Deny = "deny"
  type Rule struct{ Default Action; Patterns map[string]Action }
  func (r *Rule) UnmarshalTOML(v any) error      // accepts "deny" or {"git status*"="allow", ...}
  type PermissionRules map[string]Rule           // tool name → rule
  type Agent struct{ Name, Description, Prompt string; Mode AgentMode; Model ModelRef; ModelAlias string; MaxSteps int; CanSpawn bool; Hidden bool; Tools []string; Permissions PermissionRules }

  type Config struct {        // immutable snapshot; see Task 8 for TOML keys
      DefaultModel, SmallModel string
      Providers    map[string]ProviderConfig
      Agents       map[string]AgentConfig
      ModelAliases map[string]string
      Permissions  PermissionRules
      SkillPaths, Instructions []string
      Keybinds     map[string]string
      Theme        string
  }
  type ProviderConfig struct{ Type, APIKey, BaseURL string; Models []string; Options map[string]any }
  type AgentConfig struct{ Description, Mode, Model, Prompt string; MaxSteps int; CanSpawn, Hidden *bool; Tools []string; Permissions PermissionRules; Source string }
  type Skill struct{ Name, Description, Dir, Path string }
  ```
- `ports.go` holds the service ports the UIs call. Plan 2 depends on this file:
  ```go
  type SendRequest struct{ SessionID SessionID; Agent, Model, Text string }
  type SendResult struct{ SessionID SessionID; Message Message }
  type ChatService interface {
      Send(ctx context.Context, req SendRequest) (SendResult, error)
      Cancel(id SessionID)
      Close(ctx context.Context) error
  }
  type SessionService interface {
      List(ctx context.Context, limit int) ([]Session, error)       // roots only, newest first
      Get(ctx context.Context, id SessionID) (Session, error)
      Messages(ctx context.Context, id SessionID) ([]Message, error)
      Compact(ctx context.Context, id SessionID) error
  }
  type PermissionReply struct{ Kind ReplyKind; Message string }     // ReplyOnce, ReplyAlways, ReplyDeny
  type PermissionService interface{ Reply(requestID string, r PermissionReply) error }
  ```
  `ReplyKind` is a `string` type.

- [ ] **Step 1: Write the failing tests.**
  - `TestGen_SortableAndPrefixed`: using a fake clock, two IDs generated 1 ms apart compare `a < b`, and both start with `"ses_"`.
  - `TestParseModelRef`: the table includes `"anthropic/claude-haiku-4-5"`, `"openrouter/anthropic/claude-sonnet-4"` (Model `"anthropic/claude-sonnet-4"`), and the errors `""`, `"noslash"`, and `"/x"`.
  - `TestModelInfo_Cost`: `ModelInfo{CostIn:3, CostOut:15, CostCacheRead:0.3, CostCacheWrite:3.75}.Cost(Usage{Input:1e6, Output:1e6, CacheRead:1e6, CacheWrite:1e6})` equals `22.05`.
  - `TestRule_UnmarshalTOML`: decoding `write = "deny"` gives `Rule{Default: Deny}`. Decoding `bash = { "git status*" = "allow", "*" = "ask" }` gives two patterns and an empty Default. The invalid action `"maybe"` is an error naming the value.
- [ ] **Step 2: Run** `go test ./internal/ids/ ./internal/core/`. Expected: FAIL (undefined symbols).
- [ ] **Step 3: Implement the types.** `core` contains only types and the small value methods listed above. It gets no service logic and imports nothing from `internal/` except `clock` (and only in `ids`).
- [ ] **Step 4: Run** `go test ./internal/ids/ ./internal/core/ -race`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(core): value types, LLM port, service ports, ids"`

### Task 4: Event bus

**Files:**
- Create: `internal/core/event/event.go`, `internal/core/event/bus.go`
- Test: `internal/core/event/bus_test.go`

**Interfaces:**
- Consumes: `core.SessionID`, `core.MessageID`, `core.ToolCall`, `core.ToolResult`, `core.Session`, `core.Usage`, `core.Todo`, `core.PermissionReply`.
- Produces:
  ```go
  package event
  type Event interface{ Session() core.SessionID }
  // All events embed Base{SessionID core.SessionID}.
  type SessionCreated struct{ Base; Info core.Session }
  type MessageStarted struct{ Base; MessageID core.MessageID; Agent, Model string }
  type TextDelta struct{ Base; MessageID core.MessageID; Text string }
  type ReasoningDelta struct{ Base; MessageID core.MessageID; Text string }
  type ToolCallStarted struct{ Base; MessageID core.MessageID; Call core.ToolCall }
  type ToolCallFinished struct{ Base; MessageID core.MessageID; Result core.ToolResult }
  type PermissionRequested struct{ Base; RequestID, Tool, Subject string; Call core.ToolCall }
  type PermissionResolved struct{ Base; RequestID string; Reply core.PermissionReply }
  type SubagentSpawned struct{ Base; Child core.SessionID; Agent, Description string }
  type TodosUpdated struct{ Base; Todos []core.Todo }
  type RunFinished struct{ Base; MessageID core.MessageID; Usage core.Usage; CostUSD float64 }
  type RunFailed struct{ Base; Err string }

  type Publisher interface{ Publish(Event) }
  type Bus struct{ /* subscribers */ }
  func NewBus() *Bus
  func (b *Bus) Publish(e Event)          // never blocks
  func (b *Bus) Subscribe() *Subscription
  type Subscription struct{ /* pending queue + out chan */ }
  func (s *Subscription) C() <-chan Event
  func (s *Subscription) Close()
  ```
- **Delivery algorithm.** It is decided here because the tests do not force it.
  - Each subscription owns a mutex-guarded pending queue and one pump goroutine that forwards the queue head to an unbuffered `out` channel.
  - `Publish` appends to every subscription's queue.
  - If the event being appended is a `TextDelta` or `ReasoningDelta`, and the queue's last *undelivered* element has the same type and the same `MessageID`, the event's text is concatenated onto that element instead of being appended.
  - Nothing is ever dropped. `Close` stops the pump and closes `out`.

- [ ] **Step 1: Write the failing tests.**
  - `TestBus_DeliversInOrder`: publish MessageStarted, ToolCallStarted, then RunFinished, and receive them in that order.
  - `TestBus_MergesAdjacentDeltasWhenSubscriberLags`: publish 100 `TextDelta{Text:"a"}` for the same message *before* reading anything, then read until a `RunFinished` arrives. The concatenated text is 100 `a`s, and the number of events received is < 100.
  - `TestBus_DoesNotMergeAcrossMessagesOrKinds`: TextDelta(m1), TextDelta(m2), ReasoningDelta(m2), then TextDelta(m2) arrive as 4 events.
  - `TestBus_NeverMergesLifecycle`: 50 `ToolCallFinished` events arrive as 50 events.
  - `TestBus_CloseStopsDelivery`: after `Close`, `C()` is closed and `Publish` does not panic or block.
  - `TestBus_SubscribersIndependent`: a subscriber that never reads does not stop another subscriber from receiving.
- [ ] **Step 2: Run** `go test ./internal/core/event/`. Expected: FAIL.
- [ ] **Step 3: Implement** `event.go` and `bus.go` using the algorithm above.
- [ ] **Step 4: Run** `go test ./internal/core/event/ -race -count=5`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(event): typed events and coalescing bus"`

### Task 5: Extension registry

**Files:**
- Create: `internal/core/ext/ext.go` (the interfaces), `internal/core/ext/registry.go`
- Test: `internal/core/ext/registry_test.go`

**Interfaces:**
- Consumes: the `core` types from Task 3 and `event.Event`.
- Produces:
  ```go
  package ext
  type RunContext struct{ SessionID, RootID core.SessionID; MessageID core.MessageID; Agent core.Agent; Model core.ModelRef; WorkDir string; Depth int }
  type Tool interface {
      Name() string
      Description() string
      Schema() map[string]any
      Concurrent() bool
      Run(ctx context.Context, rc RunContext, call core.ToolCall) (core.ToolResult, error)
  }
  type Subjecter interface{ Subject(input json.RawMessage) string }   // optional; the permission subject
  type Verdict struct{ Block bool; Reason string }
  type ToolHook interface {
      Before(ctx context.Context, rc RunContext, tool Tool, call core.ToolCall) (core.ToolCall, Verdict, error)   // tool is resolved by the Runner, so hooks never need the registry
      After(ctx context.Context, rc RunContext, tool Tool, call core.ToolCall, res core.ToolResult) core.ToolResult
  }
  type ContextTransform interface{ Priority() int; Transform(ctx context.Context, rc RunContext, req *core.LLMRequest) error }
  type EventSubscriber interface{ Handle(ctx context.Context, e event.Event) }
  type Command interface{ Name() string; Description() string; Run(ctx context.Context, args []string) error }
  type Keybind struct{ Mode, Key, Command string }
  type ProviderFactory interface{ Type() string; New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) }

  var ErrFrozen = errors.New("ext: registry is frozen")
  type Registry struct{ /* slices + maps + frozen bool */ }
  func NewRegistry() *Registry
  func (r *Registry) AddTool(t Tool) error              // duplicate name → error naming it
  func (r *Registry) AddToolHook(h ToolHook) error
  func (r *Registry) AddTransform(t ContextTransform) error
  func (r *Registry) AddSubscriber(s EventSubscriber) error
  func (r *Registry) AddCommand(c Command) error         // duplicate → error
  func (r *Registry) AddKeybind(k Keybind) error         // same Mode+Key → later wins
  func (r *Registry) AddProvider(p ProviderFactory) error // duplicate Type → error
  func (r *Registry) Freeze() View                       // Add* returns ErrFrozen afterwards

  // Read side. Runtime code only ever receives a View, so it cannot mutate the registry.
  type View struct{ /* snapshot */ }
  func (v View) Tools() []Tool                           // sorted by Name
  func (v View) Tool(name string) (Tool, bool)
  func (v View) ToolHooks() []ToolHook                   // registration order
  func (v View) Transforms() []ContextTransform          // stable-sorted by Priority ascending
  func (v View) Subscribers() []EventSubscriber
  func (v View) Commands() []Command
  func (v View) Keybinds() []Keybind
  func (v View) Provider(typ string) (ProviderFactory, bool)
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestRegistry_DuplicateToolRejected`
  - `TestRegistry_AddAfterFreezeFails`: every `Add*` returns `ErrFrozen` after `Freeze`.
  - `TestView_TransformsSortedByPriorityStable`: priorities 30, 10, 30, 20 come back as 10, 20, 30(first), 30(second).
  - `TestView_ToolsSortedByName`
  - `TestView_KeybindLaterWins`
  - `TestView_ConcurrentReads`: 50 goroutines call `Tools()` under `-race`.
- [ ] **Step 2: Run** `go test ./internal/core/ext/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/core/ext/ -race`. Expected: PASS.
- [ ] **Step 5: Update `AGENTS.md`.** Add the invariant "all built-ins register through `ext.Registry`; runtime code depends on `ext.View`".
- [ ] **Step 6: Commit.** `git commit -am "feat(ext): extension registry and frozen view"`

### Task 6: Scripted fake LLM

**Files:**
- Create: `internal/core/llmtest/llmtest.go`
- Test: `internal/core/llmtest/llmtest_test.go`

**Interfaces:**
- Produces:
  ```go
  package llmtest
  type Turn struct {
      Events []core.StreamEvent
      Err    error    // yielded after Events
      Hang   bool     // after Events, block until ctx is done, then yield ctx.Err()
  }
  func Text(s string) Turn                                   // one StreamText + StreamFinish{FinishReason:"stop"}
  func Calls(calls ...core.ToolCall) Turn                    // StreamToolCall per call + StreamFinish{"tool_calls"}
  func Call(id, name, inputJSON string) core.ToolCall
  type Client struct{ /* mu, turns, requests */ }
  func New(turns ...Turn) *Client
  func (c *Client) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error]
  func (c *Client) Requests() []core.LLMRequest               // copies, in call order
  ```
  Running out of turns yields the error `llmtest: no scripted turn for request N`. Every `StreamFinish` carries `Usage{Input:10, Output:5}` unless the turn sets its own.

- [ ] **Step 1: Write the failing tests.**
  - `TestClient_ReplaysTurnsInOrder`
  - `TestClient_RecordsRequests`
  - `TestClient_HangRespectsCancel`: cancel the ctx, and the iterator yields `context.Canceled`.
  - `TestClient_ExhaustedTurnsError`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Add an `AGENTS.md` shared-code row:** "Fake LLM for service tests → `llmtest.New(llmtest.Text(...), ...)`".
- [ ] **Step 6: Commit.** `git commit -am "test(llmtest): scripted fake LLM"`

### Task 7: Data helpers — paths, fsroot, frontmatter

**Files:**
- Create: `internal/data/paths/paths.go`, `internal/data/fsroot/fsroot.go`, `internal/data/frontmatter/frontmatter.go`
- Test: `paths_test.go`, `fsroot_test.go`, `frontmatter_test.go` (one in each package)

**Interfaces:**
- Produces:
  ```go
  package paths
  type Paths struct{ Home, ConfigDir, DataDir, CacheDir string }    // ConfigDir = $XDG_CONFIG_HOME/jig etc.
  func Resolve(getenv func(string) string) (Paths, error)          // error if HOME is unset and no XDG vars are set
  func ExpandHome(p, home string) string                            // "~/x" → home+"/x"

  package fsroot
  func GitRoot(dir string) (string, bool)                           // nearest ancestor containing .git (dir or file)
  func Chain(root, dir string) []string                            // root → … → dir, inclusive; [dir] if dir is not under root

  package frontmatter
  func Parse(src []byte, meta any) (body string, err error)         // "---\n<yaml>\n---\n<body>"; no frontmatter → meta untouched, body=src
  ```

- [ ] **Step 1: Write the failing tests.**
  - `TestResolve_XDGOverrides`
  - `TestResolve_DefaultsUnderHome`
  - `TestExpandHome`
  - `TestGitRoot_FindsAncestor`: in `t.TempDir()`, create `repo/.git` and `repo/a/b`, and `GitRoot("repo/a/b")` returns `repo`.
  - `TestGitRoot_WorktreeGitFile`: `.git` is a regular file.
  - `TestChain_OrderRootToLeaf`
  - `TestChain_OutsideRoot`
  - `TestParse_WithFrontmatter`
  - `TestParse_NoFrontmatter`
  - `TestParse_CRLF`
  - `TestParse_UnterminatedIsError`
- [ ] **Step 2: Run** `go test ./internal/data/...`. Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Add `AGENTS.md` shared-code rows** for `fsroot.GitRoot`, `fsroot.Chain`, `frontmatter.Parse`, and `paths.ExpandHome`.
- [ ] **Step 6: Commit.** `git commit -am "feat(data): paths, git-root walking, frontmatter parsing"`

### Task 8: Config loader

**Files:**
- Create: `internal/data/config/load.go` (discovery + decode), `internal/data/config/merge.go`, `internal/data/config/subst.go`
- Test: `load_test.go`, `merge_test.go`, `subst_test.go`, plus a `testdata/` tree

**Interfaces:**
- Consumes: `paths.Paths`, `fsroot.GitRoot`/`Chain`, `core.Config`, `core.Rule.UnmarshalTOML`.
- Produces:
  ```go
  package config
  type Loaded struct {
      Config        core.Config                      // everything merged
      Files         []string                         // files actually read, in merge order
      GlobalAgents  map[string]core.AgentConfig      // [agents] from the global file only
      ProjectAgents map[string]core.AgentConfig      // [agents] from project files, merged closer-wins
  }
  func Load(p paths.Paths, workDir string, getenv func(string) string) (Loaded, error)
  ```
- **TOML keys** (spec §6.2):
  - `default_model`, `small_model`, `theme`
  - `[providers.<id>]`: `type`, `api_key`, `base_url`, `models`, `options`
  - `[agents.<name>]`: `description`, `mode`, `model`, `prompt`, `max_steps`, `can_spawn`, `hidden`, `tools`, `[agents.<name>.permissions]`
  - `[model_aliases]`, `[permissions]`, `[skills] paths = [...]`, `instructions = [...]`, `[keybinds]`
- **Pipeline.**
  1. Read each file into `map[string]any`.
  2. Substitute `{env:VAR}` (unset → `""`) and `{file:path}` (the path is relative to that config file's directory with `~` expanded; a missing file is an error naming both paths) in every string, recursively.
  3. Deep-merge the maps.
  4. Re-encode and decode into `core.Config`.
- **Merge rules** (later files win):
  - Scalars are replaced.
  - `providers.<id>` and `agents.<name>` are merged field by field.
  - `permissions.<tool>`: `Default` is replaced if set, and `Patterns` keys are merged.
  - `instructions` and `skills.paths` are appended and de-duplicated.
- **Files, in merge order:** `ConfigDir/config.toml`, then `<dir>/.jig/config.toml` for each dir in `Chain(gitRoot, workDir)`. When there is no git root, only `workDir` is used. A missing file is skipped. A TOML syntax error returns an error containing the file path and line.

- [ ] **Step 1: Write the failing tests.**
  - `TestLoad_NoFilesGivesZeroConfig`
  - `TestLoad_ProjectOverridesGlobalScalar`
  - `TestLoad_AgentsMergeFieldwise`: global sets `explore.model`, project sets `explore.max_steps`, and both survive.
  - `TestLoad_PermissionPatternsMerge`
  - `TestLoad_InstructionsAppendDedup`
  - `TestLoad_NestedProjectDirsCloserWins`: `repo/.jig` and `repo/sub/.jig`, with workDir `repo/sub`.
  - `TestLoad_AgentsSplitByScope`: the global file defines `explore` and the project file defines `reviewer`. `GlobalAgents` has only `explore`, `ProjectAgents` has only `reviewer`, and `Config.Agents` has both.
  - `TestSubst_Env`
  - `TestSubst_FileRelativeToConfig`
  - `TestSubst_MissingFileErrorNamesPaths`
  - `TestLoad_SyntaxErrorNamesFile`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(config): TOML discovery, substitution, and merge"`

### Task 9: SQLite store

**Files:**
- Create: `internal/data/store/store.go` (Open, migrations, tx helper), `internal/data/store/migrations/0001_init.sql`, `sessions.go`, `messages.go`, `todos.go`
- Test: `store_test.go`, `sessions_test.go`, `messages_test.go`, `todos_test.go`

**Interfaces:**
- Produces:
  ```go
  package store
  type Store struct{ db *sql.DB }
  func Open(ctx context.Context, path string) (*Store, error)   // mkdir -p the parent, WAL, foreign_keys=ON, busy_timeout=5000, run embedded migrations
  func (s *Store) Close() error
  func (s *Store) CreateSession(ctx context.Context, sess core.Session) error
  func (s *Store) UpdateSession(ctx context.Context, sess core.Session) error          // title, agent, model, updated_at
  func (s *Store) GetSession(ctx context.Context, id core.SessionID) (core.Session, error)   // ErrNotFound
  func (s *Store) ListSessions(ctx context.Context, parent core.SessionID, limit int) ([]core.Session, error)  // parent "" = roots; newest updated_at first
  func (s *Store) SaveMessage(ctx context.Context, m core.Message) error               // upsert the message row and replace all its parts, in one tx
  func (s *Store) ListMessages(ctx context.Context, id core.SessionID) ([]core.Message, error)  // created_at, id order; parts by seq
  func (s *Store) ReplaceTodos(ctx context.Context, id core.SessionID, todos []core.Todo) error
  func (s *Store) ListTodos(ctx context.Context, id core.SessionID) ([]core.Todo, error)
  var ErrNotFound = errors.New("store: not found")
  ```
- **Schema:** the tables in spec §6.1. Times are stored as unix milliseconds (INTEGER). `parts.data_json` holds the JSON of `core.Part`'s `Call` or `Result`, or `{"text":...}`. A `schema_migrations(version)` table records applied migrations. `messages.session_id` and `parts.message_id` have `ON DELETE CASCADE`.

- [ ] **Step 1: Write the failing tests.** All use a real DB at `filepath.Join(t.TempDir(), "jig.db")`.
  - `TestOpen_MigratesIdempotently`: open, close, and open again succeeds.
  - `TestOpen_CreatesParentDir`
  - `TestSessions_CreateGetList`
  - `TestSessions_ListRootsExcludesChildren`
  - `TestSessions_GetMissingIsErrNotFound`
  - `TestMessages_SaveReplacesParts`: save with 2 parts and then with 3 parts, and a list returns 3 parts in seq order.
  - `TestMessages_RoundTripAllPartKinds`: text, reasoning, a tool_call with JSON input, a tool_result with `IsError` and metadata, and compaction.
  - `TestMessages_ConcurrentSavesDifferentSessions`: 20 goroutines under `-race`.
  - `TestTodos_Replace`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** Pass `modernc.org/sqlite` as driver `"sqlite"` and embed migrations with `//go:embed migrations/*.sql`. **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(store): SQLite sessions, messages, parts, todos"`

### Task 10: Model catalog (catwalk)

**Files:**
- Create: `internal/client/catalog/catalog.go`, `internal/client/catalog/convert.go`
- Test: `catalog_test.go`, `convert_test.go`

**Interfaces:**
- Consumes: `clock.Clock`, `core.ProviderInfo`, `core.ModelInfo`, and `core.ProviderConfig` (config-defined custom providers).
- Produces:
  ```go
  package catalog
  type Fetcher func(ctx context.Context, etag string) ([]catwalk.Provider, error)   // prod: catwalk.New().GetProviders
  type Options struct{ CachePath string; Clock clock.Clock; Fetch Fetcher; Custom map[string]core.ProviderConfig }
  type Catalog struct{ /* atomic.Pointer to snapshot */ }
  func New(o Options) *Catalog                                    // loads the cache if present and parseable, else embedded.GetAll()
  func (c *Catalog) Providers() []core.ProviderInfo               // catalog providers + custom ones, sorted by ID
  func (c *Catalog) Provider(id string) (core.ProviderInfo, bool)
  func (c *Catalog) Model(ref core.ModelRef) (core.ModelInfo, bool)
  func (c *Catalog) RefreshIfStale(ctx context.Context) error     // no-op if the cache mtime is < 24h old; ErrNotModified → touch the mtime; any error keeps the current snapshot
  ```
- **Conversion** (`convert.go`):
  - Strip the leading `$` from `catwalk.Provider.APIKey` to get `APIKeyEnv` (`"$ANTHROPIC_API_KEY"` → `"ANTHROPIC_API_KEY"`). A value without `$` gives an empty `APIKeyEnv`.
  - Pricing mapping (this matches crush's usage): `CostPer1MIn` → `CostIn`, `CostPer1MOut` → `CostOut`, `CostPer1MInCached` → `CostCacheWrite`, `CostPer1MOutCached` → `CostCacheRead`.
  - Custom providers from config produce `ProviderInfo{ID, Type: cfg.Type, Endpoint: cfg.BaseURL}` with one zero-cost `ModelInfo` per `cfg.Models` entry. If the ID already exists in the catalog, the config values override `Endpoint` and add models.

- [ ] **Step 1: Write the failing tests.**
  - `TestNew_FallsBackToEmbedded`: with no cache, `Provider("anthropic")` exists.
  - `TestNew_UsesCacheWhenPresent`
  - `TestNew_CorruptCacheFallsBack`
  - `TestRefresh_SkipsWhenFresh`: a fake clock and a Fetcher that counts calls.
  - `TestRefresh_WritesCacheAndSwaps`
  - `TestRefresh_FetchErrorKeepsSnapshot`
  - `TestRefresh_NotModifiedTouchesMtime`
  - `TestConvert_PricingMapping`
  - `TestConvert_APIKeyEnv`
  - `TestCustomProviderFromConfig`: `{type="openai-compat", base_url="http://localhost:11434/v1", models=["qwen3"]}` makes `Model({ollama, qwen3})` resolvable.
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** Use `charm.land/catwalk/pkg/embedded` and `pkg/catwalk`. **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(catalog): catwalk-backed model catalog with cached refresh"`

### Task 11: LLM client — fantasy adapter and model Source

**Files:**
- Create: `internal/client/llm/convert.go` (core → fantasy prompt), `internal/client/llm/stream.go` (fantasy → core events), `internal/client/llm/errors.go`, `internal/client/llm/providers.go` (ProviderFactory per type), `internal/client/llm/source.go`
- Test: `convert_test.go`, `stream_test.go`, `errors_test.go`, `providers_test.go` (httptest SSE fixtures in `testdata/anthropic/*.sse`), `source_test.go`

**Interfaces:**
- Consumes: `core.LLM`, `core.LLMRequest`, `core.StreamEvent`, `core.LLMError`, `ext.ProviderFactory`, `ext.View.Provider`, and `catalog.Catalog` (through a local interface).
- Produces:
  ```go
  package llm
  func ToFantasy(req core.LLMRequest) fantasy.Call
  func FromFantasy(parts fantasy.StreamResponse) iter.Seq2[core.StreamEvent, error]
  func MapError(err error) error                                   // *fantasy.ProviderError → *core.LLMError
  func Factories() []ext.ProviderFactory                          // types: "anthropic", "openai", "openai-compat", "openrouter", "google"
  type Catalog interface{ Provider(id string) (core.ProviderInfo, bool); Model(core.ModelRef) (core.ModelInfo, bool) }
  type Source struct{ /* catalog, registry view, cfg providers, getenv */ }
  func NewSource(cat Catalog, reg ext.View, providers map[string]core.ProviderConfig, getenv func(string) string) *Source
  func (s *Source) For(ref core.ModelRef) (core.LLM, core.ModelInfo, error)
  ```
- **`ToFantasy` rules:**
  - `System` strings become one system message with one `TextPart` each.
  - A user message becomes a `NewUserMessage`.
  - An assistant message becomes an assistant message with `TextPart`/`ReasoningPart`/`ToolCallPart`, followed by a `MessageRoleTool` message containing a `ToolResultPart` for each result. An error result uses `ToolResultOutputContentError`, otherwise `ToolResultOutputContentText`.
  - A compaction part becomes a user text message prefixed `"Summary of the conversation so far:\n"`.
  - **Every `ToolCallPart` without a matching result gets a synthesized error result `"interrupted"`** (Review Focus 1).
  - Tools become `fantasy.FunctionTool{Name, Description, InputSchema}`.
  - `MaxOutputTokens` is set when > 0.
  - For Anthropic, add ephemeral cache control on the last system part and the last two messages. Confirm the exact option names with `go doc charm.land/fantasy/providers/anthropic ProviderCacheControlOptions` before writing them.
- **`FromFantasy` rules:**
  - `text_delta` → `StreamText`
  - `reasoning_delta` → `StreamReasoning`
  - `tool_call` → `StreamToolCall{Call{ID, Name: ToolCallName, Input: ToolCallInput}}`
  - `finish` → `StreamFinish` with mapped usage (`InputTokens`, `OutputTokens`, `CacheReadTokens` → CacheRead, `CacheCreationTokens` → CacheWrite)
  - `error` → yield `MapError(part.Error)` and stop
  - Every other type is ignored.
- **`MapError`:** `Retryable = pe.IsRetryable()`. `RetryAfter` is parsed from the `retry-after` response header, in seconds (the lookup is case-insensitive). A non-provider error passes through unchanged.
- **`Source.For` errors** (each is `fmt.Errorf` text the user sees):
  - `unknown provider "x" (see: jig models)`
  - `unknown model "anthropic/claude-nope" (see: jig models anthropic)`
  - `no credentials for provider "anthropic": set ANTHROPIC_API_KEY or providers.anthropic.api_key in config.toml`
  - `provider type "t" is not supported`
  - The API key is `cfg.APIKey`, else `getenv(info.APIKeyEnv)`. Providers with an empty `APIKeyEnv` and no config key (e.g. local ollama) are allowed through.
  - jig does its own retries (Task 19), so this layer never uses fantasy's retry helpers.

- [ ] **Step 1: Write the failing tests.**
  - `TestToFantasy_UserAssistantToolRoundTrip`
  - `TestToFantasy_SynthesizesMissingToolResults`
  - `TestToFantasy_ErrorResultUsesErrorContent`
  - `TestToFantasy_CompactionBecomesSummaryUserMessage`
  - `TestFromFantasy_MapsDeltasCallsAndUsage`
  - `TestFromFantasy_ErrorStops`
  - `TestMapError_RetryableAndRetryAfter`
  - `TestAnthropicFactory_StreamsFromFixture`: an httptest server serves `testdata/anthropic/text_and_tool.sse` (hand-written Anthropic SSE: `message_start`, `content_block_start` text, deltas, a `tool_use` block with `input_json_delta`, and `message_delta` with usage). It is hit through `anthropic.WithBaseURL(srv.URL)`. Assert the core event sequence.
  - `TestAnthropicFactory_429IsRetryable`: the fixture returns 429 with `retry-after: 2`, which gives `LLMError{Retryable:true, RetryAfter:2s}`.
  - `TestSource_MissingCredentialsNamesEnvVar`
  - `TestSource_UnknownModel`
  - `TestSource_UnknownProvider`
  - `TestSource_ConfigKeyBeatsEnv`
  - `TestLive_Anthropic`: skipped unless `JIG_LIVE_TESTS=1`. It sends "say hi" to `claude-haiku-4-5-20251001`.
- [ ] **Step 2: Run** `go test ./internal/client/llm/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Keep each file ≤ 500 lines (archtest). `providers.go` holds one small factory struct per type, each calling that fantasy provider's `New(WithAPIKey, WithBaseURL)` and then `LanguageModel(ctx, model)`.
- [ ] **Step 4: Run** `go test ./internal/client/llm/ -race`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(llm): fantasy adapter, error mapping, credentialed model source"`

### Task 12: File discovery — skills, markdown agents, context files

**Files:**
- Create: `internal/data/skillfs/skillfs.go`, `internal/data/agentfs/agentfs.go`, `internal/data/contextfs/contextfs.go`
- Test: one `_test.go` per package, plus `testdata/` trees

**Interfaces:**
- Consumes: `frontmatter.Parse`, `fsroot.Chain`, `paths.ExpandHome`, `core.Skill`, `core.AgentConfig`.
- Produces:
  ```go
  package skillfs
  type Warning struct{ Path, Msg string }
  func Discover(dirs []string) ([]core.Skill, []Warning)   // dirs in ascending precedence; later dirs win on name clash; result sorted by Name
  func Dirs(p paths.Paths, gitRoot, workDir string, extra []string) []string
      // global: ConfigDir/skills, ~/.agents/skills, ~/.claude/skills; then extra (skills.paths);
      // then for each d in Chain(gitRoot, workDir): d/.jig/skills, d/.agents/skills, d/.claude/skills

  package agentfs
  func Discover(dirs []string) (map[string]core.AgentConfig, []skillfs.Warning)   // name = file stem; Source = path
  func Dirs(p paths.Paths, gitRoot, workDir string) (global, project []string)    // ConfigDir/agents | Chain: .jig/agents, .claude/agents

  package contextfs
  type File struct{ Path, Content string }
  func AgentsFiles(p paths.Paths, gitRoot, workDir string) []File   // ConfigDir/AGENTS.md, then per Chain dir: AGENTS.md, else CLAUDE.md
  func Instructions(patterns []string, baseDir, home string) ([]File, error)   // ~ expansion, filepath.Glob, relative to baseDir, de-duplicated, pattern order
  ```
- **Skill rules:**
  - A skill is a directory directly under a skills dir that contains `SKILL.md`.
  - `name` defaults to the directory name. A missing `description` produces a warning and the skill is skipped.
  - A frontmatter `name` that differs from the dir name produces a warning, and the frontmatter name is used.
- **Agent frontmatter fields:** `description`, `mode`, `model`, `max_steps`, `can_spawn`, `hidden`, `tools`, `permissions`. `tools` may be a YAML list or a comma-separated string (Claude Code style). Tool names are lowercased and mapped: `Read→read`, `Write→write`, `Edit→edit`, `MultiEdit→edit`, `Bash→bash`, `Glob→glob`, `Grep→grep`, `Task→task`, `TodoWrite→todo`, `Skill→skill`. Unknown names are kept as-is (lowercased). The body becomes `Prompt`.

- [ ] **Step 1: Write the failing tests.**
  - `TestSkillDiscover_LaterDirWins`
  - `TestSkillDiscover_MissingDescriptionWarns`
  - `TestSkillDiscover_NameMismatchWarns`
  - `TestSkillDirs_Order`
  - `TestAgentDiscover_ClaudeStyleTools`: `tools: Read, Grep, Glob` gives `[read grep glob]`.
  - `TestAgentDiscover_BodyIsPrompt`
  - `TestAgentDiscover_BadYAMLWarnsAndSkips`
  - `TestAgentsFiles_ClaudeFallbackPerLevel`: the root has AGENTS.md and sub has only CLAUDE.md, so both are returned in order.
  - `TestInstructions_GlobAndHome`
  - `TestInstructions_MissingLiteralPathIsError`: a non-glob path that doesn't exist is an error; an unmatched glob is not.
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(data): skill, agent, and context file discovery"`

### Task 13: Agents service — built-ins, merge, model resolution, tool filtering

**Files:**
- Create: `internal/service/agents/builtin.go`, `internal/service/agents/prompts/{build,plan,explore,general,title,compaction}.md`, `internal/service/agents/merge.go`, `internal/service/agents/resolve.go`, `internal/service/agents/tools.go`
- Test: `merge_test.go`, `resolve_test.go`, `tools_test.go`

**Interfaces:**
- Consumes: `core.Agent`, `core.AgentConfig`, `core.Config`, `core.ModelRef`, `ext.Tool`.
- Produces:
  ```go
  package agents
  type Sources struct{ GlobalTOML, GlobalMD, ProjectTOML, ProjectMD map[string]core.AgentConfig }
  type Service struct{ /* name → core.Agent, cfg */ }
  func New(cfg core.Config, src Sources) (*Service, error)     // errors: bad mode, bad model ref, unknown alias
  func (s *Service) Get(name string) (core.Agent, bool)
  func (s *Service) Primary() []core.Agent                     // mode primary|all, not hidden, sorted: build, plan, then by name
  func (s *Service) Subagents() []core.Agent                   // mode subagent|all, not hidden
  func (s *Service) ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
  func (s *Service) SmallModel(fallback core.ModelRef) core.ModelRef
  func ToolsFor(a core.Agent, all []ext.Tool) []ext.Tool
  ```
- **Built-ins** (spec §4.1):
  - `build`: primary, all tools, `CanSpawn` true, MaxSteps 100.
  - `plan`: primary, with permissions write/edit/bash = ask, `CanSpawn` true.
  - `explore`: subagent. Tools `read, glob, grep, skill`. MaxSteps 40.
  - `general`: subagent, all tools, `CanSpawn` false.
  - `title` and `compaction`: hidden, no tools.
  - The prompts are short embedded markdown. `explore.md` must say "read-only; report findings with file paths and line numbers".
- **Merge precedence:** built-in < `GlobalTOML` < `GlobalMD` < `ProjectTOML` < `ProjectMD`, field by field (non-zero or non-nil fields override). `GlobalTOML` and `ProjectTOML` come from `config.Loaded` (Task 8), and the MD maps come from `agentfs` (Task 12).
- **Model strings:** a `model` is either `provider/model` or an alias key in `model_aliases` (e.g. `haiku`). A bare `haiku`/`sonnet`/`opus` with no alias entry is an error: `agent "x": model alias "haiku" is not defined in [model_aliases]`.
- **`ResolveModel`:** the first non-zero of agent.Model, parent, session, then `ParseModelRef(cfg.DefaultModel)`. If all are zero, it returns the error `no model configured: set default_model in config.toml or pass --model`.
- **`SmallModel`:** `cfg.SmallModel` if set, else `fallback`.
- **Default mode:** a user-defined agent with no `mode` is `ModeAll`, so Claude Code agents in `.claude/agents` work as subagents. Built-ins always set their mode explicitly.
- **`ToolsFor`:**
  - Keep the tools named in `a.Tools` (all tools when it is nil).
  - Drop `task` unless `a.CanSpawn`.
  - Drop every tool whose permission rule is `Default: Deny` with no patterns.

- [ ] **Step 1: Write the failing tests.**
  - `TestNew_BuiltinsPresent`
  - `TestMerge_PrecedenceOrder`: the same agent is defined in all 5 sources with different descriptions, and project MD wins. A field set only in global TOML survives.
  - `TestMerge_UnknownAliasErrors`
  - `TestMerge_DefaultModeAll`
  - `TestResolve_Chain`: a table of 5 rows covering each fallback level and the all-zero error.
  - `TestPrimary_Order`
  - `TestToolsFor_AllowlistSpawnAndDeny`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(agents): built-in agents, precedence merge, model resolution"`

### Task 14: Permission service

**Files:**
- Create: `internal/service/permission/rules.go` (effective rules + evaluation), `internal/service/permission/hook.go` (ToolHook), `internal/service/permission/asker.go`
- Test: `rules_test.go`, `hook_test.go`, `asker_test.go`

**Interfaces:**
- Consumes: `ext.ToolHook`, `ext.Subjecter`, `event.Publisher`, `core.PermissionReply`, `core.PermissionRules`, `ids.Gen`.
- Produces:
  ```go
  package permission
  func Defaults() core.PermissionRules            // read/glob/grep/todo/task/skill: allow; write/edit/bash: ask
  func Effective(agent, cfg core.PermissionRules) core.PermissionRules   // precedence per tool: agent > cfg > Defaults(); Default from the highest that sets it; Patterns merged, higher wins per key
  func Evaluate(r core.Rule, subject string) core.Action                 // most-literal-chars matching pattern; tie deny>ask>allow; no match → Default; empty Default → Ask
  func Match(pattern, subject string) bool                               // '*' matches any run (including '/'); everything else is literal

  type Asker interface{ Ask(ctx context.Context, req Request) (core.PermissionReply, error) }
  type Request struct{ SessionID core.SessionID; Tool, Subject string; Call core.ToolCall }
  type BusAsker struct{ /* publisher, ids, pending map[id]chan reply */ }
  func NewBusAsker(pub event.Publisher, ids *ids.Gen) *BusAsker
  func (b *BusAsker) Ask(ctx context.Context, req Request) (core.PermissionReply, error)   // publishes PermissionRequested; blocks on the reply or ctx
  func (b *BusAsker) Reply(requestID string, r core.PermissionReply) error                 // implements core.PermissionService; publishes PermissionResolved; unknown id → error
  func (b *BusAsker) Pending() int                                                         // outstanding requests (entries are removed on reply or ctx done)
  type StaticAsker struct{ Allow bool }                                                    // headless: --yes → ReplyOnce, else ReplyDeny{Message:"permission required (re-run with --yes)"}

  type Hook struct{ /* cfg rules, asker, session grants: map[RootID]map[tool][]subject under mutex */ }
  func NewHook(cfg core.PermissionRules, asker Asker) *Hook   // implements ext.ToolHook
  ```
- **`Hook.Before`:**
  1. The subject is `tool.(ext.Subjecter).Subject(call.Input)` (using the `tool` argument), or `""` when the tool doesn't implement it.
  2. Compute `Evaluate(Effective(rc.Agent.Permissions, cfg)[call.Name], subject)`.
  3. `Allow` passes.
  4. `Deny` blocks with `Verdict{Block:true, Reason:"denied by permission rule for <tool>"}`.
  5. `Ask`: first check the session grants keyed by `rc.RootID` (an exact subject match, or `""` meaning the whole tool). A grant passes. Otherwise call `asker.Ask`:
     - `ReplyAlways` records a grant and passes.
     - `ReplyOnce` passes.
     - `ReplyDeny` blocks with Reason `"user denied: <message>"`, or `"user denied"` when there is no message.
     - A ctx error is returned as the error.
- `After` returns the result unchanged.

- [ ] **Step 1: Write the failing tests.**
  - `TestMatch`: a table covering `"git status*"` vs `"git status --short"` and `"*"` vs anything.
  - `TestEvaluate_MostSpecificWins`
  - `TestEvaluate_TieDenyWins`
  - `TestEvaluate_EmptyDefaultIsAsk`
  - `TestEffective_Precedence`
  - `TestHook_AllowDenyAsk`
  - `TestHook_AlwaysGrantScopedToRoot`: a grant made under root A is not honored under root B, and is honored for a child run with RootID A.
  - `TestHook_DenyMessagePropagates`
  - `TestAsk_ContextCancelUnblocks` (Review Focus 5): cancel while waiting, `Ask` returns `context.Canceled`, and a later `Reply` for that id returns an error rather than blocking.
  - `TestBusAsker_PublishesRequestAndResolved`
  - `TestStaticAsker`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(permission): rules engine, ask flow, and tool hook"`

### Task 15: Shell and search clients

**Files:**
- Create: `internal/client/shell/shell.go`, `internal/client/search/search.go`, `internal/client/search/rg.go`, `internal/client/search/walk.go`
- Test: `shell_test.go`, `search_test.go`

**Interfaces:**
- Produces:
  ```go
  package shell
  type Spec struct{ Command, Dir string; Timeout time.Duration }
  type Result struct{ Output []byte; ExitCode int; TimedOut bool }    // stdout+stderr combined, in order
  type Runner struct{}
  func (Runner) Run(ctx context.Context, s Spec) (Result, error)     // bash -c (sh -c if bash is absent); Setpgid; kill the process group on ctx done or timeout

  package search
  type Match struct{ Path string; Line int; Text string }
  type Searcher interface {
      Glob(ctx context.Context, dir, pattern string, limit int) ([]string, error)     // relative paths, sorted by mtime desc
      Grep(ctx context.Context, dir, pattern, include string, limit int) ([]Match, error)
  }
  func New(lookPath func(string) (string, error)) Searcher   // rg if found, else the Go fallback
  func NewWalker() Searcher                                    // Go fallback: filepath.WalkDir, skips .git, honors the root .gitignore (line-by-line filepath.Match; documented as a subset)
  ```
- Timeouts are driven by `context.WithTimeout`. Tests use a short `Timeout` against `sleep 5` (the shell's sleep command, not Go's `time.Sleep`).

- [ ] **Step 1: Write the failing tests.**
  - `TestRun_CapturesOutputAndExit`: `echo hi; echo err 1>&2; exit 3`.
  - `TestRun_TimeoutKillsProcessGroup`: `sleep 5 & sleep 5` with a 200 ms timeout returns within 2 s with `TimedOut`.
  - `TestRun_ContextCancel`
  - `TestRun_Dir`
  - `TestGlob_Both`: run against `NewWalker()` and, when rg is on PATH, `New(exec.LookPath)` (otherwise `t.Skip`), and assert the same results. Cover `**/*.go`.
  - `TestGrep_Both`: cover an include filter, the limit, and the `.gitignore` honored.
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** rg invocations: `rg --files --glob <pattern>` and `rg --line-number --no-heading --color never [--glob include] -e <pattern>`. **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(client): shell runner and rg/walk searcher"`

### Task 16: File tools — read, write, edit

**Files:**
- Create: `internal/service/tools/tracker.go`, `read.go`, `write.go`, `edit.go`, `schema.go` (small helpers for building JSON Schemas)
- Test: `tracker_test.go`, `read_test.go`, `write_test.go`, `edit_test.go`

**Interfaces:**
- Consumes: `ext.Tool`, `ext.Subjecter`, `ext.RunContext`.
- Produces:
  ```go
  package tools
  type FS interface{ ReadFile(string) ([]byte, error); WriteFile(string, []byte, fs.FileMode) error; Stat(string) (fs.FileInfo, error); ReadDir(string) ([]fs.DirEntry, error); MkdirAll(string, fs.FileMode) error }
  func OSFS() FS
  type Tracker struct{ /* mu; map[SessionID]map[abs path]modtime */ }
  func NewTracker() *Tracker
  func (t *Tracker) MarkRead(sid core.SessionID, path string, mod time.Time)
  func (t *Tracker) CheckWritable(sid core.SessionID, path string, current fs.FileInfo) error  // nil if the file doesn't exist; error if never read or mtime changed
  func NewRead(fs FS, tr *Tracker) ext.Tool     // input {path, offset?, limit?}; Concurrent true
  func NewWrite(fs FS, tr *Tracker) ext.Tool    // input {path, content}; Concurrent false; Subject = abs path
  func NewEdit(fs FS, tr *Tracker) ext.Tool     // input {path, old_string, new_string, replace_all?}; Concurrent false; Subject = abs path
  ```
- **Paths:** relative paths are resolved against `rc.WorkDir`.
- **`read`:**
  - Output lines are `"<n>: <line>"`, 1-based.
  - The default limit is 2000 lines, and lines longer than 2000 chars are truncated with `…`.
  - A file with a NUL byte in its first 8 KB gives the error `"<path> appears to be binary"`.
  - A directory lists its entries one per line, with a trailing `/` on subdirectories.
  - Every successful file read calls `MarkRead`.
- **`write` and `edit`:**
  - `CheckWritable` failures read `"<path> has not been read in this session; read it first"` or `"<path> was modified since it was last read; read it again"`.
  - After a successful write, call `MarkRead` with the new mtime.
- **`edit`:**
  - Fails if `old_string == new_string`, or if `old_string` is not found (the message says "not found").
  - Multiple matches without `replace_all` gives `"old_string matches N times; add context or set replace_all"`.
  - The result output includes the number of replacements.
- Tool errors are returned as `core.ToolResult{IsError:true}` with a nil `error`. A Go `error` return is reserved for ctx cancellation.

- [ ] **Step 1: Write the failing tests** (all in `t.TempDir()`).
  - `TestRead_LineNumbersOffsetLimit`
  - `TestRead_LongLineTruncated`
  - `TestRead_BinaryRefused`
  - `TestRead_Directory`
  - `TestWrite_NewFileNoReadNeeded`
  - `TestWrite_ExistingRequiresRead`
  - `TestEdit_UniqueReplace`
  - `TestEdit_MultipleMatchesError`
  - `TestEdit_ReplaceAll`
  - `TestEdit_NotFound`
  - `TestEdit_RefusesWhenFileChangedSinceRead` (Review Focus 3): read the file, then change its mtime with `os.Chtimes` to +1 s, and the edit is refused.
  - `TestTracker_PerSession`
  - `TestWriteEdit_SubjectIsAbsPath`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Add an `AGENTS.md` note** in the "Invariants" section: "tools return IsError results, not Go errors, except for ctx cancellation".
- [ ] **Step 6: Commit.** `git commit -am "feat(tools): read, write, edit with read-before-write tracking"`

### Task 17: Tools — bash, glob, grep, todo

**Files:**
- Create: `internal/service/tools/bash.go`, `glob.go`, `grep.go`, `todo.go`
- Test: `bash_test.go`, `glob_test.go`, `grep_test.go`, `todo_test.go`

**Interfaces:**
- Consumes: `shell.Runner` and `search.Searcher` through local interfaces (`type Shell interface{ Run(ctx, ShellSpec) (ShellResult, error) }` with its own local struct types; `internal/app` adapts between them), plus `event.Publisher`.
- Produces:
  ```go
  func NewBash(sh Shell, tempDir string) ext.Tool   // input {command, timeout_ms?, description?}; Concurrent false; Subject = command
  func NewGlob(s Searcher) ext.Tool                 // input {pattern, path?}; limit 100; Concurrent true
  func NewGrep(s Searcher) ext.Tool                 // input {pattern, include?, path?}; limit 100; Concurrent true
  type TodoStore interface{ ReplaceTodos(ctx context.Context, id core.SessionID, t []core.Todo) error }
  func NewTodo(store TodoStore, pub event.Publisher) ext.Tool   // input {todos:[{content,status}]}; publishes TodosUpdated; Concurrent false
  ```
- **`bash`:**
  - `timeout_ms` defaults to 120000 and is capped at 600000.
  - Output over 30 KB keeps the last 30 KB, prefixed `"[output truncated; full output: <path>]\n"`, and the full output is written to `tempDir/jig-bash-<callID>.log`.
  - A non-zero exit appends `"\n[exit code N]"` and is *not* `IsError`, since the model reads the code.
  - A timeout appends `"\n[timed out after Ns]"` and is `IsError`.
- **`glob`/`grep`:** output is one line per match, with `"[truncated at 100 results]"` when the limit is hit, and `"no matches"` when empty. `path` must resolve inside `rc.WorkDir` or be absolute.
- **`todo`:** validates statuses, and at most one todo may be `in_progress`.

- [ ] **Step 1: Write the failing tests** (`bash` uses a fake `Shell`).
  - `TestBash_TruncatesAndSavesFullOutput`
  - `TestBash_NonZeroExitNotError`
  - `TestBash_TimeoutCappedAndError`
  - `TestBash_SubjectIsCommand`
  - `TestGlob_NoMatches`
  - `TestGrep_Truncation`
  - `TestTodo_ValidatesAndPublishes`
  - `TestTodo_RejectsTwoInProgress`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(tools): bash, glob, grep, todo"`

### Task 18: Context transforms and skills

**Files:**
- Create: `internal/service/prompt/prompt.go` (the AgentPrompt, Env, Instructions, and AgentsMD transforms), `internal/service/skills/skills.go` (Service + list transform), `internal/service/skills/tool.go`
- Test: `prompt_test.go`, `skills_test.go`, `tool_test.go`

**Interfaces:**
- Consumes: `ext.ContextTransform`, `ext.Tool`, `core.Skill`, `clock.Clock`.
- Produces:
  ```go
  package prompt
  type File struct{ Path, Content string }
  func AgentPrompt() ext.ContextTransform                                          // priority 0: appends rc.Agent.Prompt if non-empty
  func Env(clk clock.Clock, platform string, isGit func(dir string) bool) ext.ContextTransform  // priority 10
  func Instructions(files []File) ext.ContextTransform                             // priority 20
  func AgentsMD(files []File) ext.ContextTransform                                  // priority 30

  package skills
  type FS interface{ ReadFile(string) ([]byte, error); WalkDir(root string, fn fs.WalkDirFunc) error }
  type Service struct{ /* skills by name */ }
  func New(list []core.Skill, fsys FS) *Service
  func (s *Service) Transform() ext.ContextTransform   // priority 40
  func (s *Service) Tool() ext.Tool                    // "skill"; input {id}; Concurrent true
  ```
- **Exact text** (the tests pin it):
  - Env appends `"<env>\n  Working directory: <rc.WorkDir>\n  Platform: <platform>\n  Is git repo: <yes|no>\n  Today's date: <Mon Jan 2 2006>\n</env>"`.
  - Instructions and AgentsMD append one system string per file: `"Instructions from: <path>\n<content>"`.
  - The skills transform appends nothing when there are no skills **or** `req.Tools` has no `"skill"` tool. Otherwise it appends `"Skills provide specialized instructions and workflows for specific tasks.\nUse the skill tool to load a skill when a task matches its description.\n<available_skills>\n"` + for each skill `"  <skill>\n    <id>NAME</id>\n    <description>DESC</description>\n  </skill>\n"` + `"</available_skills>"`.
  - Skill tool output: `"<skill_content name=\"NAME\">\n# Skill: NAME\n\n<body without frontmatter>\n\nBase directory for this skill: <dir>\nRelative paths in this skill are relative to this base directory.\n\n<skill_files>\n<file><abs path></file>…\n</skill_files>\n</skill_content>"`.
    - The file list excludes `SKILL.md` and is sorted, capped at 50.
    - An unknown id gives `IsError` with `"unknown skill \"x\"; available: a, b"`.

- [ ] **Step 1: Write the failing tests.**
  - `TestTransforms_OrderAndText`: register all five transforms in shuffled order, apply them via a sorted `ext.View`, and compare `req.System` exactly.
  - `TestEnv_NotGit`
  - `TestSkillsTransform_OmittedWithoutSkillTool`
  - `TestSkillsTransform_Lists`
  - `TestSkillTool_LoadsBodyAndFiles`
  - `TestSkillTool_FileListCappedAt50`
  - `TestSkillTool_Unknown`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(prompt,skills): context transforms and skill tool"`

### Task 19: Runner — the streaming loop

**Files:**
- Create: `internal/service/agent/runner.go` (Runner, Run, Cancel, step loop), `internal/service/agent/request.go` (build LLMRequest), `internal/service/agent/stream.go` (consume one stream into a message), `internal/service/agent/retry.go`, `internal/service/agent/proxy.go`
- Test: `runner_test.go`, `stream_test.go`, `retry_test.go` (all using `llmtest` and a real store in `t.TempDir()` behind the local `MessageStore` interface)

**Interfaces:**
- Consumes: `core.LLM`, `ext.View` (Tools, Transforms, ToolHooks), `event.Publisher`, `clock.Clock`, `ids.Gen`, `agents.ToolsFor` (as a func).
- Produces:
  ```go
  package agent
  type LLMSource interface{ For(core.ModelRef) (core.LLM, core.ModelInfo, error) }
  type MessageStore interface{ SaveMessage(ctx context.Context, m core.Message) error }
  type History interface{ History(ctx context.Context, id core.SessionID) ([]core.Message, error) }
  type Deps struct {
      LLMs     LLMSource
      Ext      ext.View
      Store    MessageStore
      History  History
      Bus      event.Publisher
      Clock    clock.Clock
      IDs      *ids.Gen
      ToolsFor func(core.Agent, []ext.Tool) []ext.Tool
  }
  type Runner struct{ /* deps; mu; running map[SessionID]context.CancelFunc */ }
  func NewRunner(d Deps) *Runner
  func (r *Runner) Run(ctx context.Context, rc ext.RunContext, userText string) (core.Message, error)
  func (r *Runner) Cancel(id core.SessionID)
  var ErrBusy = errors.New("agent: session already has a running turn")

  // Late binding for the task tool (Task 21), which needs the Runner before the registry is frozen.
  // This is the only sanctioned setter-style wiring; Set panics if called twice.
  type Proxy struct{ /* atomic.Pointer[Runner] */ }
  func (p *Proxy) Set(r *Runner)
  func (p *Proxy) Run(ctx context.Context, rc ext.RunContext, userText string) (core.Message, error)
  ```
- **`Run` algorithm** (`rc.Model` is already resolved by the caller):
  1. If the session is already running, return `ErrBusy`. Register a cancel func for `rc.SessionID` and remove it on return.
  2. Save the user message (`StatusComplete`).
  3. Loop over steps `1..maxSteps`, where maxSteps is `rc.Agent.MaxSteps`, or 100 when it is 0:
     1. Build the request: `Messages = History(sid)`; `Tools = ToolSpecs(ToolsFor(agent, Ext.Tools()))`; apply `Ext.Transforms()` in order; `MaxOutputTokens = info.DefaultMaxTokens`.
     2. Create an assistant message (`StatusStreaming`, `Agent`, `Model`), publish `MessageStarted`, and stream it (see below).
     3. Compute the cost with `info.Cost(usage)`.
     4. If there are no tool calls, mark the message complete, save it, and stop.
     5. Otherwise execute the tools (Task 20), then save.
  4. When max steps is exhausted with tool calls still pending, append the text part `"[stopped: reached max_steps (N)]"` and stop.
  5. Publish `RunFinished` with the summed usage and cost, and return the last assistant message.
- **Streaming:**
  - Text and reasoning deltas are appended to the current part of the same kind (a new part starts when the kind changes) and published as `TextDelta`/`ReasoningDelta`.
  - Tool calls are appended as `PartToolCall` parts.
  - `StreamFinish` records usage.
  - The message is saved at the end of every step, and also when the first tool call arrives.
- **Retries:**
  - Only when the error is a `*core.LLMError` with `Retryable` **and** no event has been received yet in that step. Up to 3 attempts in total.
  - The delay is `RetryAfter` if > 0, else 1 s, then 2 s, waited with `Clock.After` (and ctx).
  - Any other error: mark the message `StatusFailed`, save it, publish `RunFailed{Err}`, and return the error.
- **Cancellation:**
  - Mark the message `StatusInterrupted`.
  - Every tool call without a result gets `ToolResult{IsError:true, Output:"cancelled"}`.
  - Save, publish `RunFailed{Err:"cancelled"}`, and return `context.Canceled`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRunner_TextOnlyTurn`: the store has a user message and an assistant message. The published events are MessageStarted, TextDelta, and RunFinished in that order.
  - `TestRunner_RequestHasTransformsToolsAndHistory`: inspect `llmtest.Requests()[0]`.
  - `TestRunner_CostFromModelInfo`
  - `TestRunner_MaxStepsStops`: `MaxSteps: 2`, with a script that always calls a tool. There are 2 LLM requests, and the final text part says `reached max_steps (2)`.
  - `TestRunner_RetriesBeforeFirstEvent`: turn 1 is `Err: &core.LLMError{Retryable:true}` and turn 2 is text. Use `fake.BlockUntilWaiters(1)` and then `Advance(time.Second)`. It succeeds.
  - `TestRunner_NoRetryAfterPartialOutput`: the turn yields text then a retryable error, and the run fails with `StatusFailed`.
  - `TestRunner_NonRetryableFails`
  - `TestRunner_BusyRejected`
  - `TestRunner_CancelMarksInterrupted`: a `Hang` turn, then `Cancel(sid)`, gives `context.Canceled` and a stored status of interrupted.
  - `TestProxy_SetTwicePanics`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** Keep `Run` itself under 80 lines by delegating to `step()`, `buildRequest()`, `consume()`, and `finish()`. **Step 4: Run with `-race -count=3`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(agent): streaming runner loop with retries and cancellation"`

### Task 20: Runner — tool execution

**Files:**
- Create: `internal/service/agent/exec.go`
- Test: `exec_test.go`

**Interfaces:**
- Consumes: `ext.Tool`, `ext.ToolHook`, `ext.RunContext`.
- Produces: `func (r *Runner) execute(ctx context.Context, rc ext.RunContext, allowed []ext.Tool, calls []core.ToolCall) []core.ToolResult`, which returns results in call order.
- **Batching:** consecutive calls whose tool is `Concurrent()` form a batch that runs in parallel (`sync.WaitGroup`). Every other call runs alone, in order.
- **Each call:**
  1. Publish `ToolCallStarted`.
  2. Resolve the tool from `allowed`. A miss gives the error result `"tool \"x\" is not available to agent \"a\"; available: …"` (Review Focus 2).
  3. The input must be a JSON object, else `"invalid JSON input for x: <err>"` (Review Focus 2).
  4. Run `Before` hooks in order: a `Block` gives an error result with `Reason`, and a hook error (ctx) gives the result `"cancelled"`.
  5. `Run`, with `recover()`: a panic gives `"tool x panicked: <v>"`.
  6. Run `After` hooks in order.
  7. Set `Result.CallID` and `Name`, and publish `ToolCallFinished`.
  - Results are attached to the assistant message as `PartToolResult` parts, placed after all tool-call parts.

- [ ] **Step 1: Write the failing tests.** Use stub tools and hooks declared in the test file, plus `event.Bus`.
  - `TestExec_ConcurrentBatchRunsInParallel`: two concurrent tools each wait on a shared barrier channel, and the test completes (it would deadlock if run serially).
  - `TestExec_NonConcurrentSerialAndOrdered`
  - `TestExec_ResultsInCallOrder`
  - `TestRunner_UnknownToolBecomesErrorResult`
  - `TestRunner_MalformedInputBecomesErrorResult`
  - `TestExec_HookBlocks`
  - `TestExec_PanicRecovered`
  - `TestRunner_CancelDuringPermissionPrompt` (Review Focus 5): a real `permission.Hook` with a `BusAsker` and two parallel calls to an `ask` tool. Wait for two `PermissionRequested` events on a bus subscription, then `Cancel`. `Run` returns `context.Canceled`, both results are `"cancelled"`, and `asker.Pending() == 0`.
  - `TestRunner_CancelledRunPairsEveryToolCall` (Review Focus 1): after the cancellation, every `PartToolCall` in the stored messages has a matching `PartToolResult`. The provider-level replay is covered by `TestE2E_ResumeAfterCancelledRun` in Task 24.
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race -count=3`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(agent): hooked, batched tool execution"`

### Task 21: `task` tool — subagents

**Files:**
- Create: `internal/service/task/task.go`
- Test: `task_test.go`

**Interfaces:**
- Consumes: `agent.Proxy` (as the `Runner` interface), `agents.Service` (as an interface), `event.Publisher`.
- Produces:
  ```go
  package task
  type Sessions interface{
      CreateChild(ctx context.Context, parent core.SessionID, agent string, model core.ModelRef, title string) (core.Session, error)
      Get(ctx context.Context, id core.SessionID) (core.Session, error)
  }
  type Agents interface{
      Get(name string) (core.Agent, bool)
      Subagents() []core.Agent
      ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
  }
  type Runner interface{ Run(ctx context.Context, rc ext.RunContext, text string) (core.Message, error) }
  const MaxDepth = 3
  func New(s Sessions, a Agents, r Runner, pub event.Publisher) ext.Tool   // "task"; input {agent, description, prompt, session_id?}; Concurrent true
  ```
- **`Description()`** is built on each call: `"Launch a subagent to handle a task autonomously.\n\nAvailable agents:\n- <name>: <description>\n…"`.
- **`Run`:**
  1. If `rc.Depth >= MaxDepth`, return the error `"subagent depth limit (3) reached"`.
  2. An unknown or non-subagent `agent` gives an error listing the available agents.
  3. With `session_id`: `Get` it, and error unless `ParentID == rc.SessionID`.
  4. Otherwise `CreateChild(rc.SessionID, agent, model, description)`, where the model is `ResolveModel(sub, rc.Model, zero)`.
  5. Publish `SubagentSpawned`.
  6. Call `Run` with `ext.RunContext{SessionID: child, RootID: rc.RootID, Agent: sub, Model: model, WorkDir: rc.WorkDir, Depth: rc.Depth+1}`, using the **same ctx** so that cancelling the parent cancels the child.
- **Output:** `"<task_result session_id=\"<id>\">\n<final assistant text parts joined>\n</task_result>"`. A child error gives `IsError` with the same wrapper around `"error: <err>"`.

- [ ] **Step 1: Write the failing tests** (with fakes).
  - `TestTask_SpawnsChildWithResolvedModel`: the subagent has a fixed haiku model while the parent has opus, and the child rc uses haiku.
  - `TestTask_InheritsParentModelWhenUnset`
  - `TestTask_DepthLimit`
  - `TestTask_UnknownAgentListsAvailable`
  - `TestTask_ContinueRequiresOwnChild`
  - `TestTask_ParentCancelCancelsChild`
  - `TestTask_DescriptionListsSubagents`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(task): subagent tool with per-agent models"`

### Task 22: Session and chat services

**Files:**
- Create: `internal/service/agent/complete.go`, `internal/service/session/session.go`, `internal/service/session/history.go`, `internal/service/session/compact.go`, `internal/service/session/title.go`, `internal/service/chat/chat.go`
- Test: `complete_test.go`, `session_test.go`, `history_test.go`, `compact_test.go`, `title_test.go`, `chat_test.go`

**Interfaces:**
- Consumes: the store (through a local interface with the Task 9 method set), `agent.LLMSource`, `agents.Service` (through an interface), `event.Publisher`, `clock.Clock`, `ids.Gen`, `agent.Runner` (Run/Cancel).
- Produces:
  ```go
  package agent
  func Complete(ctx context.Context, m core.LLM, system, user string) (string, error)   // one-shot, no tools; joins text events

  package session
  type Service struct{ /* store, llms, agents, pub, clk, ids */ }
  func New(d Deps) *Service                                   // Deps struct mirrors the Consumes list
  func (s *Service) Create(ctx context.Context, agent, cwd string) (core.Session, error)          // publishes SessionCreated
  func (s *Service) CreateChild(ctx context.Context, parent core.SessionID, agent string, model core.ModelRef, title string) (core.Session, error)
  func (s *Service) Get(ctx context.Context, id core.SessionID) (core.Session, error)
  func (s *Service) Update(ctx context.Context, sess core.Session) error
  func (s *Service) List(ctx context.Context, limit int) ([]core.Session, error)
  func (s *Service) Messages(ctx context.Context, id core.SessionID) ([]core.Message, error)
  func (s *Service) History(ctx context.Context, id core.SessionID) ([]core.Message, error)
  func (s *Service) Compact(ctx context.Context, id core.SessionID) error
  func (s *Service) GenerateTitle(ctx context.Context, id core.SessionID, firstPrompt string) error
  // *Service satisfies core.SessionService, task.Sessions, and agent.History.

  package chat
  type ConfigError struct{ Err error }                         // Error(), Unwrap(); headless maps it to exit 2
  type Service struct{ /* deps, base ctx, wg */ }
  func New(d Deps) *Service                                    // implements core.ChatService
  ```
- **`History`:** find the last message that contains a `PartCompaction`, and return that message plus everything after it. With none, return all messages.
- **`Compact`:**
  - Render the messages since the last compaction as plain text: `"user: …"` and `"assistant: …"`, with tool calls shown as `"[tool <name>]"`.
  - Call `Complete` using the `compaction` agent's prompt and the model `agents.SmallModel(resolved session model)`.
  - Save an assistant message whose single `PartCompaction` holds the summary, with `Agent "compaction"`.
  - When there is nothing new since the last compaction, return the error `"nothing to compact"`.
- **`GenerateTitle`:**
  - Uses the `title` agent's prompt and `SmallModel`, with the user prompt truncated to 2000 chars.
  - The title is the first non-empty line of the result with surrounding quotes trimmed, capped at 50 runes with `…`.
  - It is saved with `Update`. On error the placeholder title is kept.
- **`chat.Send`:**
  1. The agent defaults to `"build"`. A non-primary agent gives `ConfigError("agent \"explore\" is a subagent; primary agents: build, plan")`.
  2. Create the session when `req.SessionID` is empty; otherwise `Get` it, and a missing session gives a `ConfigError`.
  3. If `req.Model != ""`, parse it (a failure gives a `ConfigError`), store it on the session, and `Update`.
  4. `model := ResolveModel(agent, zero, sessionModel)`. Preflight with `LLMs.For(model)`: any error is wrapped in a `ConfigError`.
  5. If the session title is empty, set the placeholder (the first line of the text, capped at 50 runes) and start `GenerateTitle` in a goroutine tracked by `wg`, using the service's base ctx.
  6. `Runner.Run(ctx, RunContext{SessionID, RootID: SessionID, Agent, Model, WorkDir: d.WorkDir})`.
- **`Close(ctx)`:** wait for `wg` or `ctx.Done()`, then cancel the base ctx.

- [ ] **Step 1: Write the failing tests.** Use a real store in a temp dir, plus `llmtest`.
  - `TestComplete_JoinsText`
  - `TestCreate_PublishesSessionCreated`
  - `TestHistory_StartsAtLastCompaction`
  - `TestCompact_StoresSummaryUsingSmallModel`: `llmtest` records the request's model.
  - `TestCompact_NothingToCompact`
  - `TestTitle_TrimsAndCaps`
  - `TestTitle_ErrorKeepsPlaceholder`
  - `TestSend_DefaultsToBuildAndCreatesSession`
  - `TestSend_SubagentRejectedAsConfigError`
  - `TestSend_ModelFlagStoredOnSession`
  - `TestSend_PreflightCredentialErrorIsConfigError`
  - `TestSend_TitleGeneratedInBackground`: after `Close`, the stored title equals the scripted one.
  - `TestSend_ResumeExistingSession`
- [ ] **Step 2: Run.** Expected: FAIL. **Step 3: Implement.** **Step 4: Run with `-race`.** Expected: PASS.
- [ ] **Step 5: Update `AGENTS.md`.** List the ports in "Architecture": `core.ChatService`, `core.SessionService`, and `core.PermissionService` are what the UIs call.
- [ ] **Step 6: Commit.** `git commit -am "feat(session,chat): sessions, history, compaction, titles, chat facade"`

### Task 23: Headless renderer, composition root, and CLI

**Files:**
- Create: `internal/ui/plain/plain.go`
- Create: `internal/app/app.go` (`Run`, `Stdio`, exit codes), `cli.go` (subcommand and flag parsing), `env.go` (paths, workDir, git root, config), `data.go` (store, catalog + background refresh, discovery), `registry.go` (registers tools, hooks, transforms, providers; Freeze), `services.go` (agents, permission, session, runner + proxy, chat), `headless.go`, `models.go`, `sessions.go`, `providers_default.go` (`//go:build !jigtest`)
- Modify: `cmd/jig/main.go`
- Test: `internal/ui/plain/plain_test.go`, `internal/app/cli_test.go`, `internal/app/app_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  ```go
  package plain
  type Renderer struct{ /* out, errw, children set */ }
  func New(out, errw io.Writer) *Renderer
  func (r *Renderer) Run(events <-chan event.Event)          // returns when the channel closes

  package app
  type Stdio struct{ In io.Reader; Out, Err io.Writer }
  func Run(ctx context.Context, args []string, std Stdio, getenv func(string) string) int
  func extraProviders() []ext.ProviderFactory                // nil by default; the jigtest build tag adds jigtest (Task 24)
  ```
- **`plain` output:**
  - A session counts as a *child* once a `SubagentSpawned` names it. Every other session is the root.
  - Root `TextDelta` text goes to `out` verbatim, and a trailing newline is ensured on the root's `RunFinished`.
  - `ToolCallStarted` goes to `errw` as `"→ <tool> <input JSON, first 80 runes>"`, indented two spaces per child level.
  - An error `ToolCallFinished` goes to `errw` as `"  ✗ <first line of output>"`.
  - `SubagentSpawned` goes to `errw` as `"↳ <agent>: <description>"`.
  - `RunFailed` on the root goes to `errw` as `"error: <err>"`.
  - `ReasoningDelta` is not printed.
- **CLI:**
  - `jig` with no arguments prints `"the interactive UI is not built yet; use: jig run \"prompt\""` to stderr and exits 2.
  - `jig run [--agent A] [--model M] [--yes] [--session ID] [--cwd DIR] <prompt...>`. An empty prompt is a usage error (exit 2).
  - `jig models [provider]` lists `provider/model  ctx <N>k  $<in>/$<out> per 1M`, with each provider header marked `(configured)` or `(no credentials)`.
  - `jig sessions` prints `<id>  <updated RFC3339>  <title>`.
  - `jig version`.
- **Exit codes:**
  - Config load errors, `agents.New` errors, and `chat.ConfigError` give 2.
  - Other run errors, including cancellation, give 1.
- **`main.go`:** `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`, then `os.Exit(app.Run(ctx, os.Args[1:], app.Stdio{os.Stdin, os.Stdout, os.Stderr}, os.Getenv))`. It must stay ≤ 60 lines.
- **Wiring rules:**
  - One `event.Bus`.
  - The permission asker is `StaticAsker{Allow: yes}` in headless mode (`BusAsker` is used by Plan 2).
  - The catalog refresh runs in a goroutine bound to ctx and is never awaited.
  - Each `internal/app` function stays ≤ 40 lines (archtest), so split construction into one function per subsystem, each returning a small struct.

- [ ] **Step 1: Write the failing tests.**
  - `TestPlain_RootTextToStdoutToolsToStderr`
  - `TestPlain_ChildIndentedAndTextSuppressed`
  - `TestPlain_TrailingNewline`
  - `TestCLI_ParseRunFlags`
  - `TestCLI_EmptyPromptUsage`
  - `TestRun_NoArgsExits2`
  - `TestRun_UnknownSubcommandExits2`
  - `TestModels_ShowsCredentialStatus`: temp XDG dirs via the `getenv` map, and a config with a custom `openai-compat` provider.
  - `TestSessions_EmptyDBPrintsNothing`
- [ ] **Step 2: Run** `go test ./internal/ui/plain/ ./internal/app/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./... -race`, including archtest. Expected: PASS.
- [ ] **Step 5: Smoke test manually** (only if `ANTHROPIC_API_KEY` is set). Run `go build -o bin/jig ./cmd/jig && bin/jig run --model anthropic/claude-haiku-4-5-20251001 "list the files here using glob"`. It should stream text and print a `→ glob` line.
- [ ] **Step 6: Commit.** `git commit -am "feat(app): composition root, headless renderer, and CLI"`

### Task 24: Scripted provider and end-to-end tests

**Files:**
- Create: `internal/client/llm/jigtest/jigtest.go` (`//go:build jigtest`), `internal/app/providers_jigtest.go` (`//go:build jigtest`)
- Create: `e2e/harness_test.go` (build once in `TestMain`, temp env, `runJig`), `e2e/e2e_test.go`, `e2e/testdata/*.json`
- Modify: `Makefile` (the `test` target also runs `go test -tags jigtest ./e2e/...`)

**Interfaces:**
- Consumes: `ext.ProviderFactory`, `llm.ToFantasy`.
- Produces:
  ```go
  //go:build jigtest
  package jigtest
  type Script struct{ Models map[string][]Turn }    // model ID → queue; each model's queue is consumed in order
  type Turn struct {
      Text                 string
      Calls                []struct{ ID, Name string; Input json.RawMessage }
      Hang                 bool          // emit Text, then block until ctx is done
      Err                  string
      Retryable            bool
      ExpectSystemContains []string      // fail the turn with a non-retryable error if any is missing
  }
  func Factory() ext.ProviderFactory      // Type "jigtest"; the script path comes from cfg.Options["script"]; one shared queue state per script path per process
  ```
- **Every jigtest `Stream`:**
  1. Run `llm.ToFantasy(req)`.
  2. Verify that every `ToolCallPart` has a matching `ToolResultPart` in a later tool message. A mismatch gives the error `"jigtest: unpaired tool call <id>"`.
  3. Check `ExpectSystemContains`.
  4. Then replay the turn.
- **The e2e harness:**
  - It builds with `go build -tags jigtest -o <tmp>/jig ./cmd/jig` once.
  - Each test gets fresh `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, and `XDG_CACHE_HOME` under `t.TempDir()`, plus a `work/` dir with a `.git` directory, and `ANTHROPIC_API_KEY=""`.
  - `runJig(t, env, args...) (stdout, stderr string, code int)` has a 30 s context timeout.
  - `writeConfig(t, env, toml string)` and `writeScript(t, env, name string, s jigtest.Script)` are helpers.

- [ ] **Step 1: Write the failing tests.**
  - `TestE2E_TextReply`: stdout contains the scripted text, the exit is 0, and `jig sessions` lists one session.
  - `TestE2E_WriteDeniedWithoutYes` / `TestE2E_WriteAllowedWithYes`: the file exists only with `--yes`, and stderr shows `✗` when it is denied.
  - `TestE2E_SubagentUsesItsModel`: `agents.explore.model = "jigtest/m2"`. The `m1` queue calls `task` with `explore`, the `m2` queue answers, and the `m1` queue then finishes. The exit is 0, which proves the `m2` queue was consumed.
  - `TestE2E_ClaudeAgentWithAlias`: `.claude/agents/reviewer.md` with `model: haiku` and `tools: Read`, plus `[model_aliases] haiku = "jigtest/m2"`, gives the same routing assertion.
  - `TestE2E_SkillListedAndLoaded`: `.agents/skills/demo/SKILL.md`. The turn expects `<id>demo</id>` in the system prompt and calls `skill {"id":"demo"}`, and the next turn expects nothing.
  - `TestE2E_AgentsMDInPrompt`
  - `TestE2E_MissingCredentialsExits2` (Review Focus 4): stderr contains `ANTHROPIC_API_KEY`.
  - `TestE2E_UnknownModelExits2`
  - `TestE2E_ResumeAfterCancelledRun` (Review Focus 1): script 1 calls `bash {"command":"sleep 30"}` with `--yes`. Read stderr until `→ bash`, send SIGINT, and the exit is 1. Then `jig run --session <id>` with a second script whose turn is text. The exit is 0, which means jigtest found every call paired.
- [ ] **Step 2: Run** `go test -tags jigtest ./e2e/ -race`. Expected: FAIL.
- [ ] **Step 3: Implement** jigtest and the build-tagged `extraProviders`.
- [ ] **Step 4: Run** `make test`. Expected: PASS, including the e2e tests.
- [ ] **Step 5: Commit.** `git commit -am "test(e2e): scripted provider and end-to-end headless tests"`

### Task 25: Documentation and final verification

**Files:**
- Create: `README.md`, `docs/config.example.toml`
- Modify: `AGENTS.md`, `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` (status line)

- [ ] **Step 1: Write the `README.md` sections.**
  - What jig is.
  - Install: `go install github.com/gammons/jig/cmd/jig@latest`.
  - Quick start with `ANTHROPIC_API_KEY`.
  - `jig run` flags.
  - Config file locations.
  - Per-agent models (the explore/haiku example).
  - Markdown agents.
  - Skills, including using superpowers via `instructions`.
  - Permissions.
  - Status: the TUI is coming in Plan 2.
- [ ] **Step 2: Write `docs/config.example.toml`.** It exercises every key from Task 8, and `TestLoad_ExampleConfigParses` (add it to `internal/data/config`) loads it without error.
- [ ] **Step 3: Complete `AGENTS.md`.**
  - The shared-code table covers every helper introduced.
  - Invariants: the registry is frozen, `View` is read-only, tools return `IsError` results, `Proxy` is the only late binding, no I/O in `ui`, and one owner per piece of state.
  - A "how to add a tool / transform / hook" recipe, three short numbered lists pointing at `internal/app/registry.go`.
- [ ] **Step 4: Verify.** Run `make check`. Expected: all green, with `gofmt -l .` empty and the archtest allowlist empty.
- [ ] **Step 5: Update the spec status line** to `implemented (Plan 1: headless); Plan 2 (TUI) pending`.
- [ ] **Step 6: Commit.** `git commit -am "docs: README, example config, AGENTS.md; mark Plan 1 complete"`
