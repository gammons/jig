# Usage

jig has an interactive TUI (`jig`) and a headless mode (`jig run`), plus a
few listing subcommands.

## `jig` (the TUI)

```
jig [--cwd DIR] [--session ID] [--trust-project]
```

- `--cwd DIR` — run as if jig were started in DIR (see `jig run` below).
- `--session ID` — resume an existing session.
- `--trust-project` — trust this project's config without asking.

Without `--trust-project`, a project config (`.jig/config.toml`, project
agent files, `.mcp.json`) that is new or changed since you last trusted it
opens a trust dialog listing what it would change: `t` trusts it
(remembered until the config changes); `n` or `esc` continues untrusted,
with its loosening settings (and its `[keybinds]`) ignored. See
[[Permissions and Trust|Permissions-and-Trust]].

The TUI needs a terminal: with stdout redirected, `jig` exits 2 and
suggests `jig run`. See [[Keybindings]] for how to drive it.

## `jig run`

```
jig run [--agent A] [--model M] [--effort E] [--yes] [--trust-project] [--attach FILE]... [--session ID] [--cwd DIR] <prompt...>
```

- `--agent A` — which agent runs the prompt (default: `build`). Built-in
  primary agents are `build` (full tool access) and `plan` (asks before
  write/edit/bash). See [[Agents]].
- `--model M` — a model ref, `provider/model` (e.g.
  `anthropic/claude-opus-4-5-20251101`), or a `[model_aliases]` name (e.g.
  `--model haiku`). It sets the *session's* model, which persists across
  turns in that session (`--session ID`) once set. It does **not**
  override an agent that has its own configured `model` — see
  [[Models and Effort|Models-and-Effort]] for the full precedence. An
  unknown alias is a configuration error (exit code 2).
- `--effort E` — the reasoning effort for this session: `none`,
  `minimal`, `low`, `medium`, `high`, `xhigh`, or `max` (any case). Like
  `--model`, it is stored on the session and persists across its turns,
  clamped to the nearest level the model supports. An unknown level is a
  configuration error (exit code 2).
- `--yes` — allow every tool call that would otherwise ask for
  permission. Headless jig has no one to ask, so without `--yes` an `ask`
  rule denies the call.
- `--trust-project` — trust this project's config (as in the TUI).
  Headless jig never shows the trust dialog: without this flag, an
  untrusted project config with effects runs restricted, with a warning.
- `--attach FILE` — attach a file (e.g. an image) to the prompt,
  resolved against the working directory. Repeatable.
- `--session ID` — continue an existing session instead of starting a new
  one (see `jig sessions` for IDs). The session must have been started in
  the same working directory; otherwise jig exits 2 with
  `session <id> belongs to <dir>; re-run with --cwd <dir>`.
- `--cwd DIR` — run as if jig were started in DIR (default: the current
  directory). Config discovery, git-root detection, and tool paths all
  follow this directory.
- `<prompt...>` — everything left over is joined with spaces and sent as
  the prompt. Flags may appear anywhere among the prompt words; `--` ends
  flag parsing, so everything after it is treated as the prompt even if it
  looks like a flag.

Exit codes: `0` success, `1` the run failed (a model or tool error), `2` a
configuration or usage error (bad flags, bad config, `--cwd` not a
directory, and the like).

## `jig models`, `jig sessions`, `jig version`

- `jig models [provider]` — lists every provider in the catalog (or just
  one), each model's context window and per-million-token pricing, for
  models that accept one, their reasoning effort levels and default (e.g.
  `effort low…max (high)`), and whether jig has credentials for it.
- `jig sessions` — lists root sessions, newest first, as
  `<id> <updated RFC3339> <title>`.
- `jig version` — prints the build version (or `dev` for an untagged
  local build).

## `jig mcp`

```
jig mcp list             # every configured server, its transport, and status
jig mcp auth <name>      # sign in to a server that needs OAuth
jig mcp logout <name>    # forget a server's stored token
```

See [[MCP Servers|MCP-Servers]].

## `jig import opencode`

```
jig import opencode [--db PATH] [--dry-run]
```

One-time migration of opencode sessions into jig's store. See
[[Importing from opencode|Importing-from-opencode]].

## The model catalog

The model catalog comes from [catwalk](https://catwalk.charm.land), cached
at `$XDG_CACHE_HOME/jig/catalog.json` and refreshed in the background (at
most once every 24h) from `$CATWALK_URL` (default
`https://catwalk.charm.land`). A refresh never blocks a command; a stale
or unreachable catalog just means jig keeps using the last cached
snapshot.
