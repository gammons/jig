# jig: Debug Log (`JIG_DEBUG`)

- **Status:** draft, awaiting review
- **Date:** 2026-09-30
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` (implemented).
- **Prior art:** slk's `internal/debuglog` and `SLK_DEBUG` (same author).

## 1. Intent

**What the author asked for**

- When jig drives subagents, they seem to run very slowly and fail often ("API error", "upstream proxy error", "ran out of steps"). Today nothing records why: jig has no logging, and a failure reaches the UI only as one `RunFailed` string.
- A `JIG_DEBUG` environment variable, like slk's `SLK_DEBUG`, that writes a debug log we can read after a bad run.

**Decisions made in review**

1. **Scope A first.** This spec covers subagents and model calls: HTTP, retries, runner steps, the task tool, and tool timings. A later spec (scope B) adds config and trust decisions, sessions and compaction, and TUI timing. The design must let B add categories and call sites without changing the mechanism.
2. **The log is `jig-debug.log` in the working directory**, truncated on each start, as in slk. Two jig processes in the same directory overwrite each other's log; that is accepted. Users should gitignore the file.
3. **Metadata only, plus error bodies.** No prompts, tool inputs, tool outputs, or streamed text. The one exception is the body of a non-2xx HTTP response, capped at 4 KB.
4. **Standard-library `log/slog`, passed in as a dependency.** Not a slk-style package with globals (the architecture tests forbid package-level mutable vars), and not events on the bus (the bus drops undelivered events when a subscription closes).

**Background found during design**

- The client libraries do not retry. fantasy sets `option.WithMaxRetries(0)` on the Anthropic and OpenAI clients, and fantasy's own retry code runs only inside its `Agent`, which jig does not use. The only retries are jig's: `internal/service/agent/retry.go`, 3 attempts, only when the error arrives before any stream event.
- A stream that fails after output has started is never retried, and nothing currently records it.
- The default step limit is 100 (`defaultMaxSteps`), after which `stopAtMaxSteps` appends a notice.

**Success criteria**

- With `JIG_DEBUG=1`, after a session like the one that prompted this, `jig-debug.log` shows for each failed subagent: its session ID and depth, each HTTP attempt with status, time to first byte, total duration, and provider request ID, the error body of any non-2xx response, every retry decision, and the run's outcome (`failed`, `max_steps`, and so on) with step count and duration.
- `grep session=<id> jig-debug.log` gives one subagent's full timeline, including its HTTP lines.
- With `JIG_DEBUG` unset, no file is created, the HTTP client is the one each provider uses today, and the performance budgets in AGENTS.md still pass.
- The log never contains an API key, an auth header, a prompt, or tool output.

## 2. Scope

**In scope**

- The `JIG_DEBUG` switch and `jig-debug.log`, for both the TUI and `jig run`.
- The categories `app`, `run`, `step`, `retry`, `http`, `task`, `tool` (§3).
- Tagging every line with the run's `root`, `session`, `depth`, and `agent`, including HTTP lines.

**Out of scope (scope B, later)**

- Config loading, trust decisions, session and compaction, title generation, catalog refresh, and TUI render timing.
- Request and response bodies beyond capped error bodies.
- Log levels other than debug, log rotation, and choosing the file path.

## 3. What gets logged

Every line is a `slog` text record at `DEBUG` level with a `cat=` attribute. Where a run is known, it also carries `root=`, `session=`, `depth=`, and `agent=`. Example:

```
time=2026-09-30T14:02:11.482913Z level=DEBUG msg="http response" cat=http root=ses_a session=ses_b depth=1 agent=general method=POST host=api.anthropic.com path=/v1/messages status=502 ttfb=31.2s req_id=req_123 body="upstream connect error or disconnect/reset before headers"
```

### 3.1 `cat=app` (`internal/app`, at startup)

- `debug log start`: jig version, working directory, PID, resolved default model.

### 3.2 `cat=run` (`agent.Runner.Run`)

- `run start`: `model=provider/model`, `max_steps`.
- `run end`: `outcome=done|max_steps|failed|cancelled`, `steps`, `in`/`out` tokens, `cost`, `dur`, and `err` when failed.

### 3.3 `cat=step` (`Runner.step`)

- `step start`: `step`, `messages`, `tools` (counts of the request's messages and tools).
- `step end`: `step`, `first_event` (time from request to first stream event), `stream` (stream duration), `tools_dur` (time in tool execution), `in`/`out`/`cache_read`/`cache_write` tokens, `calls` (tool calls), `status`.

### 3.4 `cat=retry` (`Runner.stream`)

- `attempt failed`, logged for every failed attempt: `attempt`, `received` (whether any stream event had arrived), `retryable`, `retry_after`, then either `delay` or `giving_up=true`, and `err`.

### 3.5 `cat=http` (`internal/client/llm/httplog.go`)

A logging `http.RoundTripper` wraps the default transport:

- `http request`: `method`, `host`, `path`.
- `http response`: `status`, `ttfb`, and `req_id` (the first of `request-id`, `x-request-id`, `cf-ray` that is present).
- `http body done`: `bytes`, `dur` (request start to body EOF or Close), and `read_err` when the read failed. A dropped stream shows as `read_err="unexpected EOF"`.
- `http error`: the transport error (DNS, TLS, timeout, cancel) and `dur`, when no response came back.
- For a non-2xx status, the `http response` line also carries `body`: the first 4 KB of the response body, read through a tee so the SDK still receives the complete, unchanged body. The transport logs it raw; the handler's `ReplaceAttr` (§4.1) cleans it with `ansi.SanitizeLine`, so `client/llm` needs no `ansi` import.

No other header or body content is logged.

### 3.6 `cat=task` (`internal/service/task`)

- `task spawn`: `parent`, `child`, `resumed`, `subagent`, `model`, `depth`.
- `task end`: `child`, `dur`, `outcome=ok|error|cancelled`, and `err` holding the raw child error before `wrapResult` turns it into tool output.
- `task rejected`: `reason` (depth limit, unknown agent, model resolution failure, invalid session). The reason is the same message the tool returns.

### 3.7 `cat=tool` (`internal/service/agent/exec.go`)

- `tool call`: `tool`, `call_id`, `dur`, `out_bytes` (before the 50 KB cap), `out_bytes_capped` (after), `is_error`, and `blocked=true` when a hook blocked the call. Unknown and invalid calls are logged with `blocked=false is_error=true` and a `reason`.

## 4. Design

### 4.1 Setup (`internal/app/debuglog.go`)

`openDebugLog(getenv func(string) string, workDir string) (*slog.Logger, io.Closer, error)`:

- If `getenv("JIG_DEBUG")` is empty, it returns a logger over a handler whose `Enabled` is always false, and a no-op closer. A log call on it returns after one method call and formats nothing.
- Otherwise it opens `filepath.Join(workDir, "jig-debug.log")` with `O_CREATE|O_TRUNC|O_WRONLY`, mode 0644, and returns `slog.New(slog.NewTextHandler(f, opts))` with `Level: slog.LevelDebug`. Its `ReplaceAttr` formats `time` as RFC 3339 with microseconds in UTC, formats `time.Duration` values with `String()`, and passes string values through `ansi.SanitizeLine`.
- If the file cannot be opened, the caller prints a warning to stderr through `printLine`, before the TUI takes the terminal, and continues with the disabled logger. Logging never makes jig fail.

`loadEnv` calls it once, after `workDir` is resolved. The environment stores the logger and closer, and the runtime closes the closer when it closes. The TUI and `jig run` both go through this path. The `cat=app` line is written right after the logger opens.

### 4.2 Passing the logger in

No package-level state. Each consumer takes a `*slog.Logger`, and a nil logger is replaced with the disabled one at construction, so tests that do not care pass nothing.

- `agent.Deps` gains `Log *slog.Logger`. It covers `run`, `step`, `retry`, and `tool`, since the executor is in the `agent` package.
- `task.New(sessions, agents, runner, bus, log)` gains a final `log` parameter.
- `llm.Factories()` becomes `llm.Factories(opts ...Option)` with `llm.WithHTTPClient(*http.Client)`. Each factory stores the client and, when it is non-nil, passes it to the provider's `WithHTTPClient` option (anthropic, openai, openaicompat, openrouter, google). With no option, every factory builds its provider exactly as it does today.
- `internal/app/registry.go` passes `llm.WithHTTPClient(&http.Client{Transport: llm.NewLoggingTransport(http.DefaultTransport, log, clk)})` only when debug is on. With debug off, the HTTP path is unchanged.

`service/` importing `log/slog` is a stdlib import and is allowed by the layer rules. No archtest allowlist entry is needed.

### 4.3 Session attributes through context (`internal/core/logctx.go`)

```go
func WithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context
func LogAttrs(ctx context.Context) []slog.Attr
```

The key is an unexported struct type. `WithLogAttrs` replaces, not appends, attributes that have the same key, so a child run's `session` and `depth` replace the parent's.

- `Runner.Run` does `ctx = core.WithLogAttrs(ctx, root, session, depth, agent)` right after `register`, and derives its own logger with `log.With(core.LogAttrs(ctx)...)` for the run.
- That ctx flows through `llm.Stream`, fantasy, and the SDK into `req.Context()`. The transport adds `core.LogAttrs(req.Context())` to every `http` line.
- The task tool logs `task spawn` and `task end` with the parent's attributes, and the child's own `run` lines carry the child's attributes, because `Runner.Run` sets them from the child `RunContext`.

### 4.4 Timing

Durations come from `clock.Clock`: the Runner's `Deps.Clock`, a clock added to the task tool, and the one passed to `NewLoggingTransport`. Tests use `clock.NewFake`, so no test calls `time.Now` or `time.Sleep`. The `time=` attribute written by slog itself uses the real clock; tests drop it with `ReplaceAttr`.

### 4.5 Body tracking in the transport

The response body is wrapped in a `ReadCloser` that counts bytes and records the first non-EOF read error. It logs `http body done` exactly once, at the first of EOF, a read error, or `Close`. For a non-2xx status, the wrapper first reads up to 4 KB, logs it on the `http response` line, and serves those bytes back before the rest of the body, so the SDK sees the original stream.

## 5. Testing

- **`internal/app`:** with `JIG_DEBUG=1` and a temp workdir, `openDebugLog` creates `jig-debug.log`, a second call truncates it, and a line written through the logger lands in it. With `JIG_DEBUG` unset, no file is created and `Enabled` is false. An unwritable workdir returns an error, and the caller continues.
- **`internal/core`:** `WithLogAttrs` replaces same-key attributes and keeps the others, and `LogAttrs` on a bare context is empty.
- **`internal/service/agent`**, with a `bytes.Buffer` text handler with `time` dropped and `llmtest` scripts:
  - A normal two-step run logs `run start`, `step start`/`step end` twice, and `run end outcome=done steps=2`.
  - A run hitting `MaxSteps` logs `outcome=max_steps`.
  - A retryable error, then success, logs `attempt failed attempt=1 retryable=true delay=1s`.
  - A failure after the first stream event logs `received=true giving_up=true` and `outcome=failed`.
  - Cancellation logs `outcome=cancelled`.
  - A blocked tool call and an erroring tool call log `blocked=true` and `is_error=true`.
- **`internal/service/task`:** `task spawn` and `task end` carry parent and child IDs. A child run error appears raw as `err` on `task end outcome=error`. The depth limit logs `task rejected`.
- **`internal/client/llm`**, against an `httptest.Server`:
  - A 200 streamed body logs `status=200`, `ttfb`, `req_id`, and `http body done` with the correct byte count.
  - A 502 with a 10 KB body logs `body` capped at 4 KB, the client still reads all 10 KB unchanged, and an `Authorization` header sent by the client never appears in the log.
  - A body cut off with a hijacked connection logs `read_err`.
  - A context carrying `WithLogAttrs` puts `session=` on the `http` lines.
  - Each factory given `WithHTTPClient` sends its requests through that client.
- **`e2e`:** `jig run` with `JIG_DEBUG=1` against the `jigtest` provider leaves `jig-debug.log` in the workdir containing `cat=app`, `cat=run`, and `cat=step`. Without `JIG_DEBUG`, no file is created.

## 6. Docs

- **AGENTS.md:**
  - Add `internal/app/debuglog.go` and `core.WithLogAttrs` to the relevant sections.
  - Add an invariant: the debug logger is always passed in, never stored in a package var. Nothing logs headers, prompts, tool input or output, or streamed text; the only body logged is a non-2xx response, capped at 4 KB. Every string value passes `SanitizeLine` in the handler.
  - Add a shared-code row: "Tag log lines with the run's session" → `core.WithLogAttrs(ctx, ...)` / `core.LogAttrs(ctx)`.
- **README.md:** a short "Debugging" section covering `JIG_DEBUG=1`, where the file goes, that it is truncated on each start, and to gitignore it.
