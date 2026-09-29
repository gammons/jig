# jig: MCP Client

- **Status:** draft, awaiting review
- **Date:** 2026-09-29
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` (deferred item 4, "MCP client") and `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md` (sidebar, picker, status bar).
- **Changes existing behavior:** the Runner recomputes its allowed tools at every model step instead of once per turn (§4.4). Permission rule keys and agent `tools` entries may contain `*` globs over tool names (§5.4). The registry takes one new, live extension point: `ext.ToolSource` (§4.3).
- **Library:** `github.com/modelcontextprotocol/go-sdk` v1.8.0 (the official Go SDK). It provides the stdio, streamable HTTP, and SSE client transports, and `auth.AuthorizationCodeHandler` (OAuth 2.1: protected-resource and authorization-server discovery, Client ID Metadata Document / pre-registered / dynamic client registration, PKCE, scope step-up, RFC 9207 `iss` checks, and refresh-token requests).

## 1. Intent

**What the author asked for**

- MCP support in jig. The goal is parity with Claude Code and opencode: an MCP server that works there should work in jig with little or no change.

**Decisions made in review**

1. **Parity (C).** Stdio, streamable HTTP, and legacy SSE transports. v1 supports tools only.
2. **Config (B).** jig's own TOML (`[mcp.servers.<name>]`), plus project `.mcp.json` files in Claude Code's `mcpServers` format, read as extra project config layers. jig does not read other tools' global config files.
3. **OAuth in v1 (B).** It uses the go-sdk's `AuthorizationCodeHandler`. jig adds token persistence, a loopback callback listener, and opening the browser.
4. **Sign-in only when the user asks (B).** A browser opens only from a picker action or `jig mcp auth <name>`. Silent refresh of an existing grant is allowed. Connecting (at startup, on reconnect, or headless) never opens a browser. A server that needs sign-in is shown as `needs sign-in`.
5. **Live tools through a `ToolSource` (option 1).** The registry still freezes at startup. One registered `ext.ToolSource` supplies MCP tools, and its contents change as servers connect, sign in, or send `list_changed`. Each remote tool is a real, named `ext.Tool`.

**Success criteria**

- A repo's existing `.mcp.json` works unchanged once the project is trusted. Its servers' tools reach the model as `mcp__<server>__<tool>`.
- An untrusted project can never start a server, and so can never trigger a sign-in.
- `jig mcp auth github` signs in once. Later launches reuse and silently refresh the stored token.
- Signing in from the TUI picker makes that server's tools available at the model's next step, with no restart.
- A server that is slow, failing, crashing, or waiting for sign-in never blocks the TUI, never crashes a run, and is always shown in the sidebar (or as a status-bar badge when the sidebar is hidden).
- Permission rules like `"mcp__github__get_*" = "allow"` work in `[permissions]`, in agent permissions, and in agent `tools` lists.
- `make check` passes, and the performance budgets in AGENTS.md still pass.

## 2. Scope

**In scope**

- Stdio, streamable HTTP, and SSE transports. OAuth on streamable HTTP only; SSE takes static `headers` only.
- `tools/list`, `tools/call`, and `notifications/tools/list_changed`.
- `[mcp.servers.*]` TOML config, `.mcp.json` layers, `${VAR}` / `${VAR:-default}` expansion, and `[mcp] disabled`.
- Trust: restricting servers, trust-dialog effects, and covering `.mcp.json` in the trust hash.
- Glob tool-name keys in permissions and agent `tools`.
- OAuth sign-in, token storage, refresh, and logout.
- The `core.MCPService` port, a sidebar "MCP" section, a status-bar badge, and picker actions.
- `jig mcp list|auth|logout`.
- Headless `jig run` support: it waits for servers to settle and warns about any that aren't ready.

**Out of scope (later specs)**

- `resources/*`, `prompts/*`, sampling, elicitation, roots (offering the workdir as the single root is a cheap follow-up), and showing progress notifications.
- Revoking tokens on logout, and storing tokens in the OS keychain.
- Automatically restarting a crashed server.
- Reading `~/.claude.json` or opencode's config, and a `jig mcp import` command.
- Lazily loading tools, or an `mcp_search` meta-tool, for servers with very many tools.
- Using server annotations (`readOnlyHint`, etc.) for permissions.
- Rendering particular servers' tools specially in the transcript.

## 3. Packages

```
internal/client/mcp/        wraps go-sdk. Connect(ctx, spec) → Conn with ListTools,
                            CallTool, Close, and an OnToolsChanged callback. Exposes
                            jig-owned types only (Tool, CallResult, Content); go-sdk
                            types never leave this package. stdio through
                            client/shell's Setsid + process-group kill; streamable
                            HTTP (+ OAuthHandler); SSE (static headers).
internal/client/mcpauth/    loopback callback listener, browser opener (via
                            client/shell), the gated AuthorizationCodeFetcher, and
                            building a refreshing oauth2.TokenSource from a stored
                            record.
internal/data/mcptokens/    0600 JSON records under <DataDir>/mcp-auth/, one per
                            server URL, written with atomicfile.
internal/data/config/       + [mcp] / [mcp.servers.<name>] in TOML; + .mcp.json
                            parsing and ${VAR} expansion.
internal/service/mcp/       Manager: owns connections, per-server state, and the
                            current tool list. Implements ext.ToolSource and
                            core.MCPService's behavior. Wraps remote tools as
                            ext.Tool. Declares small interfaces (Dialer, Conn,
                            TokenStore, Authenticator) for the pieces above.
internal/core/              + core.MCPServer (config value), core.MCPState,
                            core.MCPServerStatus, core.MCPService port.
internal/core/event/        + MCPServerChanged.
internal/core/ext/          + ToolSource; Registry.SetToolSource; View.ToolSource.
internal/service/agent/     per-step allowed tools (§4.4).
internal/service/agents/    glob entries in agent tools (§5.4).
internal/service/permission/ glob rule keys (§5.4).
internal/service/trust/     restricting MCP servers, and their effects (§5.3).
internal/data/trustfs/      the hash covers .mcp.json (§5.3).
internal/app/               builds the Manager, Start after Freeze, Close on exit;
                            mcpPort; `jig mcp` subcommands.
internal/ui/                sidebar section, status-bar badge, picker levels.
```

**New archtest rule.** `internal/service/...` (non-test files) must not import `github.com/modelcontextprotocol/go-sdk/...`. This mirrors the existing ban on `charm.land/fantasy`. Only `internal/client/mcp` and `internal/client/mcpauth` import the SDK.

## 4. Architecture

### 4.1 Server states

`core.MCPState` is one of:

| State | Meaning | Sidebar |
|---|---|---|
| `connecting` | Start or Reconnect in progress | `◌ name  connecting…` |
| `ready` | connected; tool list held | `● name  N tools` |
| `needs_auth` | a 401 (or step-up 403, or dead refresh token) hit the closed gate | `● name  needs sign-in` (warning color) |
| `authenticating` | `Authenticate` in progress | `◌ name  signing in…` |
| `failed` | connect, initialize, or transport error; the process exited | `✕ name  failed: <reason>` |
| `disabled` | `enabled = false` or listed in `[mcp] disabled` | not shown |

`core.MCPServerStatus` = {Name, Source (config file path), Transport, State, Tools (count), Err (string), AuthURL (only while `authenticating`), HasToken}.

### 4.2 Manager (`service/mcp`)

- `New(deps)`, with `Dialer`, `TokenStore`, `Authenticator`, `Clock`, `Bus`, `Media`, and the resolved `[]core.MCPServer` as deps.
- `Start(ctx)` connects every enabled server at once, in the background, and returns immediately.
- `Settle(ctx, timeout)` blocks until every server has left `connecting`, or the timeout passes. Only headless mode and `jig mcp list` use it.
- `Tools() []ext.Tool` (ToolSource) returns a fresh copy of the current tools of every `ready` server, in a stable order (server name, then tool name). It holds a read lock and does no I/O.
- `Servers() []core.MCPServerStatus`, `Authenticate(ctx, name)`, `CancelAuth(name)`, `Logout(ctx, name)`, and `Reconnect(ctx, name)`.
- `Close()` closes every connection. Stdio process groups are killed through `shell.KillGroup` semantics.
- The Manager's mutex is the one owner of connections, states, and tool lists. Every state change publishes `event.MCPServerChanged{Name, State, Tools, Err}` after the lock is released.
- On `list_changed`, the Manager re-fetches that server's tools and replaces its list whole.
- A connection that drops (the process exits, or the HTTP session ends with a transport error) moves the server to `failed`. Reconnect is manual in v1.

### 4.3 `ext.ToolSource`

```go
// ToolSource supplies tools whose set changes at runtime (MCP). The
// registry holds at most one; it is registered before Freeze like every
// other extension, but Tools is called on every model step and may return
// a different set each time. Implementations own their own locking and
// must not do I/O in Tools.
type ToolSource interface {
	Tools() []Tool
}
```

- `Registry.SetToolSource(s)` fails when frozen, when `s` is nil, or when a source is already set.
- `View.ToolSource()` returns nil when none is set.
- AGENTS.md invariant update: "The registry is frozen after startup. Its one `ToolSource` is the only live part: its contents change, and its own mutex owns them."

### 4.4 Runner: per-step tools

`Runner.Run` currently computes `allowed` once per turn (`runner.go:93`). It changes to recompute at the start of each model step:

```
all := View.Tools() ++ View.ToolSource().Tools()   // source may be nil
allowed := ToolsFor(agent, all)
```

- Name collisions can't happen, because built-in tool names never start with `mcp__`. A duplicate name from the source is dropped anyway, with the built-in winning; this is a safety net.
- The request builder and the executor's `byName` map both use that step's `allowed`.
- If the model calls a tool that has disappeared since the step began, it gets the existing "unavailable" `IsError` result.
- **Cost:** one slice concatenation and one filter per step. The provider's prompt cache is invalidated only when the tool set actually changes.

### 4.5 App lifecycle

- `internal/app` builds the Manager with the other services and calls `r.SetToolSource(manager)` in a new `addToolSources` registry step.
- After `Freeze`, it calls `manager.Start(ctx)` with the process's base context.
- `Close` runs in the runtime's close path, before the spill dir is removed.
- **TUI:** never waits. Servers appear in the sidebar as they settle.
- **Headless `jig run`:** `Settle(ctx, max(startup_timeout))`. Then one stderr line per server that isn't `ready` (via `printLine`), then the run. It never calls `Authenticate`.

## 5. Config, trust, and permissions

### 5.1 TOML

```toml
[mcp]
disabled = ["some-project-server"]     # names; applied after every layer

[mcp.servers.playwright]               # stdio: `command` set
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
env = { DEBUG = "0" }
cwd = "."                               # optional

[mcp.servers.github]                   # http: `url` set; type = "sse" for legacy SSE
url = "https://api.githubcopilot.com/mcp/"
headers = { Authorization = "Bearer {env:GITHUB_TOKEN}" }
startup_timeout = "10s"                # default 10s
tool_timeout = "2m"                    # default 2m
enabled = true                         # default true

[mcp.servers.linear.oauth]             # optional; only for servers without registration support
client_id = "..."
client_secret = "{env:LINEAR_CLIENT_SECRET}"
scopes = ["read"]
```

- `type` is `"stdio"`, `"http"`, or `"sse"`. It's inferred as `stdio` when `command` is set, or `http` when `url` is set. Setting both `command` and `url`, or neither, is a config error.
- Server names must match `^[a-z0-9_-]+$`. Anything else is a config error naming the file.
- `core.MCPServer` = {Name, Source, Transport, Command, Args, Env, Cwd, URL, Headers, OAuth{ClientID, ClientSecret, Scopes}, StartupTimeout, ToolTimeout, Enabled}.

### 5.2 `.mcp.json` and merge order

- **Format:** `{"mcpServers": {"<name>": {"command", "args", "env"} | {"type": "http"|"sse", "url", "headers"}}}`. Unknown fields are ignored, with one warning per file.
- **Expansion:** `${VAR}` and `${VAR:-default}` expand in every string value. An unset `${VAR}` with no default becomes `""`, with a warning naming the server and the variable. Values are never printed.
- **Layer order, lowest to highest:** global TOML, then for each directory from the git root down to the workdir, that directory's `.mcp.json` followed by its `.jig/config.toml`.
- **Merging:** a server entry replaces a same-named entry from a lower layer as a whole. The one exception is an entry that sets only `enabled`, which toggles the inherited server.
- `[mcp] disabled` is the union of all layers, and applies last.
- **Default `cwd`:** the workdir for servers defined in the global config, and the directory holding the defining `.mcp.json` or `.jig/` for project servers. A relative `cwd` resolves against that same directory.
- **Stdio environment:** jig's environment, overlaid with the server's `env`.

### 5.3 Trust

- **Loading before trust:** project `.mcp.json` files are loaded with no `${VAR}` expansion until the project is trusted. This matches `{env:}` / `{file:}` in project TOML today.
- **`trust.Restrict` on an untrusted project:**
  - drops every server a project layer adds or replaces, and every `enabled = true` toggle;
  - keeps `enabled = false` toggles and `[mcp] disabled`, since both only tighten;
  - reports each dropped server as an effect.
- **Trust effects:** one line per project server, every line passed through `SanitizeLine`:
  ```
  mcp server "playwright" (.mcp.json): stdio npx -y @playwright/mcp@latest  env: DEBUG
  mcp server "github" (.jig/config.toml): http https://api.githubcopilot.com/mcp/  headers: Authorization (set)
  mcp server "github" overrides your global server "github"
  ```
  - `env` and `headers` show their names only.
  - Values follow the existing `secretValue` rules: a raw `{env:}` / `{file:}` / `${VAR}` token prints as-is, and a literal prints `(set)`.
  - URLs follow the existing `safeURL` rules.
  - `oauth.client_secret` follows `secretValue`.
- **Trust hash:** `trustfs.HashOptional` covers every `.mcp.json` in the chain, with a missing file hashing as absent. Reading follows the existing rules: regular files only, up to 1 MiB.
- **Token keying:** tokens are keyed by the exact server URL (§6.4). A project that shadows a global server with the same name but a different URL never receives the global server's token.

### 5.4 Permissions

- **Default:** MCP tools fall back to `ask`, since they have no entry in `permission.Defaults()`. No change is needed there.
- **Glob keys:** a key in `core.PermissionRules`, or an entry in an agent's `tools` list, that contains `*` matches tool names using the existing `permission.Match`.
- **Resolving a tool's rule within one layer:** the exact key if present. Otherwise the matching glob key with the highest `specificity`. On a tie, the more restrictive Default wins (deny > ask > allow), and Patterns merge.
- **Across layers:** `Effective` overlays defaults, then config, then agent as today, with each layer resolved to a single rule first.
- **Existing checks, applied per key:**
  - `ToolsFor`'s "plainly denied" removal applies to a resolved rule, whether it came from an exact or a glob key.
  - `Tighten` and `trust.agentDeniedTools` treat glob keys like any other key. An untrusted `"mcp__*" = "allow"` is dropped, like any allow.
- **No permission subject:** MCP tools don't implement `ext.Subjecter` in v1, so rules match on the tool name only. "Allow for session" grants that one tool name.
- **Annotations:** server annotations never affect permissions.

## 6. OAuth

### 6.1 Wiring

Every `http` server with no static `Authorization` header gets an `auth.AuthorizationCodeHandler` on its `StreamableClientTransport.OAuthHandler`, configured with:

- `PreregisteredClient` from `[…oauth]` when `client_id` is set;
- `DynamicClientRegistrationConfig` with metadata: `client_name = "jig"`, `redirect_uris = [http://127.0.0.1:<port>/callback]`, `grant_types = ["authorization_code", "refresh_token"]`, `token_endpoint_auth_method = "none"`;
- `RequestRefreshToken: true`;
- `InitialTokenSource`, when a stored record has a token (§6.4);
- `NewTokenSource`, wrapped so every token it yields, new or refreshed, is saved when it changes;
- `AuthorizationCodeFetcher`, the gated fetcher (§6.2);
- `Client`, an `http.Client` whose transport refuses non-`https` URLs except for loopback hosts.

A Client ID Metadata Document is not used in v1: jig has no hosted client URL.

### 6.2 The gate

- The fetcher reads a per-server gate that the Manager holds.
- **Closed** (the default): the fetcher returns `mcp.ErrNeedsAuth` right away. The Manager maps that error, arriving through `Connect`/`CallTool`, to `needs_auth`. Nothing is opened or bound.
- **Open:** only during `Manager.Authenticate(ctx, name)`. The fetcher:
  1. starts the callback listener;
  2. publishes the auth URL (state `authenticating`, `AuthURL` set);
  3. opens the browser;
  4. waits for the redirect, cancellation, or a 5-minute timeout;
  5. returns `{Code, State, Iss}`.

  The gate closes when `Authenticate` returns, whether it succeeded or failed.
- **A 403 asking for more scopes, or a failed refresh mid-session:** both hit the closed gate. The server becomes `needs_auth`, and the call that triggered it gets an `IsError` result: `mcp server "github" needs sign-in; open ctrl+p → MCP to sign in`.

### 6.3 Callback listener (`client/mcpauth`)

- Binds `127.0.0.1` only, preferring the port saved in the server's record. If that port is taken, it uses port 0, and the Manager discards the stored dynamic registration so a new client registers with the new redirect URI.
- Accepts only `GET /callback`, answering anything else with 404. It hands the first `/callback` request's `code`, `state`, and `iss` to the fetcher, then closes. The SDK validates `state`.
- The response is a fixed HTML page ("Signed in to jig. You can close this tab.") that echoes nothing from the request.
- **Opening the browser:** `xdg-open` (linux) or `open` (darwin), run through `client/shell` with no stdio. Failing to open isn't an error, because the URL is always shown too. This matters over SSH.

### 6.4 Storage (`data/mcptokens`)

- **Path:** `<DataDir>/mcp-auth/<hex sha256(server URL)>.json`. The directory is 0700, files are 0600, and writes go through `atomicfile.Write`.
- **Record fields:** `server_url`, `issuer`, `client_id`, `client_secret`, `registration` (`preregistered` | `dynamic`), `redirect_port`, `auth_url`, `token_url`, `scopes`, and a `token` object (`access_token`, `refresh_token`, `token_type`, `expiry`).
- **On load:** a record whose `server_url` doesn't exactly match the configured URL is ignored. With a token present, `InitialTokenSource` is `oauth2.Config{…}.TokenSource(ctx, token)`, wrapped for saving.
- **Refresh races with another jig process:** on `invalid_grant`, re-read the file once. If it holds a different refresh token, retry with it. Otherwise clear `token`, keep the client registration, and move the server to `needs_auth`.
- **`Logout`:** deletes the file, closes the connection, and reconnects. The server then shows `needs sign-in`.
- Secrets never go to logs, events, or `jig mcp list`. `HasToken` is the only thing reported.

## 7. Tool calls and results

### 7.1 Wrapping

- **Name:** `mcp__<server>__<tool>`. Tool name characters outside `[A-Za-z0-9_-]` become `_`. If the full name is longer than 64 characters, it becomes its first 55 characters + `_` + the first 8 hex characters of `sha256(full name)`. The Manager keeps a map from each wrapped name to the remote name for every server.
- **`Description()`:** `[mcp:<server>] ` + the server's description, cut to 2 KB.
- **`Schema()`:** the server's `inputSchema`, untouched. If it's missing or its top-level `type` isn't `"object"`, it becomes `{"type": "object"}`. A schema that isn't valid JSON drops that tool, with the reason appended to the server's `Err` as a warning; the server stays `ready`.
- **`Concurrent()`:** true only when the server sets `annotations.readOnlyHint = true`.

### 7.2 Calling

1. If the server isn't `ready`, return an `IsError` result naming its state, plus the sign-in hint for `needs_auth`.
2. Send `tools/call` with the input unchanged, under a context with a deadline of `tool_timeout` from the Manager's clock.
3. If the parent context is cancelled, return `ctx.Err()`. The SDK sends `notifications/cancelled`.
4. If the deadline passes, return `IsError`: `mcp: <tool> timed out after <d>`.
5. On a JSON-RPC or transport error, return `IsError` with the error text. A transport failure also moves the server to `failed` (§4.2).
6. If the server replies with `isError: true`, map the content (§7.3) and set `IsError`.

### 7.3 Mapping content to `core.ToolResult`

| MCP content | jig |
|---|---|
| `text` | appended to `Output`, parts joined with `\n` |
| `image` (base64) | decoded, then `media.Process` → `Media`; decode, size, or format failures become `[image omitted: <reason>]` in `Output` |
| `audio` | `[audio omitted: <mime>, <bytes>]` |
| `resource_link` | `[resource: <uri> <name>]` |
| embedded `resource`, text | `--- <uri> ---\n<text>` |
| embedded `resource`, blob | `[resource omitted: <uri>, <mime>]` |
| `structuredContent` | pretty JSON as `Output` only when there is no text content |

- `Metadata["mcp.server"]` and `Metadata["mcp.tool"]` (the remote name) are set on every result.
- The executor's 50 KB cap applies as it does for every tool.
- Output is stored raw and sanitized when rendered, which is the existing rule.

### 7.4 Transcript

- An MCP tool's line reads `<server> <tool>`, plus a one-line summary of its input: the first string argument, cut to fit. The server and tool come from `Metadata`. When a call has no result yet (still running, blocked, or denied), they come from splitting the wrapped name on `__`, and a hash-shortened name is shown shortened.
- Details show the input as indented JSON and the output. Images use the existing image rendering.

## 8. UI

### 8.1 Port

```go
type MCPService interface {
	Servers(ctx context.Context) ([]MCPServerStatus, error)
	Authenticate(ctx context.Context, name string) error
	CancelAuth(ctx context.Context, name string) error
	Logout(ctx context.Context, name string) error
	Reconnect(ctx context.Context, name string) error
}
```

- `mcpPort` in `internal/app/ports.go` implements it over the Manager.
- The App calls it only from Cmds in `cmds.go`. It re-reads `Servers` after each `MCPServerChanged` event (coalesced into one read per event burst).

### 8.2 Sidebar and status bar

- The sidebar gets an "MCP" section when at least one server is not `disabled`, with one row per server in name order (§4.1).
- Names, reasons, and counts go through `SanitizeLine`, and a reason is cut at the column edge.
- New `Styles` fields for the section (ok / warn / error / muted), built from the palette in `theme/widgets.go` and pushed in `pushTheme`.
- **Status-bar badge:** when the sidebar is hidden and at least one server is `needs_auth` or `failed`, the status bar's right side shows `mcp N!`, where N is the count of such servers.

### 8.3 Picker

- A new built-in action `mcp.servers` ("MCP servers"), in a new "MCP" group, with no default key. It opens a `drillLevel` of servers, with their status as the preview.
- **A server's actions, shown by state:**
  - Sign in (`needs_auth`, or failed with an auth error)
  - Cancel sign-in and Copy URL (while `authenticating`; Copy URL uses the existing OSC 52 clipboard path)
  - Reconnect (any state except `authenticating`)
  - Sign out (`HasToken`)
  - Tools…, a read-only list of `<tool> — <description>` rows, cut and sanitized
- These choices are handled in `pickerCtl.chosen`.
- **While a sign-in runs:** the sidebar row shows the URL under the server, so the user can copy it by hand if the browser didn't open.

## 9. CLI

- `jig mcp list`: runs `loadEnv` (trust applies), then Start, `Settle`, and `Close`. Prints `name  source  transport  state  tools` per server via `printLine`. Never signs in.
- `jig mcp auth <name>`: prints the auth URL to stderr, waits, then prints `name: ready (N tools)` on success. Exit 1 if sign-in fails or is cancelled (ctrl+c); exit 2 for an unknown or disabled server.
- `jig mcp logout <name>`: deletes the record. Exit 2 for an unknown server.
- **Trust:** all three run the normal trust flow. With no TTY, an untrusted project's servers simply don't exist.

## 10. Error handling summary

| Situation | Result |
|---|---|
| Invalid server name, both or neither of `command`/`url`, bad duration | config error, exit 2 (headless) / startup error (TUI), naming the file |
| stdio command not found, exits, or initialize fails | `failed: <reason>`; other servers unaffected |
| Startup timeout | `failed: timed out after <d>`; the connection attempt is cancelled |
| 401 with no usable token | `needs_auth` |
| Server needs OAuth but offers no registration and no `client_id` is set | `failed: auth unsupported — set [mcp.servers.<name>.oauth] client_id` |
| Callback timeout or cancel | `failed: sign-in cancelled` / `timed out`; the gate closes and the listener closes |
| Tool call on a server that isn't ready | `IsError` naming the state |
| Tool vanished mid-step | existing "unavailable" `IsError` |
| Tool timeout | `IsError`: `timed out after <d>` |

## 11. Testing

- **`client/mcp`:** tested against in-process go-sdk servers over `mcp.NewInMemoryTransports`: listing, calling, content conversion into jig types, and `list_changed` delivery. Stdio is tested against `e2e/testdata/mcpfake`, a small scripted server (tools to list, replies, crash-on-call); these tests check group kill on `Close` and detection of a process exit.
- **`client/mcpauth` + Manager:** an `httptest` server plays both the MCP resource and the authorization server (PRM, AS metadata, DCR, token, refresh, `invalid_grant`). The browser opener is injected: it GETs the auth URL and follows the redirect to the callback. Cases:
  - first sign-in; restart reusing the stored token; silent refresh and save
  - `invalid_grant`, then the re-read-and-retry path (both outcomes)
  - the closed gate gives `needs_auth` with no listener bound
  - cancel; callback timeout
  - a busy redirect port triggers re-registration
  - a URL mismatch never sends the token
  - the listener rejects non-`/callback` paths

  Expiry uses fixed far-past or far-future timestamps (no `time.Now` in tests).
- **`service/mcp`:** tested with a fake `Dialer`/`Conn` and `clock.Fake`: state changes and events, wrapping (sanitizing, 64-character shortening, schema fallback), the content mapping table with a fake blob store, timeout vs. cancel, a transport failure moving the server to `failed`, and `Tools()` returning copies.
- **Runner:** a ToolSource whose set changes between steps is picked up on the next step. A tool removed mid-step gives "unavailable".
- **Config / trust:** `.mcp.json` parsing, `${VAR}` / `${VAR:-default}`, merge order and `enabled`-only toggles, `Restrict` (dropping additions and replacements, keeping disables), golden trust-effect lines, the hash covering `.mcp.json`, and no expansion before trust.
- **Permissions:** exact beats glob, the most specific glob wins, tie on restrictiveness, the layer overlay, `ToolsFor` with glob `tools` entries and glob denies, and `Tighten` dropping `"mcp__*" = "allow"`.
- **UI:** `newTestApp` with a fake `MCPService`. Golden frames for the sidebar section (every state), the badge, and the picker levels. `MCPServerChanged` triggers a re-read.
- **e2e:** `jig run` with a `jigtest` script calling a `mcpfake` tool and printing its output. A TUI pty test waiting for `● fake  2 tools` in the sidebar.
- **archtest:** the new rule that `service` must not import the go-sdk.

## 12. Documentation

- `AGENTS.md`: the new packages in the tree, the ToolSource invariant, OAuth gate and token-keying invariants, the glob permission-key rule, and shared-code rows (`mcp.Manager`, `mcptokens`).
- `docs/config.example.toml`: an `[mcp]` section.
- README: an "MCP servers" section covering config, `.mcp.json` compatibility, trust, permissions globs, and `jig mcp auth`.
