# Agents

jig ships six built-in agents:

| Agent | Kind | What it does |
|---|---|---|
| `build` | primary | Full tool access. The default. |
| `plan` | primary | Asks before `write`, `edit`, and `bash`. |
| `explore` | subagent | Read-only investigation, spawned via `task`. |
| `general` | subagent | General-purpose, spawned via `task`. |
| `title`, `compaction` | hidden | Used internally for session titles and compaction. |

Primary agents are selectable with `jig run --agent`, or with
`tab`/`shift+tab` in the TUI. Subagents are spawned via the `task` tool by
an agent with `can_spawn`; in the TUI, `enter` on a subagent's block opens
its live transcript in the side column.

## Defining agents in TOML

Any `[agents.<name>]` table overlays a built-in of that name field by
field, or defines a brand-new agent:

```toml
[agents.explore]
description = "Read-only subagent for investigating the codebase."
mode = "subagent"
model = "haiku"
prompt = "You investigate the codebase; never write files or run commands."
max_steps = 40
can_spawn = false
hidden = false
tools = ["read", "glob", "grep", "skill"]

[agents.explore.permissions]
bash = "deny"
```

## Defining agents in markdown

Agents can also be defined as markdown files with YAML frontmatter (Claude
Code's agent format), discovered from, in ascending precedence:

- `$XDG_CONFIG_HOME/jig/agents/*.md` (global)
- `<dir>/.jig/agents/*.md`, then `<dir>/.claude/agents/*.md`, for each
  directory from the git root down to the current directory (or just the
  current directory outside a git repo)

The file stem is the agent name; frontmatter fields (`description`,
`mode`, `model`, `effort`, `max_steps`, `can_spawn`, `hidden`, `tools`,
`permissions`) map onto the same fields as `[agents.<name>]`, and the
markdown body is the agent's prompt.

## Precedence

An agent definition's precedence (lowest to highest, each overlaying the
last field by field) is:

1. built-in
2. global TOML (`config.toml`'s `[agents.<name>]`)
3. global markdown (`.../jig/agents/*.md`)
4. project TOML (`.jig/config.toml`'s `[agents.<name>]`)
5. project markdown (`.jig/agents/*.md` and `.claude/agents/*.md`)

Project agent files are trust-gated like the rest of project config; see
[[Permissions and Trust|Permissions-and-Trust]].
