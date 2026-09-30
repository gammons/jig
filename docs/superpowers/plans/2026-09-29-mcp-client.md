# MCP Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** jig connects to MCP servers defined in `[mcp.servers.*]` or a project `.mcp.json`, exposes their tools to the model as `mcp__<server>__<tool>`, signs in to OAuth servers only when the user asks, and shows each server's state in the TUI.

**Architecture:**
- `client/mcp` wraps go-sdk transports and sessions, and exposes jig-owned types only.
- `client/mcpauth` holds the OAuth plumbing: the gated code fetcher, the loopback listener, and the browser opener.
- `data/mcptokens` persists sign-in records.
- `service/mcp.Manager` owns connections and states and implements the new `ext.ToolSource`.
- The Runner recomputes its allowed tools each step.
- Config, trust, and permissions gain MCP servers and glob tool keys.
- The UI reaches the Manager only through `core.MCPService` and `MCPServerChanged` events.

**Tech Stack:** Go 1.27, `github.com/modelcontextprotocol/go-sdk` v1.8.0 (`mcp`, `auth`, `oauthex`), `golang.org/x/oauth2`, BurntSushi/toml, bubbletea v2.

**Spec:** `docs/superpowers/specs/2026-09-29-mcp-client-design.md` (§ references below point into it).

## Global Constraints

- `make check` must pass before every commit.
- **Layers:**
  - `internal/service/...` never imports `internal/client/...`, `internal/data/...`, or `github.com/modelcontextprotocol/go-sdk/...`.
  - Only `internal/client/mcp` and `internal/client/mcpauth` import the SDK.
  - `ui` does no I/O; every port call goes in a Cmd in `cmds.go`.
- No `time.Now` or `time.Sleep` in `_test.go` files. Deadlines come from `clock.Clock`.
- Files are at most 500 lines. App functions are at most 40 lines.
- Every server- or config-derived string passes `ansi.SanitizeLine`/`Sanitize` before it is rendered.
- **Names:**
  - Server names match `^[a-z0-9_-]+$`.
  - Wrapped tool names are `mcp__<server>__<tool>`; characters outside `[A-Za-z0-9_-]` become `_`.
  - A name over 64 characters becomes its first 55 characters + `_` + the first 8 hex characters of `sha256(full name)`.
- **Defaults:** `startup_timeout` 10s, `tool_timeout` 2m, callback timeout 5m.
- **Token storage:**
  - Path: `<DataDir>/mcp-auth/<hex sha256(url)>.json`.
  - Permissions: the directory 0700, files 0600.
  - Writes go through `atomicfile.Write`.
- **Callback listener:** binds `127.0.0.1` only and serves only `GET /callback`.
- **Dynamic client registration metadata:**
  - `client_name` `"jig"`
  - `grant_types` `["authorization_code","refresh_token"]`
  - `token_endpoint_auth_method` `"none"`
- **Exact copy:**
  - IsError hint: `mcp server "<name>" needs sign-in; open ctrl+p → MCP to sign in`
  - Timeout: `mcp: <tool> timed out after <d>`
  - Callback page: `Signed in to jig. You can close this tab.`
  - Headless warning: `mcp: <name> needs sign-in; run "jig mcp auth <name>"`

## Review Focus

1. **A stdio server that writes junk to stdout or never answers `initialize`.** It must hit `startup_timeout`, become `failed: timed out after 10s`, and have its process group killed. It must never hang `jig run` or `Close`. The test goes in Task 5.
2. **The server list changing while a run is mid-step.** `list_changed` or a disconnect between the model's call and execution gives the "unavailable" result, and `Tools()` never hands out a slice the Manager later mutates. The test goes in Task 8.
3. **A project `.mcp.json` shadowing a global server name with a different URL.** The global server's stored token is never sent, and trust effects show the override line. The tests go in Tasks 3 and 7.
4. **Two jig processes sharing a refresh token.** `invalid_grant` gives one re-read-and-retry. It must never loop or open a browser. The test goes in Task 7.
5. **Server-controlled text containing terminal escapes** (tool descriptions, error reasons, the auth URL). It is sanitized in the sidebar, the picker, the trust dialog, and `jig mcp list`. The tests go in Tasks 10 and 11.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/mcp.go` (create) | `MCPServer`, `MCPOAuth`, `MCPState`, `MCPServerStatus`, `MCPService`, `ErrMCPNeedsAuth` |
| `internal/core/config.go` (modify) | `Config.MCP MCPConfig` |
| `internal/core/event/event.go` (modify) | `MCPServerChanged` |
| `internal/core/ext/ext.go`, `registry.go` (modify) | `ToolSource`, `SetToolSource`, `View.ToolSource` |
| `internal/service/agent/runner.go`, `exec.go` (modify) | per-step allowed tools |
| `internal/service/permission/rules.go` (modify) | `resolveRule` (glob keys) inside `Effective` |
| `internal/service/agents/tools.go` (modify) | glob `tools` entries, glob-deny removal |
| `internal/data/config/mcp.go` (create) | TOML `[mcp]` DTO → `core.MCPConfig`, validation |
| `internal/data/config/mcpjson.go` (create) | `.mcp.json` parsing, `${VAR}` expansion |
| `internal/data/config/load.go`, `merge.go` (modify) | interleaved layer order, `ProjectMCPFiles`, merging MCP |
| `internal/service/trust/mcp.go` (create) | `restrictMCP`, `mcpEffects` |
| `internal/app/trust.go` (modify) | hash covers `.mcp.json` |
| `internal/data/mcptokens/store.go` (create) | `Record`, `Store` (Load/Save/Delete) |
| `internal/client/mcp/{conn.go,convert.go,stdio.go}` (create) | `Dial`, `Conn`, jig-side `Tool`/`Content`/`CallResult` |
| `internal/client/mcpauth/{gate.go,listener.go,browser.go,handler.go}` (create) | gated fetcher, loopback listener, opener, `NewHandler` |
| `internal/service/mcp/{manager.go,state.go,tool.go,content.go,auth.go}` (create) | Manager, state machine, `ext.Tool` wrapper, content mapping, the Authenticate flow |
| `internal/app/mcp.go` (create) | build the Manager, adapters, `mcpPort`, Start/Close wiring |
| `internal/app/mcpcmd.go` (create) | `jig mcp list|auth|logout` |
| `internal/ui/mcp.go` (create) | the sidebar section, badge count, picker levels, Cmds |
| `internal/archtest/layers_test.go` (modify) | the no-SDK-in-service rule |
| `e2e/testdata/mcpfake/main.go` (create) | a scripted stdio MCP server |
| `AGENTS.md`, `docs/config.example.toml`, `README.md` (modify) | docs |

---

### Task 1: Core types, `ToolSource`, and per-step tools in the Runner

**Files:**
- Create: `internal/core/mcp.go`
- Modify: `internal/core/config.go`, `internal/core/event/event.go`, `internal/core/ext/ext.go`, `internal/core/ext/registry.go`, `internal/service/agent/runner.go`, `internal/service/agent/exec.go`
- Test: `internal/core/ext/registry_test.go`, `internal/service/agent/toolsource_test.go`

**Interfaces:**
- Produces:
  - `core.MCPTransport` (`"stdio"|"http"|"sse"`)
  - `core.MCPOAuth{ClientID, ClientSecret string; Scopes []string}`
  - `core.MCPServer{Name, Source, Transport MCPTransport; Command string; Args []string; Env map[string]string; Cwd, URL string; Headers map[string]string; OAuth MCPOAuth; StartupTimeout, ToolTimeout time.Duration; Enabled *bool}`
  - `core.MCPConfig{Servers map[string]MCPServer; Disabled []string}`
  - `Config.MCP MCPConfig`
  - `core.MCPState` constants: `MCPConnecting`, `MCPReady`, `MCPNeedsAuth`, `MCPAuthenticating`, `MCPFailed`, `MCPDisabled`. The string values are the spec §4.1 names.
  - `core.MCPServerStatus{Name, Source string; Transport MCPTransport; State MCPState; Tools int; ToolNames []string /*remote name + " — " + description, sorted*/; Err, AuthURL string; HasToken bool}`
  - `core.MCPService`, the §8.1 interface
  - `core.ErrMCPNeedsAuth` (a sentinel error)
  - `event.MCPServerChanged{Base; Name string; State core.MCPState; Tools int; Err string}` (Base left empty; it isn't session-scoped)
  - `ext.ToolSource{ Tools() []Tool }`
  - `(*Registry).SetToolSource(ToolSource) error`
  - `View.ToolSource() ToolSource`

- [ ] **Step 1: Write the failing tests**
  - `TestRegistry_SetToolSource`: nil → error; a second call → error; after `Freeze` → `ErrFrozen`; `View.ToolSource()` returns the source; with no source, nil.
  - `TestRunner_ToolSourceRecomputedPerStep`: an `llmtest` script with two steps; a fake `ToolSource` returns `[]` for step 1 and `[fakeTool "mcp__s__t"]` from step 2 on. Step 2's request `Tools` contains `mcp__s__t`, and its call to it runs.
  - `TestRunner_ToolSourceDuplicateNameDropped`: the source returns a tool named `read`; the request has exactly one `read`, the built-in.
  - `TestRunner_ToolVanishedMidStep`: the source returns the tool while the request is built, then `[]`. Execution still resolves against that step's snapshot, so the call runs. A later step's call to the vanished tool gets the "unavailable" IsError.

- [ ] **Step 2: Run the tests to verify they fail**

  Run: `go test ./internal/core/ext/ ./internal/service/agent/ -run 'ToolSource|Vanished' -race`
  Expected: FAIL (undefined: `SetToolSource`).

- [ ] **Step 3: Implement**
  - Add the core types, the event, and the ext methods as listed above.
  - `Runner`: drop `allowed` from `run`. `step` calls `r.allowedTools(st.rc.Agent)` once at its start and passes the result to both `buildRequest` and `execute`.
  - `allowedTools` concatenates `View.Tools()` with `View.ToolSource().Tools()` (when non-nil), skipping source tools whose name is already taken, then applies `ToolsFor`.

- [ ] **Step 4: Run the tests to verify they pass**

  Run: `go test ./internal/core/... ./internal/service/agent/ -race`
  Expected: PASS.

- [ ] **Step 5: Commit** `feat(ext): live ToolSource, recomputed per Runner step`

---

### Task 2: Glob tool keys in permissions and agent `tools`

**Files:**
- Modify: `internal/service/permission/rules.go`, `internal/service/agents/tools.go`
- Test: `internal/service/permission/glob_test.go`, `internal/service/agents/tools_test.go`, `internal/service/permission/tighten_test.go`

**Interfaces:**
- Produces: `permission.resolveRule(rules core.PermissionRules, tool string) core.Rule` (unexported). `Effective` returns entries for every exact key, as today. `Effective(...)[tool]` for a tool with no exact key must now resolve globs, so change the `Hook.decideOne` lookup to `ruleFor(Effective(agent, cfg), tool)`, where `ruleFor` is exported as `permission.RuleFor(rules, tool) core.Rule`.

**Behavior (§5.4):**
- Within one layer, an exact key beats globs.
- Among matching glob keys (`permission.Match(key, tool)`), the highest `specificity` wins. On a tie, the more restrictive Default wins and the Patterns merge.
- `Effective` resolves each layer to one rule for a tool before overlaying defaults, then config, then agent.

- [ ] **Step 1: Write the failing tests**
  - `TestRuleFor_ExactBeatsGlob`: `{"mcp__gh__*": deny, "mcp__gh__get": allow}` → `mcp__gh__get` is allow.
  - `TestRuleFor_MostSpecificGlob`: `{"mcp__*": ask, "mcp__gh__get_*": allow}` → `mcp__gh__get_issue` is allow; `mcp__gh__push` is ask.
  - `TestRuleFor_TieFavorsRestrictive`: `{"mcp__*__get": allow, "mcp__gh__g*": deny}` (both have 10 literal characters) → `mcp__gh__get` is deny.
  - `TestEffective_GlobAcrossLayers`: a config glob `"mcp__gh__*" = allow` with an agent exact `"mcp__gh__push" = deny` → `push` is deny and `get` is allow.
  - `TestHook_GlobAllow`: the hook allows `mcp__gh__get` with no ask.
  - `TestToolsFor_GlobTools`: `tools = ["read", "mcp__gh__get_*"]` keeps `read` and `mcp__gh__get_issue` and drops `mcp__gh__push`.
  - `TestToolsFor_GlobDeny`: agent perms `{"mcp__gh__*": deny}` remove every `mcp__gh__` tool.
  - `TestTighten_DropsGlobAllow`: `Tighten(base, {"mcp__*": allow})` → the key is dropped.

- [ ] **Step 2: Run the tests to verify they fail.** Run: `go test ./internal/service/permission/ ./internal/service/agents/ -race`. Expected: FAIL.

- [ ] **Step 3: Implement** `RuleFor`, the per-layer resolution inside `Effective`, and the glob handling in `ToolsFor` (an allowed-set entry containing `*` matches via `permission.Match`). `agents` imports `service/permission` for `Match` and `RuleFor`; `permission` imports neither `agents` nor anything that does, so there is no cycle (checked with `go list -deps`).

- [ ] **Step 4: Run the tests to verify they pass.** Run: `go test ./internal/service/... -race`. Expected: PASS, including all existing trust tests.

- [ ] **Step 5: Commit** `feat(permission): glob tool-name keys in rules and agent tools`

---

### Task 3: Config — `[mcp]` TOML, `.mcp.json`, and merge order

**Files:**
- Create: `internal/data/config/mcp.go`, `internal/data/config/mcpjson.go`
- Modify: `internal/data/config/dto.go`, `load.go`, `merge.go`
- Test: `internal/data/config/mcp_test.go`, `internal/data/config/mcpjson_test.go`, `testdata/mcp/…`

**Interfaces:**
- Consumes: `core.MCPServer`, `core.MCPConfig` (Task 1).
- Produces:
  - `tomlFile.MCP mcpDTO`
  - `Loaded.ProjectMCPFiles []string` (every `.mcp.json` path considered, whether present or not, root → leaf)
  - `Loaded.GlobalMCPNames` is not needed; Restrict works from the layers.
  - `config.Merge` merges `MCP` by the §5.2 rules.
  - `Loaded.Warnings []string` (unknown `.mcp.json` fields; unset `${VAR}`)

**Behavior (§5.1, §5.2):**
- **Layer order in the project chain:** for each directory, `<dir>/.mcp.json` first, then `<dir>/.jig/config.toml`.
- **Replacement:** an entry replaces a lower one whole, except an entry whose only defined key is `enabled`, which toggles.
- `Disabled` lists are unioned.
- **Transport inference:** `command` → stdio, `url` → http. Both or neither is an error naming the file.
- **`Source`** is the defining file's path.
- **`Cwd`** defaults to the workdir for the global file and to the defining file's directory (or the `.jig/` parent) for project files. A relative cwd resolves against the same base.
- **Durations** parse with `time.ParseDuration`; a bad value is an error naming the file.
- **Expansion:** `${VAR}` / `${VAR:-default}` expansion in `.mcp.json` runs only when `o.SubstituteProject` is set, like `{env:}`.

- [ ] **Step 1: Write the failing tests**
  - `TestLoad_MCPTOMLStdioAndHTTP`: both types parse and infer their transport; the defaults stay zero (the Manager applies 10s/2m).
  - `TestLoad_MCPInvalidName` (`"GitHub"`), `TestLoad_MCPCommandAndURL`, `TestLoad_MCPNeither`, and `TestLoad_MCPBadDuration`: each errors, and the error contains the file path.
  - `TestLoad_MCPJSONClaudeFormat`: `testdata/mcp/claude.mcp.json` with `mcpServers` holding one stdio and one `type: "http"` entry parses; an unknown field `"disabled": true` gives one warning.
  - `TestLoad_MCPJSONExpansion`: with `SubstituteProject`, `${HOME_X}` becomes the getenv value, `${MISSING:-d}` becomes `d`, and `${MISSING}` becomes `""` plus a warning. Without `SubstituteProject`, the raw `${HOME_X}` is kept.
  - `TestLoad_MCPLayerOrder`: the root `.mcp.json` defines `a`, the root `.jig/config.toml` overrides `a`'s command, and the leaf `.mcp.json` sets `a` to `{"enabled": false}`. Result: the TOML command, disabled.
  - `TestLoad_MCPDefaultCwd`: a project server's cwd is the `.mcp.json` directory; a global one's is `""`, which means the workdir, applied by the Manager.
  - `TestMerge_MCPDisabledUnion`.
  - `TestLoad_ProjectMCPFiles`: lists the `.mcp.json` path of every directory in the chain.

- [ ] **Step 2: Run the tests to verify they fail.** Run: `go test ./internal/data/config/ -run MCP -race`. Expected: FAIL.

- [ ] **Step 3: Implement.**
  - `projectConfigFiles` returns an ordered list of `(path, kind)`, where kind is toml or mcpjson.
  - `loadLayer` dispatches on kind.
  - `.mcp.json` decodes with `encoding/json` into `map[string]json.RawMessage` so unknown fields can be detected.
  - The expansion regex is `\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`.

- [ ] **Step 4: Run the tests to verify they pass.** Run: `go test ./internal/data/config/ -race`. Expected: PASS, including the existing golden `example_test`.

- [ ] **Step 5: Commit** `feat(config): [mcp] servers and project .mcp.json layers`

---

### Task 4: Trust — restricting servers, effects, and the hash

**Files:**
- Create: `internal/service/trust/mcp.go`
- Modify: `internal/service/trust/trust.go` (`Restrict` calls `restrictMCP`), `effects.go` (`Effects` calls `mcpEffects`, and `tokenRE` also matches `\$\{[^}]*\}`), `internal/app/trust.go` (`projectFiles` adds `loaded.ProjectMCPFiles` to `optional`)
- Test: `internal/service/trust/mcp_test.go`, `internal/app/trust_test.go`

**Interfaces:**
- Consumes: `trust.Layers` (`Project.MCP`, `Global.MCP`).
- Produces: `restrictMCP(l Layers) (kept, dropped core.MCPConfig)` and `mcpEffects(out []Effect, l Layers) []Effect`.

**Behavior (§5.3):**
- **Kept from a project layer:** `Disabled` entries, and server entries that only set `enabled = false`.
- **Dropped:** everything else.
- **Effect `Key`:** `mcp.servers.<name>`, with `Source` the file.
- **Effect `Value`:** `stdio <command> <args…>  env: K1, K2`, or `http|sse <safeURL(url)>  headers: H (<secretValue>)`.
- **Override effect:** when the name exists in `Global.MCP`, an extra effect with Key `mcp.servers.<name>` and Value `overrides your global server "<name>"`.

- [ ] **Step 1: Write the failing tests**
  - `TestRestrict_DropsProjectServers`
  - `TestRestrict_KeepsDisables`: an `enabled=false` toggle and `[mcp] disabled` are both kept.
  - `TestRestrict_DropsEnableToggle`
  - `TestEffects_MCP`: golden strings, including the override line.
  - `TestEffects_MCPSecrets`: a literal header value → `(set)`, `${GH_TOKEN}` → the raw token, `{env:X}` → the raw token; a URL with userinfo and a query → `https://host/path?…`.
  - `TestEffects_MCPSanitizable`: a command containing `\x1b[31m` survives into `Value`; the dialog sanitizes it, and the existing `trustdialog_test` pattern asserts no ESC in the output.
  - `TestHashProject_CoversMCPJSON`: adding `.mcp.json` changes the hash; a missing one hashes the same as before the feature (absent).

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/service/trust/ ./internal/app/ -run 'MCP|Hash' -race`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Verify they pass.** Run: `go test ./internal/service/trust/ ./internal/app/ -race`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(trust): restrict and list project MCP servers; hash .mcp.json`

---

### Task 5: `client/mcp` — transports, the jig-side types, and a fake stdio server

**Files:**
- Create: `internal/client/mcp/conn.go`, `convert.go`, `stdio.go`, and `e2e/testdata/mcpfake/main.go`
- Modify: `go.mod`/`go.sum` (`go get github.com/modelcontextprotocol/go-sdk@v1.8.0`), `internal/archtest/layers_test.go`
- Test: `internal/client/mcp/conn_test.go`, `stdio_test.go`

**Interfaces:**
- Produces (package `mcp`, imported by `internal/app` as `mcpclient`):
  - `type Spec struct{ Name string; Transport core.MCPTransport; Command string; Args []string; Env []string /*KEY=VAL, full env*/; Dir, URL string; Headers map[string]string; OAuth auth.OAuthHandler /*nil ok*/; HTTPClient *http.Client }`
  - `func Dial(ctx context.Context, s Spec, onToolsChanged func()) (*Conn, error)`
  - `(*Conn) ListTools(ctx) ([]Tool, error)`: pages until done.
  - `(*Conn) CallTool(ctx, name string, args json.RawMessage) (CallResult, error)`
  - `(*Conn) Close() error`
  - `(*Conn) Done() <-chan struct{}`: closed when the session ends.
  - `(*Conn) Err() error`
  - `type Tool struct{ Name, Description string; Schema json.RawMessage; ReadOnly bool }`
  - `type Content struct{ Kind string /*text|image|audio|resource_link|resource_text|resource_blob*/; Text, MIME, URI, Name string; Data []byte }`
  - `type CallResult struct{ Content []Content; Structured json.RawMessage; IsError bool }`
  - `func IsUnauthorized(err error) bool`: true when the error chain holds `core.ErrMCPNeedsAuth`.

**Behavior:**
- **stdio:**
  - `exec.Cmd` with `Setsid`, `Cancel` = `shell.KillGroup(pid)`, and `WaitDelay` (reuse `client/shell`'s process-group helper; export `shell.ConfigureGroup(cmd)` if it is unexported).
  - stderr is kept in a 4 KB tail and included in `Err()`.
  - `Close` kills the group.
- **http:** `mcp.StreamableClientTransport{Endpoint, HTTPClient, OAuthHandler}`. Headers are added by a `RoundTripper` wrapper.
- **sse:** `mcp.SSEClientTransport{Endpoint, HTTPClient}`, with headers via the same wrapper.
- **Client capabilities:** none are advertised (no roots, sampling, or elicitation). The implementation name is `"jig"` and its version is the build version.
- **archtest:** `TestLayers_ServiceDoesNotImportConcreteClientOrData` adds `strings.HasPrefix(imp.Path, "github.com/modelcontextprotocol/go-sdk")`.

- [ ] **Step 1: Write the failing tests**
  - `TestConn_InMemoryListAndCall`: use `mcp.NewInMemoryTransports` through a test-only `dialTransport(ctx, t mcp.Transport, …)` seam in `conn.go` (unexported, shared by `Dial`). The in-process SDK server has 2 tools (one with `ReadOnlyHint`). Check that `ListTools` maps its fields, and that `CallTool` returns text, image, resource_link, and embedded text resource contents converted to `Content`, with `Structured` passed through.
  - `TestConn_ToolsChangedCallback`: the server adds a tool, and `onToolsChanged` fires.
  - `TestConn_ListToolsPages`: a server with a page size of 1 and 3 tools → 3 tools.
  - `TestStdio_FakeServer`: build `e2e/testdata/mcpfake` in `TestMain` (`go build -o`) and dial it with `MCPFAKE_SCRIPT` set to a JSON file listing its tools and replies. Checks a call and `Close`. The `/proc` check that the process group is empty runs on linux only.
  - `TestStdio_CrashClosesDone`: the script sets `crash_on_call`, so `Done()` closes and `Err()` contains the stderr tail.
  - `TestStdio_StartupTimeout`: the script sets `hang_initialize`, and `Dial` with a 200ms-deadline ctx returns `context.DeadlineExceeded` with the group killed (Review Focus 1). The deadline comes from `context.WithTimeout`, which is allowed; the ban is only on `time.Now`/`Sleep`.
  - `TestHTTP_HeadersSent`: an `httptest` server running the SDK's streamable handler checks the `X-Test` header.
  - archtest: the existing suite passes.

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/client/mcp/ -race`. Expected: FAIL.

- [ ] **Step 3: Implement.** `mcpfake` is a `main` using the go-sdk server with `StdioTransport`, configured entirely from the script file (`{"tools":[{"name","description","schema","reply":{"text"|"image_b64"|"is_error"}}],"crash_on_call":bool,"hang_initialize":bool}`).

- [ ] **Step 4: Verify they pass.** Run: `go test ./internal/client/mcp/ ./internal/archtest/ -race`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(client/mcp): stdio, streamable HTTP, and SSE connections over go-sdk`

---

### Task 6: `data/mcptokens` and `client/mcpauth`

**Files:**
- Create: `internal/data/mcptokens/store.go`, `internal/client/mcpauth/gate.go`, `listener.go`, `browser.go`, `handler.go`
- Test: `internal/data/mcptokens/store_test.go`, `internal/client/mcpauth/*_test.go`, `internal/client/mcpauth/fakeas_test.go` (an `httptest` authorization server plus MCP resource)

**Interfaces:**
- Produces:
  - `mcptokens.Record{ServerURL, Issuer, ClientID, ClientSecret, Registration string; RedirectPort int; AuthURL, TokenURL string; Scopes []string; Token *oauth2.Token}`
  - `mcptokens.New(dir string) *Store`
  - `(*Store) Load(url string) (Record, bool, error)`: false when there is no file or `ServerURL != url`.
  - `(*Store) Save(r Record) error`
  - `(*Store) Delete(url string) error`
  - `mcpauth.Gate`: an atomic open/closed switch holding the per-attempt fetch function.
  - `mcpauth.Listen(port int) (*Listener, error)`: tries `port`; on failure uses `:0`.
  - `(*Listener) Port() int`, `(*Listener) RedirectURL() string`, `(*Listener) Wait(ctx) (code, state, iss string, err error)`, `(*Listener) Close()`
  - `mcpauth.OpenBrowser(ctx, url string) error`: `xdg-open` or `open`, via the `client/shell` group helpers, no stdio.
  - `mcpauth.Config{ServerURL string; Pre *core.MCPOAuth; Store TokenSaver; Gate *Gate; HTTP *http.Client}`, where `TokenSaver` is `interface{ Load(string) (mcptokens.Record, bool, error); Save(mcptokens.Record) error }`
  - `mcpauth.NewHandler(ctx, cfg Config) (*auth.AuthorizationCodeHandler, error)`
  - `mcpauth.SecureClient() *http.Client`: refuses non-https except loopback.
  - `(*Gate) Open(fetch func(ctx, authURL string) (auth.AuthorizationResult, error))`, `(*Gate) Close()`

**Behavior (§6):**
- **The handler's `AuthorizationCodeFetcher`:** a closed gate returns `core.ErrMCPNeedsAuth`; an open gate calls its fetch.
- **`NewTokenSource`** wraps the source so that each `Token()` whose `AccessToken` or `RefreshToken` differs from the last one saved is written to the store.
- **On `invalid_grant`:** the wrapper re-reads the record once. If the stored refresh token differs, it retries via a fresh `oauth2.Config.TokenSource`; otherwise it clears `Token`, saves, and returns the error.
- **`InitialTokenSource`:** built from a loaded record that has a Token.
- **Client registration:** `PreregisteredClient` from `Pre`, or from a record with `Registration == "dynamic"` whose `RedirectPort` equals the listener's port. Reusing a stored dynamic registration as pre-registered this way keeps a restart from registering again. `DynamicClientRegistrationConfig` uses the Global Constraints metadata.
- **The listener** replies 404 to anything other than `GET /callback`. The first `/callback` gets the fixed page, and the listener shuts down after it.

- [ ] **Step 1: Write the failing tests**
  - Store: `TestStore_RoundTripPerms` (file 0600, dir 0700); `TestStore_URLMismatch` (a record for `https://a/mcp` isn't returned for `https://b/mcp`, even with a crafted hash collision, simulated by writing a file under b's hash that contains a's URL); `TestStore_Delete`.
  - Listener: `TestListener_Callback` (GET `/callback?code=c&state=s&iss=i` → values returned, the response body is exactly the fixed copy, and a second request fails to connect); `TestListener_RejectsOtherPaths` (404, `Wait` still waiting); `TestListener_BusyPortFallsBack`; `TestListener_LoopbackOnly` (`Port` is bound on `127.0.0.1`).
  - Handler, against `fakeas`:
    - `TestHandler_ClosedGateNeedsAuth`: `Dial` → `IsUnauthorized`, with no listener bound (the fetch fn is never called).
    - `TestHandler_OpenGateFullFlow`: the fetch GETs the auth URL and follows the redirect to the listener; the store then holds the token and client ID.
    - `TestHandler_RestartReusesToken`: a second handler from the store connects with no fetch and no new registration (fakeas counts registrations: 1).
    - `TestHandler_RefreshSaved`: the token expiry is set to `time.Unix(1, 0)`; the next call refreshes, and the store holds the new token.
    - `TestHandler_InvalidGrantReReads`: two cases, the store updated by "another process" and not updated (Review Focus 4). The retry happens exactly once, and the fetch is never called.
    - `TestSecureClient_RejectsHTTP`: `http://example.com` is refused; `http://127.0.0.1` is allowed.

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/data/mcptokens/ ./internal/client/mcpauth/ -race`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Verify they pass.** Same command. Expected: PASS.

- [ ] **Step 5: Commit** `feat(mcpauth): gated OAuth handler, loopback callback, token store`

---

### Task 7: `service/mcp` Manager — states, events, and the auth flow

**Files:**
- Create: `internal/service/mcp/manager.go`, `state.go`, `auth.go`
- Test: `internal/service/mcp/manager_test.go`, `auth_test.go`, `fakes_test.go`

**Interfaces:**
- Consumes: `core.MCPServer`, `core.MCPServerStatus`, `event.MCPServerChanged`, `ext.ToolSource`.
- Produces (the service declares these; `internal/app` adapts them in Task 9):
  - `mcp.Dialer` = `interface{ Dial(ctx, srv core.MCPServer, auth AuthMode, onChanged func()) (Conn, error) }`
  - `mcp.AuthMode` = `int` (`AuthClosed`, `AuthOpen`)
  - `mcp.Conn` = `interface{ ListTools(ctx) ([]RemoteTool, error); CallTool(ctx, name string, args json.RawMessage) (RemoteResult, error); Close() error; Done() <-chan struct{}; Err() error }`
  - `mcp.RemoteTool{Name, Description string; Schema json.RawMessage; ReadOnly bool}`
  - `mcp.RemoteResult{Content []RemoteContent; Structured json.RawMessage; IsError bool}`
  - `mcp.RemoteContent`, which mirrors `client/mcp.Content`
  - `mcp.Authenticator` = `interface{ Begin(ctx, srv core.MCPServer) (Session, error) }`, where `Session` = `interface{ URL() <-chan string; Close() }`. The Dialer in AuthOpen mode uses the same gate; the app adapter wires the Authenticator and Dialer together per server.
  - `mcp.Tokens` = `interface{ Has(url string) bool; Delete(url string) error }`
  - `mcp.New(Deps{Servers []core.MCPServer; WorkDir string; Dialer; Auth Authenticator; Tokens; Clock clock.Clock; Bus event.Publisher; Images Imager}) *Manager`
  - `(*Manager) Start(ctx)`, `Settle(ctx, d time.Duration)`, `Tools() []ext.Tool`, `Servers() []core.MCPServerStatus`, `Authenticate(ctx, name) error`, `CancelAuth(name) error`, `Logout(ctx, name) error`, `Reconnect(ctx, name) error`, `Close() error`
  - `mcp.ErrUnknownServer`

**Behavior (§4.1, §4.2, §6.2):**
- **Defaults:** `Start` applies 10s/2m and `Cwd` = WorkDir when empty. It skips `Enabled == false` servers and those in `Disabled`, marking them `MCPDisabled`.
- **Each server's goroutine:**
  1. Connect under `startup_timeout`, then `ListTools`, then `ready`.
  2. On `IsUnauthorized`, go to `needs_auth`; on any other error, to `failed` (with `timed out after <d>` for a deadline).
  3. Watch `Done()`, which moves the server to `failed`.
- `onChanged` re-lists the tools; a changed count publishes an event.
- **Events:** every state change publishes after the lock is released.
- **`Authenticate`:**
  1. Error if the server is unknown or not an http server.
  2. Set `authenticating`; `Begin` gives the URL.
  3. Set `AuthURL` and publish.
  4. Reconnect with `AuthOpen` (no startup timeout, bounded by the 5-minute callback timeout inside the fetcher).
  5. `ready`, or `failed: sign-in cancelled` / `failed: <err>`.

  `CancelAuth` cancels that ctx.
- **`Logout`:** `Tokens.Delete(url)`, then close and reconnect (closed gate).
- **`Tools()`:** builds from each ready server's cached wrapped-tool slice (Task 8) into a new slice.
- **`Servers()`:** fills `ToolNames` from each ready server's `RemoteTool` list (name + " — " + description, capped at 120 runes, sorted). `TestManager_StartStates` asserts it for the ready server.

- [ ] **Step 1: Write the failing tests.** They use a fake Dialer and Conn that can be scripted to succeed, fail, hang, return unauthorized, or trigger `onChanged`, plus `clock.NewFake` and a recording bus.
  - `TestManager_StartStates`: 4 servers → ready, needs_auth, failed, and disabled; events arrive in order per server.
  - `TestManager_StartupTimeout`: the Dialer blocks until its ctx is done; the fake clock advances 10s → `failed: timed out after 10s`. The Manager's timeout uses `Clock.After` in a select that cancels the dial ctx.
  - `TestManager_ListChanged`: `onChanged` gives a new tool count and an event, and `Tools()` includes the new tool.
  - `TestManager_ConnDoneFails`
  - `TestManager_ReconnectFromFailed`
  - `TestManager_ToolsReturnsCopy`: mutating the returned slice doesn't affect the next call.
  - `TestManager_Settle`: returns once none are connecting, or at the deadline.
  - `TestManager_AuthenticateFlow`: `authenticating` with an AuthURL, then `ready`; the Dialer saw `AuthOpen` exactly once.
  - `TestManager_AuthenticateCancel` → `failed: sign-in cancelled`
  - `TestManager_AuthenticateStdioRejected`
  - `TestManager_Logout`: Delete is called, then `needs_auth`.
  - `TestManager_ShadowedURLNoToken`: a server `gh` from the project with a different URL than the global `gh`; `Tokens.Has` is queried with the project URL only (Review Focus 3).
  - `TestManager_CloseClosesAll`

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/service/mcp/ -race`. Expected: FAIL.

- [ ] **Step 3: Implement.** One mutex; per-server `serverState{cfg, status, conn, tools []ext.Tool, names map[string]string, cancel}`.

- [ ] **Step 4: Verify they pass.** Same command. Expected: PASS.

- [ ] **Step 5: Commit** `feat(service/mcp): Manager with per-server states and on-demand sign-in`

---

### Task 8: `service/mcp` tool wrapper and content mapping

**Files:**
- Create: `internal/service/mcp/tool.go`, `content.go`
- Test: `internal/service/mcp/tool_test.go`, `content_test.go`

**Interfaces:**
- Consumes: `RemoteTool`, `RemoteResult`, `Conn` (Task 7).
- Produces:
  - `wrapName(server, tool string) string`
  - `newTool(m *Manager, server string, rt RemoteTool) (ext.Tool, error)`
  - `mapResult(call core.ToolCall, server, remote string, r RemoteResult, img Imager) core.ToolResult`
  - `mcp.Imager` = `interface{ Process([]byte) (core.Media, core.ImageInfo, error) }`

**Behavior (§7):**
- Name, description, schema fallback, and `Concurrent` as in §7.1.
- A tool with an invalid-JSON schema is dropped, and `Err` gets `tool <name>: invalid schema`.
- `Run` follows the §7.2 order: not-ready IsError with the hint, the `tool_timeout` deadline via `Clock`, cancel → `ctx.Err()`, a timeout → the exact copy.
- A transport error calls `m.markFailed(server, err)`.
- The content table is §7.3; `Metadata` gets `mcp.server` and `mcp.tool`.

- [ ] **Step 1: Write the failing tests**
  - `TestWrapName` (table):
    - `("gh","get_issue")` → `mcp__gh__get_issue`
    - `("gh","get.issue/v2")` → `mcp__gh__get_issue_v2`
    - a 70-character result → 64 characters, equal to `full[:55]+"_"+hex8`, and stable across calls
  - `TestTool_Description`: prefixed `[mcp:gh] `, capped at 2048 bytes.
  - `TestTool_SchemaFallback`: missing → `{"type":"object"}`; `{"type":"string"}` → fallback; invalid JSON → dropped, with the warning.
  - `TestTool_ConcurrentReadOnly`
  - `TestTool_RunNotReady`: exact hint copy for needs_auth.
  - `TestTool_RunTimeout`: fake clock, the Conn blocks → `mcp: get_issue timed out after 2m0s` (`time.Duration.String()`).
  - `TestTool_RunCancel`: returns `context.Canceled` as a Go error.
  - `TestTool_TransportErrorMarksFailed`
  - `TestMapResult`: one case per table row; image bytes that `Process` rejects give `[image omitted: <reason>]`; structured content is used only when there is no text; `isError` sets `IsError`; metadata keys are set.
  - `TestTool_VanishedBetweenListAndCall` (Review Focus 2): the server re-lists without the tool, so `Run` returns IsError `tool <remote> is no longer offered by mcp server "<name>"`.

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/service/mcp/ -run 'Tool|Wrap|Map' -race`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Verify they pass.** Run: `go test ./internal/service/mcp/ -race`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(service/mcp): wrap remote tools and map MCP content to tool results`

---

### Task 9: App wiring, headless mode, and `jig mcp` commands

**Files:**
- Create: `internal/app/mcp.go`, `internal/app/mcpcmd.go`
- Modify: `internal/app/registry.go` (the new `addToolSources` step and a `registryDeps.mcp` field), `services.go` (build the Manager, start it after Freeze, `rt.close` calls `mcp.Close` before removing the spill dir), `headless.go` (Settle and warnings), `app.go` (`case "mcp"`, usage text), `tui.go` (`Ports.MCP`)
- Test: `internal/app/mcp_test.go`, `internal/app/mcpcmd_test.go`, `e2e/mcp_test.go`

**Interfaces:**
- Consumes: Tasks 5–8.
- Produces:
  - `dialerAdapter` (implements `mcp.Dialer` over `mcpclient.Dial`, building `mcpauth.NewHandler` for http servers without an `Authorization` header, one `Gate` per server)
  - `authAdapter` (`mcp.Authenticator`: opens the server's gate with a fetch that `Listen`s, sends the URL, calls `OpenBrowser`, then `Wait`s with a 5-minute `Clock.After`)
  - `tokensAdapter`
  - `mcpPort` (implements `core.MCPService`)
  - `mcpCmd(ctx, args, std, getenv) int`

**Behavior (§4.5, §9):**
- **Headless:** `Settle` up to the maximum `StartupTimeout` among enabled servers, then one warning per server that isn't ready:
  - `mcp: <name> needs sign-in; run "jig mcp auth <name>"` for needs_auth
  - `mcp: <name> failed: <err>` otherwise

  Warnings are printed with `printLine`.
- **`jig mcp list`:** a tab-aligned `name  source  transport  state  tools`, where source is relative to the workdir when it is under it.
- **`jig mcp auth <name>`:** prints `open this URL to sign in: <url>` to stderr, prints `<name>: ready (<n> tools)` on success, and exits 1 on cancel or failure. ctrl+c cancels the ctx.
- **`jig mcp logout <name>`:** prints `<name>: signed out`.
- **Exit codes:** 2 for an unknown name, a stdio server given to auth, or a usage error.

- [ ] **Step 1: Write the failing tests**
  - `TestBuildRegistry_ToolSourceSet`
  - `TestHeadless_MCPWarnings`: a fake Manager via the dialer seam, with one needs_auth and one failed server → exact warning lines.
  - `TestMCPCmd_List`: a config with a `mcpfake` stdio server → the row shows `ready` and `2`.
  - `TestMCPCmd_UnknownName` → exit 2.
  - `TestMCPCmd_UntrustedProjectHidden`: a project `.mcp.json` with no trust grant → `list` prints no row for it.
  - `TestMCPCmd_ListSanitizes`: an error reason containing ESC → none in the output (Review Focus 5).
  - e2e `TestRun_MCPToolCall` (tag `jigtest`): `jigtestConfig` plus `[mcp.servers.fake]` pointing at the built `mcpfake`. The script calls `mcp__fake__echo`; `--yes` is set; the output contains the fake's reply text.

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/app/ -run MCP -race && go test -tags jigtest ./e2e/ -run MCP`. Expected: FAIL.

- [ ] **Step 3: Implement.** Each app function stays at most 40 lines; split helpers as needed.

- [ ] **Step 4: Verify they pass.** Same commands. Expected: PASS.

- [ ] **Step 5: Commit** `feat(app): wire the MCP Manager; headless warnings; jig mcp list|auth|logout`

---

### Task 10: TUI — the sidebar section, status-bar badge, and picker

**Files:**
- Create: `internal/ui/mcp.go`, `internal/ui/mcp_test.go`
- Modify: `internal/ui/ports.go` (`MCP core.MCPService`), `app.go` (`onEvent`: an `MCPServerChanged` event is handled first, before the root-session check, since its Base is empty. It schedules `mcpServersCmd`, coalesced by a pending flag, re-arms `waitEvent`, and returns), `sidebar.go` (`sidebarSections` appends `mcpSection`), `status.go` (badge), `internal/bubbles/statusbar/statusbar.go` + `format.go` (`State.MCPIssues int`, which renders as `mcp N!` in the Warn style when > 0), `actions/actions.go` (`MCPServers ID = "mcp.servers"`, Title `MCP servers…`, Group `MCP`, Drill), `pickerlevels.go` (levels `mcp`, `mcp.server`, `mcp.tools`), `mode_picker.go` (`chosen` on the MCP levels), `cmds.go` (`mcpServersCmd`, `mcpActionCmd`), `internal/ui/apptest_test.go` (fake MCPService)
- Test: `internal/ui/mcp_test.go`, `internal/bubbles/statusbar/statusbar_test.go`, golden files under `internal/ui/testdata/golden/`

**Interfaces:**
- Consumes: `core.MCPService`, `core.MCPServerStatus`, and `event.MCPServerChanged`.
- Produces:
  - `mcpSection(list []core.MCPServerStatus) sidebar.Section`
  - `mcpIssues(list) int`
  - `mcpServersMsg{list []core.MCPServerStatus; err error}`
  - `mcpActionCmd(ctx, p, verb, name string) tea.Cmd`, where verb is one of `signin|cancel|reconnect|signout`

**Behavior (§8):**
- **Rows:** the icon and tone come from §4.1: `●` Success for ready, `●` Warning for needs_auth, `◌` Muted for connecting and authenticating, `✕` Error for failed.
- **Text:** `<name>  <detail>`, all through `SanitizeLine`. While authenticating there is a second Muted row holding the URL.
- The section is omitted when no server is non-disabled.
- **Badge:** shown only when the sidebar is hidden (`!layout.sidebar`) and `mcpIssues > 0`.
- **The `mcp` level:** one item per server, with Detail = state text; drilling in opens `mcp.server` (Arg = name). Its items depend on state (§8.3):
  - `signin` Sign in
  - `cancel` Cancel sign-in
  - `copy` Copy sign-in URL (`tea.SetClipboard`)
  - `reconnect` Reconnect
  - `signout` Sign out
  - `tools` Tools… (drills to `mcp.tools`, which lists `<tool> — <desc>` and whose choices do nothing)

  The rows in `mcp.tools` come from `MCPServerStatus.ToolNames` (Task 1), each through `SanitizeLine`.
- Choosing an item runs `mcpActionCmd` and closes the picker. An error shows through the existing `errMsg` hint.

- [ ] **Step 1: Write the failing tests**
  - `TestMCPSection_States`: golden `mcp_sidebar_states` at 36 columns with every state and a long reason truncated.
  - `TestMCPSection_Sanitizes`: a name and reason with `\x1b]0;x\a` → no ESC in the render (Review Focus 5).
  - `TestMCPSection_HiddenWhenNone`
  - `TestStatusbar_MCPIssues`: golden `statusbar_mcp_badge`.
  - `TestApp_MCPBadgeOnlyWhenSidebarHidden`
  - `TestApp_MCPEventRereadsServers`: 3 events in a burst → 1 `Servers` call (coalesced).
  - `TestApp_MCPPickerSignIn`: open the picker, choose `mcp.servers` → `gh` → Sign in; the fake MCPService's `Authenticate("gh")` is called.
  - `TestApp_MCPPickerItemsByState`: needs_auth → [Sign in, Reconnect, Tools…]; authenticating → [Cancel sign-in, Copy sign-in URL, Tools…]; ready with HasToken → [Reconnect, Sign out, Tools…].
  - `TestApp_MCPNilPort`: `Ports.MCP == nil` → no section, the action is disabled, no panic.

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/ui/... ./internal/bubbles/statusbar/ -race`. Expected: FAIL.

- [ ] **Step 3: Implement.** Generate goldens with `JIG_UPDATE_GOLDEN=1` once the behavior is right, then inspect them.

- [ ] **Step 4: Verify they pass.** Run: `go test ./internal/ui/... ./internal/bubbles/... -race`, plus the budgets: `go test -run XXX -bench . -benchmem ./internal/ui/`. Expected: PASS, and every benchmark under budget.

- [ ] **Step 5: Commit** `feat(ui): MCP sidebar section, status-bar badge, and picker actions`

---

### Task 11: Transcript tool line, the pty e2e test, and docs

**Files:**
- Modify: `internal/ui/toolline.go` (a `default:` branch before `unknownLine`: names with the `mcp__` prefix go to `mcpLine`), `AGENTS.md`, `docs/config.example.toml`, `README.md`
- Create: `e2e/tui_mcp_test.go`
- Test: `internal/ui/toolline_test.go`, `internal/data/config/example_test.go` (the example must still load)

**Interfaces:**
- Produces: `mcpLine(b transcript.Block) (icon, name, summary string)`.

**Behavior (§7.4):**
- `name` = `<server> <tool>`, from `Result.Metadata` when present; otherwise the call name split on `__` into 3 parts.
- `summary` = the first string value in the input JSON (by key order), through `SanitizeLine`, cut via `truncateRunes` to 60 runes.

- [ ] **Step 1: Write the failing tests**
  - `TestToolLine_MCP`: with metadata → `gh get_issue` and the summary `"owner/repo#1"`; without metadata (running) → the name is split from the call name.
  - `TestToolLine_MCPSanitizes`: an argument containing ESC → the summary has none.
  - e2e `TestTUI_MCPSidebar` (tag `jigtest`, pty at 140×40): `screen.waitFor(ctx, t, from, "fake")`, then `"2 tools"`.
  - `TestExampleConfigLoads` still passes with the new `[mcp]` example block.

- [ ] **Step 2: Verify they fail.** Run: `go test ./internal/ui/ -run ToolLine_MCP -race && go test -tags jigtest ./e2e/ -run TUI_MCP`. Expected: FAIL.

- [ ] **Step 3: Implement `mcpLine` and write the docs**
  - `AGENTS.md`: the package tree rows; the ToolSource, OAuth gate, and token-keying invariants; the glob-key rule; shared-code rows for `mcp.Manager` and `mcptokens`.
  - `docs/config.example.toml`: the §5.1 block.
  - `README.md`: an "MCP servers" section.

- [ ] **Step 4: Verify.** Run: `make check`. Expected: every target passes with no lint issues.

- [ ] **Step 5: Commit** `feat(ui): MCP tool lines; docs for MCP servers`
