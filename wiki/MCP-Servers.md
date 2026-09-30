# MCP Servers

jig can call tools from [Model Context Protocol](https://modelcontextprotocol.io)
servers, alongside its built-in tools.

```toml
[mcp]
disabled = ["some-project-server"]     # names; applied after every layer

[mcp.servers.playwright]               # stdio: `command` set
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
env = { DEBUG = "0" }
cwd = "."                              # optional

[mcp.servers.github]                   # http: `url` set
url = "https://api.githubcopilot.com/mcp/"
headers = { Authorization = "Bearer {env:GITHUB_TOKEN}" }
startup_timeout = "10s"                # default 10s
tool_timeout = "2m"                    # default 2m
enabled = true                         # default true

[mcp.servers.linear]
url = "https://mcp.linear.app/sse"
type = "sse"                           # legacy SSE transport

[mcp.servers.linear.oauth]             # optional; only for servers without registration support
client_id = "..."
client_secret = "{env:LINEAR_CLIENT_SECRET}"
scopes = ["read"]
```

A server is `stdio` (set `command`) or `http`/`sse` (set `url`); setting
both, or neither, is a config error. Server names must match
`^[a-z0-9_-]+$`.

A stdio server runs in its own process group, which jig kills whole when
it shuts the server down, so a server that spawns children (e.g.
`npx` → `node`) doesn't leave them behind.

## `.mcp.json`

jig also reads `.mcp.json` (Claude Code's format:
`{"mcpServers": {"<name>": {...}}}`), one per directory from the git root
down to the workdir, merged under the global TOML servers and under that
directory's own `.jig/config.toml`. Its string values expand `${VAR}` and
`${VAR:-default}` (an unset `${VAR}` with no default becomes `""`).

## Trust

A project's `.mcp.json` and any `[mcp]`/`[mcp.servers.*]` in its
`.jig/config.toml` are trust-gated like the rest of project config: an
untrusted project can't add a new server or turn one on, only disable one.
`.mcp.json` is included in the trust hash, so editing it re-opens the
trust dialog. See [[Permissions and Trust|Permissions-and-Trust]].

## Permissions

MCP tools are named `mcp__<server>__<tool>`. They fall back to `ask` like
any other tool with no built-in default, and `[permissions]` keys may use
`*` globs to match a whole server:

```toml
[permissions]
"mcp__github__*" = "allow"
```

## Managing servers

```
jig mcp list             # every configured server, its transport, and status
jig mcp auth <name>      # sign in to a server that needs OAuth
jig mcp logout <name>    # forget a server's stored token
```

In the TUI, ctrl+p → "MCP servers" shows the same list.

OAuth sign-in only happens when you run `jig mcp auth`: jig never opens a
browser or binds a callback port on its own. A server that needs sign-in
reports `needs_auth` from `jig mcp list` and from any tool call against
it. Tokens are stored per server URL, mode 0600, under
`$XDG_DATA_HOME/jig/mcp-auth/`, and never appear in logs or listings.
