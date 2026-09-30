# Skills

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

```toml
[skills]
paths = ["~/my-skills", ".jig/skills"]
```

## Using superpowers

To use [superpowers](https://github.com/obra/superpowers) skills, put its
`using-superpowers/SKILL.md` in `instructions` so its instructions are
always in the system prompt, and let the rest of its skills directory be
found normally by the discovery rules above:

```toml
instructions = ["~/.agents/skills/using-superpowers/SKILL.md"]
```

(Superpowers itself decides where it installs; adjust the path to wherever
its skills live on your machine — often `~/.agents/skills/...` or
`~/.claude/skills/...`, both of which jig already scans.)

## agent-browser

If [agent-browser](https://github.com/vercel-labs/agent-browser) is on
`PATH`, jig adds its bundled skills automatically, and a preset allows its
read-only commands (`agent-browser snapshot`, `screenshot`, `get …`, and
the like) through `bash` without asking. Control it with:

```toml
[integrations.agent_browser]
enabled = "auto"   # "auto" (default: on if found), true, or false
```

With `enabled = true` and no binary on `PATH`, jig warns at startup. An
untrusted project can't turn it on.
