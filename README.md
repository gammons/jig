# jig

jig is a terminal AI coding harness written in Go, in the spirit of
OpenCode, built on the Charm stack (`fantasy` for LLM access, `catwalk`
for the model catalog). It runs agents with tools — read, write, edit,
bash, glob, grep, todo, skill, task — against your codebase, in an
interactive TUI (`jig`) or headless (`jig run`).

## Status

Plan 1 (foundation + headless `jig run`) and the Plan 2 interactive TUI
(`jig` with no subcommand) are implemented. `jig models`, `jig sessions`,
and `jig version` are also available.

## Install

```
go install github.com/gammons/jig/cmd/jig@latest
```

Or build from a checkout: `make build` (produces `bin/jig`).

## Quick start

```
export ANTHROPIC_API_KEY=sk-ant-...
mkdir -p ~/.config/jig
echo 'default_model = "anthropic/claude-sonnet-4-6"' > ~/.config/jig/config.toml
jig run "list the files in this directory"
```

jig needs a `default_model` (or a `--model` flag on every run) before
it can call an LLM; jig's built-in `anthropic` provider needs no other
config, since it resolves its API key from `$ANTHROPIC_API_KEY`. See
"Config files" below for the full file, including other providers.

## `jig` (the TUI)

```
jig [--cwd DIR] [--session ID] [--trust-project]
```

- `--cwd DIR` — as for `jig run`.
- `--session ID` — resume an existing session.
- `--trust-project` — trust this project's config without asking.

Without `--trust-project`, a project config (`.jig/config.toml`, project
agent files) that is new or changed since you last trusted it opens a
trust dialog listing what it would change: `t` trusts it (remembered
until the config changes); `n` or `esc` continues untrusted, with its
loosening settings (and its `[keybinds]`) ignored. The TUI needs a
terminal: with stdout redirected, `jig` exits 2 and suggests `jig run`.

## `jig run`

```
jig run [--agent A] [--model M] [--yes] [--session ID] [--cwd DIR] <prompt...>
```

- `--agent A` — which agent runs the prompt (default: `build`; see
  "Agents" below). Built-in primary agents are `build` (full tool
  access) and `plan` (asks before write/edit/bash).
- `--model M` — a model ref, `provider/model` (e.g.
  `anthropic/claude-opus-4-5-20251101`), or a `[model_aliases]` name
  (e.g. `--model haiku`). It sets the *session's* model,
  which persists across turns in that session (`--session ID`) once
  set. It does **not** override an agent that has its own configured
  `model` — see "Models and per-agent models" for the full precedence.
  An unknown alias is a configuration error (exit code 2).
- `--yes` — allow every tool call that would otherwise ask for
  permission. Headless jig has no one to ask, so without `--yes` an
  `ask` rule denies the call (see "Permissions").
- `--session ID` — continue an existing session instead of starting a
  new one (see `jig sessions` for IDs). The session must have been
  started in the same working directory; otherwise jig exits 2 with
  `session <id> belongs to <dir>; re-run with --cwd <dir>`.
- `--cwd DIR` — run as if jig were started in DIR (default: the current
  directory). Config discovery, git-root detection, and tool paths all
  follow this directory.
- `<prompt...>` — everything left over is joined with spaces and sent
  as the prompt. Flags may appear anywhere among the prompt words; `--`
  ends flag parsing, so everything after it is treated as the prompt
  even if it looks like a flag.

Exit codes: `0` success, `1` the run failed (a model or tool error),
`2` a configuration or usage error (bad flags, bad config, `--cwd` not
a directory, and the like).

## `jig models`, `jig sessions`, `jig version`

- `jig models [provider]` — lists every provider in the catalog (or
  just one), each model's context window and per-million-token
  pricing, and whether jig has credentials for it.
- `jig sessions` — lists root sessions, newest first, as `<id>
  <updated RFC3339> <title>`.
- `jig version` — prints the build version (or `dev` for an untagged
  local build).

The model catalog itself comes from [catwalk](https://catwalk.charm.land),
cached at `$XDG_CACHE_HOME/jig/catalog.json` and refreshed in the
background (at most once every 24h) from `$CATWALK_URL` (default
`https://catwalk.charm.land`). A refresh never blocks a command; a
stale or unreachable catalog just means `jig models` and `jig run`
keep using the last cached snapshot.

## Config files

jig merges TOML config files from two places, later files winning
key by key:

1. **Global:** `$XDG_CONFIG_HOME/jig/config.toml` (default
   `~/.config/jig/config.toml`).
2. **Project:** `<dir>/.jig/config.toml`, once per directory from the
   git root down to the current directory (`--cwd`, or the process's
   working directory). Outside a git repository, only the current
   directory's `.jig/config.toml` is considered.

A missing file is skipped; a TOML syntax error is reported with the
file path and line. See `docs/config.example.toml` for every key, with
comments, and merge details for each. In short: scalars are replaced by
the last file that sets them; `[providers.<id>]` and `[agents.<name>]`
merge field by field; `[permissions]` (and `[agents.<name>.permissions]`)
merge per tool, with `Default` replaced and patterns merged; `[skills]
paths` and `instructions` are appended and de-duplicated.
`[providers.<id>.options]` is not supported yet: `jig run` ignores it
and prints `warning: providers.<id>.options is not supported yet and is
ignored`.

Every string value in a config file may contain substitution tokens,
expanded before the TOML is parsed:

- `{env:VAR}` — the environment variable's value, or `""` if unset.
- `{file:path}` — that file's contents (one trailing newline
  trimmed), resolved relative to the config file's own directory, with
  a leading `~` expanded to `$HOME`.

Other data locations: the session/message database is
`$XDG_DATA_HOME/jig/jig.db` (default `~/.local/share/jig/jig.db`).

## Models and per-agent models

Each turn resolves a model from, in precedence order (highest first):

1. The running agent's own configured `model` (`agents.<name>.model`,
   set in TOML or a markdown agent's frontmatter).
2. For a subagent spawned by the `task` tool, its parent's resolved
   model.
3. The session's model: whatever `--model` set on this or an earlier
   turn of the same session (`--session ID`) — `--model` writes it once
   and it sticks.
4. `default_model`.

It is an error (exit code 2) to reach the end of that chain with
nothing set: either configure `default_model` or pass `--model`.

`small_model` is used for cheap background work — session titles and
conversation compaction — falling back to the run's already-resolved
model when unset. `default_model` and `small_model` are each a
`provider/model` ref or a `[model_aliases]` name.

`[model_aliases]` gives short names to models, usable anywhere a model
ref is accepted: `--model`, `default_model`, `small_model`, and
`agents.<name>.model` (TOML or markdown frontmatter). A value
containing `/` is always a `provider/model` ref; anything else must be
an alias, and an undefined alias is a configuration error (exit code 2)
naming the key that used it:

```toml
[model_aliases]
haiku  = "anthropic/claude-haiku-4-5-20251001"
sonnet = "anthropic/claude-sonnet-4-6"
opus   = "anthropic/claude-opus-4-5-20251101"
```

Agents can be pinned to a specific (usually cheaper or more capable)
model. For example, giving the read-only `explore` subagent a cheap
model and `plan` a strong one:

```toml
[agents.explore]
model = "haiku"

[agents.plan]
model = "opus"
```

## Agents

jig ships six built-in agents: `build` and `plan` (primary, selectable
with `--agent`), `explore` and `general` (subagents, spawned via the
`task` tool by an agent with `can_spawn`), and `title`/`compaction`
(hidden, used internally). Any `[agents.<name>]` table overlays a
built-in of that name field by field, or defines a brand-new agent.

Agents can also be defined as markdown files with YAML frontmatter
(Claude Code's agent format), discovered from, in ascending precedence:

- `$XDG_CONFIG_HOME/jig/agents/*.md` (global)
- `<dir>/.jig/agents/*.md`, then `<dir>/.claude/agents/*.md`, for each
  directory from the git root down to the current directory (or just
  the current directory outside a git repo)

The file stem is the agent name; frontmatter fields
(`description`, `mode`, `model`, `max_steps`, `can_spawn`, `hidden`,
`tools`, `permissions`) map onto the same fields as `[agents.<name>]`,
and the markdown body is the agent's prompt.

An agent definition's precedence (lowest to highest, each overlaying
the last field by field) is: built-in < global TOML
(`config.toml`'s `[agents.<name>]`) < global markdown
(`.../jig/agents/*.md`) < project TOML (`.jig/config.toml`'s
`[agents.<name>]`) < project markdown (`.jig/agents/*.md` and
`.claude/agents/*.md`).

## Skills

Skills are `SKILL.md` files (with `name`/`description` frontmatter) in
their own directory, discovered from, in ascending precedence:

- `$XDG_CONFIG_HOME/jig/skills/*/SKILL.md`
- `~/.agents/skills/*/SKILL.md`
- `~/.claude/skills/*/SKILL.md`
- any directory listed in `[skills] paths` (config)
- `<dir>/.jig/skills`, `<dir>/.agents/skills`, `<dir>/.claude/skills`,
  for each directory from the git root down to the current directory

An agent lists available skills (name and description) via the `skill`
tool and reads one in full on demand.

To use [superpowers](https://github.com/obra/superpowers) skills, put
its `using-superpowers/SKILL.md` in `instructions` so its instructions
are always in the system prompt, and let the rest of its skills
directory be found normally by the discovery rules above:

```toml
instructions = ["~/.agents/skills/using-superpowers/SKILL.md"]
```

(Superpowers itself decides where it installs; adjust the path to
wherever its skills live on your machine — often
`~/.agents/skills/...` or `~/.claude/skills/...`, both of which jig
already scans.)

## Permissions

Every tool call is checked against a permission rule that resolves to
`allow`, `ask`, or `deny`. Built-in defaults: `read`, `glob`, `grep`,
`todo`, `task`, and `skill` are `allow`; `write`, `edit`, and `bash`
are `ask`. `[permissions]` in config overrides the defaults, and an
agent's own `permissions` (in `[agents.<name>.permissions]` or a
markdown agent's frontmatter) overrides both, tool by tool. A rule can
be a bare action or a table of glob pattern to action, e.g.:

```toml
[permissions.bash]
"git status*" = "allow"
"rm *"        = "deny"
"*"           = "ask"
```

The most specific matching pattern wins (ties favor `deny` over `ask`
over `allow`); an unmatched call falls back to the rule's default
action (`ask` if none is given). The pattern's subject is the command
for `bash`, the resolved absolute path for `read`, `write`, and `edit`,
and the resolved search directory for `glob` and `grep` — so, e.g.,
`[permissions.read]` `"*.env" = "deny"` blocks reading any `.env` file.

A `bash` command allowed by a *pattern* (not by the tool's default) is
downgraded to `ask` if it contains any of `;`, `&`, `|`, `` ` ``,
`$(`, `>`, `<`, or a newline: `"git status*" = "allow"` allows
`git status --short` but asks for `git status; rm -rf x`.

**Subagents never exceed their parents.** A subagent spawned via
`task` is checked against its own rules *and* the rules of every agent
above it, and the most restrictive result wins (`deny` > `ask` >
`allow`). So `--agent plan` (bash = `ask`) delegating to `general`
still asks before bash, even if config sets `bash = "allow"`.

**Headless mode never actually asks anyone.** An `ask` rule denies the
call, explaining that the run needs `--yes`, unless `jig run` was
invoked with `--yes`, in which case every `ask` is allowed for that
run. `deny` always blocks, `--yes` or not. This is the standing
interactive-permission-prompt behavior planned for the TUI in Plan 2.

## Output limits

Every tool result the model sees is capped at 50 KB (cut at a UTF-8
boundary, with `[output truncated at 50 KB]` appended). `read` stops
before 50 KB and says which `offset` to continue from; `grep` lines are
cut to 500 characters and the search stops once 100 matches are found;
`bash` keeps the last 30 KB of output and saves the full output to a
private per-run temp directory (mode 0700, removed when jig exits).

## Development

```
make build      # go build -o bin/jig ./cmd/jig
make test       # go test ./... -race, plus the jigtest-tagged e2e tests
make lint       # golangci-lint run, with and without --build-tags jigtest
make fmt-check  # gofmt -l . must be empty
make check      # all of the above
```

See `AGENTS.md` for the architecture and conventions.
