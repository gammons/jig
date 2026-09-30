# Configuration

## Config files

jig merges TOML config files from two places, later files winning key by
key:

1. **Global:** `$XDG_CONFIG_HOME/jig/config.toml` (default
   `~/.config/jig/config.toml`).
2. **Project:** `<dir>/.jig/config.toml`, once per directory from the git
   root down to the current directory (`--cwd`, or the process's working
   directory). Outside a git repository, only the current directory's
   `.jig/config.toml` is considered.

A project config is trust-gated: until you trust it, anything in it that
would loosen your settings is ignored. See
[[Permissions and Trust|Permissions-and-Trust]].

A missing file is skipped; a TOML syntax error is reported with the file
path and line.

## Full example

[`docs/config.example.toml`](https://github.com/gammons/jig/blob/main/docs/config.example.toml)
has every key, with comments.

## How files merge

- Scalars are replaced by the last file that sets them.
- `[providers.<id>]` and `[agents.<name>]` merge field by field.
- `[permissions]` (and `[agents.<name>.permissions]`) merge per tool,
  with `Default` replaced and patterns merged.
- `[skills] paths` and `instructions` are appended and de-duplicated.

`[providers.<id>.options]` is not supported yet: jig ignores it and prints
`warning: providers.<id>.options is not supported yet and is ignored`.

## Substitution tokens

Every string value in a config file may contain substitution tokens,
expanded before the TOML is parsed:

- `{env:VAR}` — the environment variable's value, or `""` if unset.
- `{file:path}` — that file's contents (one trailing newline trimmed),
  resolved relative to the config file's own directory, with a leading
  `~` expanded to `$HOME`.

In a project config, tokens are only expanded once the project is trusted.

## Providers

jig's built-in `anthropic` provider resolves its API key from
`$ANTHROPIC_API_KEY` and needs no config. Any provider in the catalog
(`jig models`) can be configured under `[providers.<id>]`:

```toml
[providers.anthropic]
type = "anthropic"
api_key = "{env:ANTHROPIC_API_KEY}"
base_url = "https://api.anthropic.com"
```

A custom provider can declare the reasoning effort levels its models
accept with `efforts = ["low", "medium", "high"]`. See
[[Models and Effort|Models-and-Effort]].

## System prompt instructions

`instructions` is a list of extra files (or globs) whose contents are
appended to the system prompt, in order.

jig also reads project context files into the system prompt:
`$XDG_CONFIG_HOME/jig/AGENTS.md`, then, for each directory from the git
root down to the working directory, that directory's `AGENTS.md` (or its
`CLAUDE.md` if it has no `AGENTS.md`).

```toml
instructions = ["~/.agents/skills/using-superpowers/SKILL.md"]
```

A bare `key = value` line must come before any `[table]` header: TOML
assigns it to whichever table was most recently opened.

## Themes and keybinds

`theme` picks the TUI's color theme, and `[keybinds]` remaps its keys.
See [[Keybindings]].

## Data locations

| What | Where |
|---|---|
| Global config | `$XDG_CONFIG_HOME/jig/config.toml` |
| Global context | `$XDG_CONFIG_HOME/jig/AGENTS.md` |
| Global agents | `$XDG_CONFIG_HOME/jig/agents/*.md` |
| Global skills | `$XDG_CONFIG_HOME/jig/skills/*/SKILL.md` |
| Custom themes | `$XDG_CONFIG_HOME/jig/themes/*.toml` |
| Sessions and messages | `$XDG_DATA_HOME/jig/jig.db` (default `~/.local/share/jig/jig.db`) |
| MCP OAuth tokens | `$XDG_DATA_HOME/jig/mcp-auth/` |
| Model catalog cache | `$XDG_CACHE_HOME/jig/catalog.json` |
