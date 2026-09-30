# Permissions and Trust

## Permission rules

Every tool call is checked against a permission rule that resolves to
`allow`, `ask`, or `deny`. Built-in defaults: `read`, `glob`, `grep`,
`todo`, `task`, and `skill` are `allow`; `write`, `edit`, and `bash` are
`ask`. Any other tool (including MCP tools) falls back to `ask`.

`[permissions]` in config overrides the defaults, and an agent's own
`permissions` (in `[agents.<name>.permissions]` or a markdown agent's
frontmatter) overrides both, tool by tool. A rule can be a bare action or
a table of glob pattern to action:

```toml
[permissions]
write = "ask"
edit = "ask"

[permissions.bash]
"git status*" = "allow"
"rm *"        = "deny"
"*"           = "ask"
```

The most specific matching pattern wins (ties favor `deny` over `ask` over
`allow`); an unmatched call falls back to the rule's default action (`ask`
if none is given). The pattern's subject is:

| Tool | Subject |
|---|---|
| `bash` | the command |
| `read`, `write`, `edit` | the resolved absolute path |
| `glob`, `grep` | the resolved search directory |

So, e.g., `[permissions.read]` `"*.env" = "deny"` blocks reading any
`.env` file.

Permission keys may use `*` globs to match many tools at once, e.g.
`"mcp__github__*" = "allow"` for every tool on one MCP server.

### Shell metacharacters

A `bash` command allowed by a *pattern* (not by the tool's default) is
downgraded to `ask` if it contains any of `;`, `&`, `|`, `` ` ``, `$`,
`>`, `<`, or a newline: `"git status*" = "allow"` allows
`git status --short` but asks for `git status; rm -rf x`.

### Subagents never exceed their parents

A subagent spawned via `task` is checked against its own rules *and* the
rules of every agent above it, and the most restrictive result wins
(`deny` > `ask` > `allow`). So `--agent plan` (bash = `ask`) delegating to
`general` still asks before bash, even if config sets `bash = "allow"`.

## Answering `ask`

**In the TUI**, an `ask` shows a permission card on the tool call. In
NORMAL mode:

| Key | Reply |
|---|---|
| `a` | allow this call |
| `A` | allow for the rest of the session |
| `d` | deny |
| `D` | deny with a message to the model |

`gp` jumps to the next pending request. A new card ignores keys for a
moment (its legend shows `…`) so a keystroke meant for something else
can't answer it by accident.

**Headless (`jig run`) never asks anyone.** An `ask` rule denies the call,
explaining that the run needs `--yes`, unless `jig run` was invoked with
`--yes`, in which case every `ask` is allowed for that run. `deny` always
blocks, `--yes` or not.

## Project trust

Project config — `.jig/config.toml`, project agent files, and
`.mcp.json` — comes with the repository you're working in, so jig doesn't
apply it blindly. It hashes those files; when the hash is new (a project
you haven't trusted, or a config that changed since you did), jig:

- **TUI:** shows a trust dialog listing exactly what the project config
  would change. `t` trusts it (remembered until the config changes); `n`
  or `esc` continues untrusted.
- **Headless:** runs untrusted with a warning, unless you pass
  `--trust-project`.

A project whose config changes nothing is never asked about.

An **untrusted** project config can only tighten your settings:

- permission `allow` patterns are dropped, and `ask` patterns are kept only
  where your own config doesn't `deny`;
- `[keybinds]` are ignored entirely;
- it can't start new MCP servers or turn one on (only disable one);
- it can't enable the agent-browser integration;
- `{env:}`/`{file:}` tokens are not expanded.

Trust is remembered per git root (or working directory), for up to 16
recent config versions, so switching branches back and forth doesn't
re-prompt.
