# JIG_DEBUG Debug Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `JIG_DEBUG=1` writes a `slog` debug log, `jig-debug.log` in the working directory. It records runs, steps, retries, tool calls, subagent spawns, and HTTP traffic, each line tagged with the run's session.

**Architecture:** `internal/app` builds one `*slog.Logger`, which discards everything unless `JIG_DEBUG` is set, and passes it into `agent.Deps`, the task tool, and a logging `http.RoundTripper` that the provider factories use. Session attributes travel in the request context (`core.WithLogAttrs`), so HTTP lines carry the same `session=`/`depth=` as the run that made them.

**Tech Stack:** Go 1.27, `log/slog` (`TextHandler`, `DiscardHandler`), `net/http`, `net/http/httptest`, fantasy provider `WithHTTPClient` options.

**Spec:** `docs/superpowers/specs/2026-09-30-jig-debug-log-design.md`

## Global Constraints

- Every commit passes `make check` (build, test with `-race`, jigtest-tagged tests, lint with and without `--build-tags jigtest`, `gofmt -l .` empty).
- No package-level mutable vars. The logger is always passed in. A nil `*slog.Logger` given to any constructor or `Deps` is replaced with `slog.New(slog.DiscardHandler)`.
- No `time.Now`/`time.Sleep` in `_test.go` files. Durations come from `clock.Clock`; tests use `clock.NewFake`.
- Every line is `level=DEBUG` with a `cat=` attribute, one of `app`, `run`, `step`, `retry`, `http`, `task`, `tool`.
- Metadata only. Never log headers (except the request ID value), prompts, tool input or output, or streamed text. The only body logged is a non-2xx HTTP response body, capped at **4096 bytes**.
- The file is `filepath.Join(workDir, "jig-debug.log")`, opened `O_CREATE|O_TRUNC|O_WRONLY`, mode `0644`.
- Log messages (the `msg=` text) are exactly the names in the spec: `debug log start`, `run start`, `run end`, `step start`, `step end`, `attempt failed`, `http request`, `http response`, `http body done`, `http error`, `task spawn`, `task end`, `task rejected`, `tool call`.
- Layer rules hold: `service/` and `client/` import only stdlib `log/slog`, never `bubbles/ansi`. Sanitizing happens in `internal/app`'s handler.

**One change from the spec, §4.1:** the log is opened in `newRuntime`, not `loadEnv`. `loadEnv` also runs for `jig models` and `jig sessions`, which would truncate a running session's log. Only the TUI and `jig run` build a runtime, and `newRuntime` still runs before the TUI takes the terminal, so the warning path is unchanged.

## Review Focus

- **Parallel subagents.** The task tool is `Concurrent`, so two children log at the same time. Each child's `http` lines must carry that child's `session=`, never its sibling's. Covered by Task 4's `TestLoggingTransport_ConcurrentRequestsKeepTheirAttrs`.
- **SDK closes the body early.** The SDK may call `Close` without reading to EOF, or call `Close` after EOF. `http body done` must be logged exactly once in both cases. Covered by Task 4's `TestLoggingTransport_BodyDoneLoggedOnce`.
- **Empty or tiny error bodies.** A 502 with an empty body, or one under 4 KB, must not block and must log what's there. Covered by Task 4's `TestLoggingTransport_EmptyErrorBody`.
- **Hostile bytes in a provider error body** (ANSI escapes, newlines) must not break a log line or reach a terminal that later `cat`s the file. Covered by Task 7's `TestDebugLog_SanitizesStrings`.
- **`jig sessions` or `jig models` run while a TUI is running** must not truncate the TUI's log. Covered by Task 7's `TestLoadEnv_DoesNotOpenDebugLog`.

---

### Task 1: Context log attributes and a test logger

**Files:**
- Create: `internal/core/logctx.go`, `internal/core/logctx_test.go`
- Create: `internal/core/logtest/logtest.go`, `internal/core/logtest/logtest_test.go`

**Interfaces:**
- Produces:
  - `core.WithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context`: returns a child ctx whose attrs are the parent's with same-key entries replaced by `attrs`. It keeps the parent's key order and appends new keys.
  - `core.LogAttrs(ctx context.Context) []slog.Attr`: returns a copy, or nil on a bare ctx.
  - `core.LogArgs(ctx context.Context) []any`: the same attrs as `[]any`, for `logger.With(...)` and `logger.Debug(msg, ...)`.
  - `logtest.New() (*slog.Logger, *logtest.Buffer)`: a debug-level `TextHandler` over a mutex-guarded buffer, with the `time` attr dropped.
  - `(*Buffer).Lines() []string`: complete lines, without trailing newline.
  - `(*Buffer).Find(msg string) []string`: lines containing `msg="<msg>"` (or `msg=<msg>` when it has no spaces).

- [ ] **Step 1: Write the failing tests**

`internal/core/logctx_test.go`:
```go
func TestWithLogAttrs_ReplacesSameKeyKeepsOthers(t *testing.T) {
	ctx := core.WithLogAttrs(context.Background(), slog.String("root", "r"), slog.String("session", "p"), slog.Int("depth", 0))
	ctx = core.WithLogAttrs(ctx, slog.String("session", "c"), slog.Int("depth", 1))
	got := fmt.Sprint(core.LogAttrs(ctx))
	if got != "[root=r session=c depth=1]" {
		t.Errorf("LogAttrs = %s", got)
	}
}

func TestLogAttrs_BareContextIsEmpty(t *testing.T) {
	if a := core.LogAttrs(context.Background()); len(a) != 0 {
		t.Errorf("LogAttrs = %v, want empty", a)
	}
}

func TestLogAttrs_ParentUnchanged(t *testing.T) {
	parent := core.WithLogAttrs(context.Background(), slog.String("session", "p"))
	_ = core.WithLogAttrs(parent, slog.String("session", "c"))
	if got := fmt.Sprint(core.LogAttrs(parent)); got != "[session=p]" {
		t.Errorf("parent attrs = %s", got)
	}
}
```

`internal/core/logtest/logtest_test.go`:
```go
func TestNew_DropsTimeAndFindsByMsg(t *testing.T) {
	log, buf := logtest.New()
	log.Debug("run start", "cat", "run", "steps", 2)
	lines := buf.Find("run start")
	if len(lines) != 1 || lines[0] != `level=DEBUG msg="run start" cat=run steps=2` {
		t.Errorf("lines = %q", lines)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/core/ ./internal/core/logtest/`
Expected: FAIL (undefined: `core.WithLogAttrs`, package `logtest` not found).

- [ ] **Step 3: Implement**

`logctx.go`: an unexported `logAttrsKey struct{}` holding a `[]slog.Attr`. Copy on write, and never mutate the parent's slice. `logtest.go`: `Buffer` wraps `bytes.Buffer` with a `sync.Mutex` and implements `io.Writer`. `New` uses `slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: drop time at top level}`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race ./internal/core/ ./internal/core/logtest/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/logctx.go internal/core/logctx_test.go internal/core/logtest/
git commit -m "feat(core): context log attrs and logtest helper"
```

---

### Task 2: Runner logs runs, steps, and retries

**Files:**
- Modify: `internal/service/agent/runner.go` (`Deps`, `NewRunner`, `Run`, `step`), `internal/service/agent/retry.go` (`stream`), `internal/service/agent/stream.go` (`consume`)
- Create: `internal/service/agent/log_test.go`

**Interfaces:**
- Consumes: `core.WithLogAttrs`, `core.LogArgs`, `logtest.New` (Task 1).
- Produces:
  - `agent.Deps.Log *slog.Logger` (nil → discard).
  - `Runner.Run` sets `core.WithLogAttrs(ctx, root, session, depth, agent)` right after `register`, where `root` is `rc.RootID`, falling back to `rc.SessionID` when empty. Every later call, including `llm.Stream`, gets that ctx.

Log records, all with `cat=` and `core.LogArgs(ctx)`:
- `run start`: `model` (`rc.Model.String()`), `max_steps`. Logged after `maxSteps` is resolved.
- `run end`: `outcome`, `steps`, `in`, `out`, `cost`, `dur`, and `err` when failed. `outcome` is `cancelled` when `ctx.Err() != nil`, `failed` on any other error, `max_steps` when `stopAtMaxSteps` ran, else `done`. Logged from one `defer` after `run start`. Setup failures before `run start` (saving the user message, `LLMs.For`) log only `run end outcome=failed steps=0`.
- `step start`: `step`, `messages` (`len(req.Messages)`), `tools` (`len(req.Tools)`). Logged after `buildRequest`.
- `step end`: `step`, `first_event`, `stream`, `tools_dur`, `in`, `out`, `cache_read`, `cache_write`, `calls`, `status`. Logged on success and on abort. `first_event` is the time from the last attempt's start to its first event (0 if none arrived). `stream` is the time from the first attempt's start to stream end.
- `attempt failed` (`cat=retry`): `attempt`, `received`, `retryable`, `retry_after`, and then either `delay` or `giving_up=true`, plus `err`.

`step` needs the step number: pass `n` from `Run`'s loop into `step(ctx, st, n)`. `consume` records its first-event time in a `*stepTiming` (`start`, `firstEvent time.Time`) owned by `step` and passed through `stream`. All times come from `r.d.Clock.Now()`.

- [ ] **Step 1: Write the failing tests** in `log_test.go`

Use `newFixture`. Set `f.deps.Log, buf = logtest.New()`.

```go
func TestRunnerLog_TwoStepRun(t *testing.T)
// llm: Calls(Call("c1","echo",`{}`)), Text("done"), with an echo tool registered as in existing runner tests.
// want: exactly 1 "run start" line containing "cat=run", "session=ses_test", "depth=0", "model=prov/mod", "max_steps=100";
//       2 "step start" and 2 "step end" lines; "step end" for step=1 contains "calls=1" and step=2 "calls=0";
//       1 "run end" line containing "outcome=done", "steps=2".

func TestRunnerLog_MaxSteps(t *testing.T)
// f.rc.Agent.MaxSteps = 2; llm always calls a tool. want "run end" contains "outcome=max_steps" and "steps=2".

func TestRunnerLog_RetryThenSuccess(t *testing.T)
// same script and clock driving as TestRunner_RetriesBeforeFirstEvent.
// want 1 "attempt failed" line containing "cat=retry", "attempt=1", "received=false", "retryable=true", "delay=1s", `err=overloaded`;
//      "run end" contains "outcome=done".

func TestRunnerLog_FailureAfterFirstEvent(t *testing.T)
// llm: Turn{Events: [StreamText "partial"], Err: &core.LLMError{Retryable: true, Err: errors.New("stream reset")}}.
// want "attempt failed" contains "received=true", "giving_up=true"; "run end" contains "outcome=failed", `err="stream reset"`.

func TestRunnerLog_Cancelled(t *testing.T)
// llm: Turn{Hang: true}; cancel ctx after f.rec.started fires. want "run end" contains "outcome=cancelled".

func TestRunnerLog_StepTiming(t *testing.T)
// llm: Turn{Gate: g, Events: text+finish}; after the runner blocks on the gate, f.clk.Advance(3*time.Second), close(g).
// want "step end" contains "first_event=3s".
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/agent/ -run TestRunnerLog`
Expected: FAIL (`f.deps.Log` undefined).

- [ ] **Step 3: Implement** the records above in `runner.go`, `retry.go`, and `stream.go`.

- [ ] **Step 4: Run to verify they pass, and the rest of the package still passes**

Run: `go test -race ./internal/service/agent/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/agent/
git commit -m "feat(agent): debug-log runs, steps, and retries"
```

---

### Task 3: Executor logs tool calls

**Files:**
- Modify: `internal/service/agent/exec.go` (`call`, `resolveAndRun`)
- Test: `internal/service/agent/log_test.go`

**Interfaces:**
- Consumes: `Deps.Log`, and the ctx carrying `core.WithLogAttrs` (Task 2).
- Produces: `resolveAndRun(ctx, call) (core.ToolResult, callOutcome)`, where `callOutcome` is an unexported string type with values `ran`, `blocked`, `unknown`, `invalid`, `cancelled`. `guarded` passes it through, and a panic counts as `ran`.

Record `tool call` (`cat=tool`), logged in `call`: `tool`, `call_id`, `dur` (Clock around `guarded`), `out_bytes` (before `capOutput`), `out_bytes_capped` (after), `is_error`, `blocked` (outcome `blocked`), and `reason` only when the outcome is `unknown`, `invalid`, or `cancelled`.

- [ ] **Step 1: Write the failing tests**

```go
func TestRunnerLog_ToolCall(t *testing.T)
// a tool returning 60*1024 bytes of "x". want "tool call" contains "cat=tool", "tool=big", "call_id=c1", "out_bytes=61440",
// "out_bytes_capped=" (value = len of capped output), "is_error=false", "blocked=false", "session=ses_test".

func TestRunnerLog_ToolBlockedAndUnknown(t *testing.T)
// a ToolHook whose Before blocks "echo"; the model calls echo (c1) and "nope" (c2).
// want the c1 line to contain "blocked=true"; the c2 line to contain "is_error=true", "blocked=false", "reason=unknown".
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/agent/ -run TestRunnerLog_Tool`
Expected: FAIL (no `tool call` lines).

- [ ] **Step 3: Implement** `callOutcome` and the record.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race ./internal/service/agent/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/agent/
git commit -m "feat(agent): debug-log tool calls"
```

---

### Task 4: Logging HTTP transport

**Files:**
- Create: `internal/client/llm/httplog.go`, `internal/client/llm/httplog_test.go`

**Interfaces:**
- Consumes: `core.LogArgs` (Task 1), `clock.Clock`.
- Produces: `llm.NewLoggingTransport(base http.RoundTripper, log *slog.Logger, clk clock.Clock) http.RoundTripper`.

Behavior (each record has `cat=http` plus `core.LogArgs(req.Context())`):
- `http request`: `method`, `host`, `path` (no query).
- On a `base.RoundTrip` error, `http error`: `err`, `dur`. The error is returned unchanged.
- `http response`: `status`, `ttfb`, and `req_id` (the first non-empty of `request-id`, `x-request-id`, `cf-ray`, omitted if none). For `status >= 300`, it reads up to 4096 bytes with `io.ReadFull` on an `io.LimitReader`, treating `io.ErrUnexpectedEOF`/`io.EOF` as the end, and adds `body` (raw string). It then sets `resp.Body` to a reader over `io.MultiReader(bytes.NewReader(prefix), original)` with the original closer.
- The body wrapper counts bytes and logs `http body done` (`bytes`, `dur` from request start, and `read_err` for a non-EOF read error) exactly once, guarded by `sync.Once`, on EOF, the first non-EOF error, or `Close`.

- [ ] **Step 1: Write the failing tests** (`httptest.Server`, `logtest.New`, `clock.NewFake`; set the request's ctx with `core.WithLogAttrs(ctx, slog.String("session","ses_c"))`)

```go
func TestLoggingTransport_OKStream(t *testing.T)
// server: header "request-id: req_1", 200, writes "abc" then "def" with flush. read all, close.
// want "http request" contains "method=POST", "host=127.0.0.1:", "path=/v1/messages", "session=ses_c";
//      "http response" contains "status=200", "req_id=req_1", and no "body=";
//      exactly 1 "http body done" containing "bytes=6" and no "read_err".

func TestLoggingTransport_ErrorBodyCapped(t *testing.T)
// server: 502 with a 10*1024-byte body; the client request sets "Authorization: Bearer sk-secret".
// want the client reads all 10240 bytes unchanged; "http response" contains "status=502" and a body= value of exactly 4096 bytes;
//      no line in buf contains "sk-secret" or "Authorization".

func TestLoggingTransport_EmptyErrorBody(t *testing.T)
// server: 502, empty body. want the round trip returns; "http response" contains `body=""`.

func TestLoggingTransport_TruncatedStream(t *testing.T)
// server: Content-Length: 100, writes 10 bytes, hijacks and closes the connection.
// want "http body done" contains "bytes=10" and "read_err=".

func TestLoggingTransport_TransportError(t *testing.T)
// base is a RoundTripper func returning errors.New("dial refused"). want "http error" contains `err="dial refused"` and the error is returned.

func TestLoggingTransport_BodyDoneLoggedOnce(t *testing.T)
// two cases: (a) read to EOF then Close; (b) Close without reading. each yields exactly 1 "http body done".

func TestLoggingTransport_ConcurrentRequestsKeepTheirAttrs(t *testing.T)
// 8 goroutines, each with session=ses_<i>, each doing one request. for each i, every line containing "session=ses_<i>"
// comes from a request by goroutine i (check the path /req/<i>), and each i has exactly 1 "http request" line.
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/client/llm/ -run TestLoggingTransport`
Expected: FAIL (undefined: `NewLoggingTransport`).

- [ ] **Step 3: Implement** `httplog.go` as described. `ttfb` and `dur` come from `clk.Now()` differences. In the tests, use a fake clock and assert only that the keys are present.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race ./internal/client/llm/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/client/llm/httplog.go internal/client/llm/httplog_test.go
git commit -m "feat(llm): logging HTTP transport for debug log"
```

---

### Task 5: Provider factories take an HTTP client

**Files:**
- Modify: `internal/client/llm/providers.go`
- Test: `internal/client/llm/providers_test.go`

**Interfaces:**
- Produces:
  - `llm.Option` (`func(*factoryOpts)`), `llm.WithHTTPClient(c *http.Client) Option`, and `llm.Factories(opts ...Option) []ext.ProviderFactory`.
  - Each factory struct gets a `hc *http.Client` field. When it's non-nil, the factory appends the provider's option: `anthropic.WithHTTPClient(hc)`, `openai.WithHTTPClient(hc)`, `openaicompat.WithHTTPClient(hc)`, `openrouter.WithHTTPClient(hc)`, `google.WithHTTPClient(hc)`. With no option, the options passed to each provider are identical to today's.

- [ ] **Step 1: Write the failing test**

```go
func TestFactories_WithHTTPClientRoutesEveryProvider(t *testing.T)
// rt := a counting RoundTripper returning &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}, Request: req}.
// for each f in llm.Factories(llm.WithHTTPClient(&http.Client{Transport: rt})):
//   llm, err := f.New(core.ProviderInfo{Endpoint: "http://example.invalid"}, core.ProviderConfig{APIKey: "k"}, "m")
//   drain llm.Stream(ctx, core.LLMRequest{Messages: one user text message}) and ignore the error
//   want rt's count for f.Type() >= 1.
// (Subtest per f.Type(); use google's own endpoint if it rejects the override.)
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/client/llm/ -run TestFactories_WithHTTPClient`
Expected: FAIL (undefined: `llm.WithHTTPClient`).

- [ ] **Step 3: Implement.** Existing callers of `llm.Factories()` keep compiling, because the options are variadic.

- [ ] **Step 4: Run to verify it passes, along with existing factory tests**

Run: `go test -race ./internal/client/llm/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/client/llm/providers.go internal/client/llm/providers_test.go
git commit -m "feat(llm): factories accept an HTTP client"
```

---

### Task 6: Task tool logs subagents

**Files:**
- Modify: `internal/service/task/task.go` (`New`, `Run`)
- Modify: `internal/app/registry.go:80` (the call site; pass `d.clk, nil` for now, and Task 7 passes the real logger)
- Test: `internal/service/task/task_test.go` (update existing `New(...)` calls), and a new `internal/service/task/log_test.go`

**Interfaces:**
- Consumes: `core.LogArgs`, `logtest.New`.
- Produces: `task.New(s Sessions, a Agents, r Runner, pub event.Publisher, clk clock.Clock, log *slog.Logger) ext.Tool`.

Records (`cat=task`, plus `core.LogArgs(ctx)`, which here are the parent's attrs):
- `task rejected`: `reason` (the exact message returned in the `ToolError`), for the depth limit, an unknown agent, model resolution failure, and a `resolveChild` `errMsg`.
- `task spawn`: `parent`, `child`, `resumed` (`in.SessionID != ""`), `subagent`, `model`, `depth` (child depth).
- `task end`: `child`, `dur`, `outcome` (`ok`, `error`, or `cancelled` when `ctx.Err() != nil`), and `err` when the outcome isn't `ok`.

- [ ] **Step 1: Write the failing tests**

```go
func TestTaskLog_SpawnAndEnd(t *testing.T)
// ctx := core.WithLogAttrs(ctx, slog.String("session","parent1")); fakeRunner returns a text message.
// want "task spawn" contains "cat=task", "parent=parent1", "child=child1", "resumed=false", "subagent=", "depth=1";
//      "task end" contains "child=child1", "outcome=ok", "dur=".

func TestTaskLog_ChildErrorIsRaw(t *testing.T)
// fakeRunner returns errors.New("upstream proxy error: 502"). want "task end" contains "outcome=error", `err="upstream proxy error: 502"`.

func TestTaskLog_DepthRejected(t *testing.T)
// rc.Depth = task.MaxDepth. want "task rejected" contains `reason="subagent depth limit (3) reached"`.
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/task/`
Expected: FAIL (wrong number of args to `New`).

- [ ] **Step 3: Implement**, update the existing tests' `New` calls, and update `registry.go:80` to `task.New(d.sessions, d.agents, d.proxy, d.bus, d.clk, nil)`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race ./internal/service/task/ ./internal/app/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/task/ internal/app/registry.go
git commit -m "feat(task): debug-log subagent spawns and outcomes"
```

---

### Task 7: Wire `JIG_DEBUG` in `internal/app`, plus e2e

**Files:**
- Create: `internal/app/debuglog.go`, `internal/app/debuglog_test.go`
- Modify: `internal/app/services.go` (`runtime`, `newRuntime`, `newChat`, `newRunner`, `providerView`, `close`), `internal/app/registry.go` (`registryDeps`, `addTools`, `addProviders`, `providerFactories`)
- Test: `e2e/e2e_test.go` (a new `TestE2E_DebugLog`)

**Interfaces:**
- Consumes: `agent.Deps.Log` (Task 2), `llm.NewLoggingTransport` (Task 4), `llm.WithHTTPClient` (Task 5), `task.New(..., clk, log)` (Task 6).
- Produces:
  - `openDebugLog(getenv func(string) string, workDir string) (*slog.Logger, io.Closer, error)`. When unset, it returns `slog.New(slog.DiscardHandler)` and a no-op closer. On an open error, it returns the discard logger, a no-op closer, and the error.
  - `debugHandlerOptions() *slog.HandlerOptions`: `Level: slog.LevelDebug`. `ReplaceAttr` formats top-level `time` as UTC `"2006-01-02T15:04:05.000000Z07:00"`, passes `KindString` values and `error` values (`KindAny` holding an `error`) through `ansi.SanitizeLine` as strings, and leaves everything else alone.
  - `runtime.log *slog.Logger`, `runtime.logFile io.Closer`. `newRuntime` opens the log first. On error it prints `warning: JIG_DEBUG: <err>` through `printLine(errw, ...)` and continues. It then logs `debug log start` (`cat=app`, `version`, `workdir`, `pid`, `default_model`). `rt.close()` closes `logFile` last.
  - `registryDeps.log *slog.Logger`, `registryDeps.httpClient *http.Client`. The client is nil unless `JIG_DEBUG` is set, in which case it is `&http.Client{Transport: llm.NewLoggingTransport(http.DefaultTransport, rt.log, clk)}`.
  - `providerView(hc *http.Client)` and `providerFactories(hc *http.Client)`. These pass `llm.WithHTTPClient(hc)` only when `hc != nil`. `extraProviders()` is unchanged.
  - `newRunner` sets `Deps.Log: rt.log`, and `addTools` passes `d.clk, d.log` to `task.New`.

- [ ] **Step 1: Write the failing tests** in `debuglog_test.go`

```go
func TestDebugLog_OffCreatesNothing(t *testing.T)
// getenv returns "" for JIG_DEBUG. want no error, log.Enabled(ctx, slog.LevelDebug) == false, and no jig-debug.log in dir.

func TestDebugLog_OnWritesAndTruncates(t *testing.T)
// JIG_DEBUG=1: open, log.Debug("first"), close; open again, log.Debug("second"), close.
// want the file's content to contain `msg=second` and not `msg=first`, with mode 0644.

func TestDebugLog_UnwritableDirReturnsError(t *testing.T)
// dir chmod 0500 (skip if os.Getuid()==0). want err != nil and a logger that is non-nil and disabled.

func TestDebugLog_SanitizesStrings(t *testing.T)
// a TextHandler with debugHandlerOptions() over a bytes.Buffer; log.Debug("x", "body", "a\x1b[31mb\nc", "err", errors.New("e\x1b]0;t\x07")).
// want the output to contain no "\x1b" and exactly one "\n" (the line end).

func TestLoadEnv_DoesNotOpenDebugLog(t *testing.T)
// use the existing loadEnv test setup with getenv returning "1" for JIG_DEBUG, and pre-create jig-debug.log containing "keep".
// want the file content still "keep" after loadEnv.

func TestNewRuntime_DebugLogStart(t *testing.T)
// following app_test.go:285-289 with JIG_DEBUG=1: newRuntime, then rt.close(). want work/jig-debug.log to contain
// `msg="debug log start"`, "cat=app", "pid=", "default_model=".
```

And in `e2e/e2e_test.go`:
```go
func TestE2E_DebugLog(t *testing.T)
// setup as TestE2E_TextReply, with env.vars = append(env.vars, "JIG_DEBUG=1"); runPrompt.
// want exit 0 and env.work/jig-debug.log to contain "cat=app", `msg="run start"`, `msg="step end"`, "outcome=done".
// Then run a second fresh env without JIG_DEBUG, and want no jig-debug.log in its work dir.
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/app/ -run 'TestDebugLog|TestLoadEnv_DoesNotOpenDebugLog|TestNewRuntime_DebugLogStart'`
Expected: FAIL (undefined: `openDebugLog`).

- [ ] **Step 3: Implement** `debuglog.go` and the wiring above.

- [ ] **Step 4: Run to verify everything passes**

Run: `make check`
Expected: all targets pass, including the e2e `TestE2E_DebugLog` and the archtest.

- [ ] **Step 5: Commit**

```bash
git add internal/app/ e2e/e2e_test.go
git commit -m "feat(app): JIG_DEBUG writes jig-debug.log"
```

---

### Task 8: Docs

**Files:**
- Modify: `AGENTS.md`, `README.md`, `.gitignore`

- [ ] **Step 1: Update `AGENTS.md`**
  - Package map: `internal/core/logtest/  slog test logger (Buffer, Find)`.
  - Invariants: add the bullet from spec §6. The debug logger is always passed in, never stored in a package var, and nil means discard. Nothing logs headers, prompts, tool input or output, or streamed text; the only body logged is a non-2xx response, capped at 4 KB. Every string value passes `SanitizeLine` in `internal/app`'s handler. The log is opened only by `newRuntime` (TUI and `jig run`).
  - Shared-code table rows:
    - "Tag log lines with the run's session" → `core.WithLogAttrs(ctx, ...)` / `core.LogArgs(ctx)`
    - "Assert on debug log lines in a test" → `logtest.New()` / `.Find(msg)`
- [ ] **Step 2: Update `README.md`**: add a "Debugging" section. It covers `JIG_DEBUG=1 jig` (or `jig run`), which writes `jig-debug.log` in the working directory and truncates it each start; what's in it (categories); what's never in it; and a reminder to gitignore it.
- [ ] **Step 3: Add `jig-debug.log` to `.gitignore`.**
- [ ] **Step 4: Verify**

Run: `make check`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md README.md .gitignore
git commit -m "docs: JIG_DEBUG debug log"
```
