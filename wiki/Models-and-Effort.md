# Models and Effort

## Which model a turn uses

Each turn resolves a model from, in precedence order (highest first):

1. The running agent's own configured `model` (`agents.<name>.model`, set
   in TOML or a markdown agent's frontmatter).
2. For a subagent spawned by the `task` tool, its parent's resolved model.
3. The session's model: whatever `--model` (or the TUI's "Switch model…")
   set on this or an earlier turn of the same session — it is written once
   and sticks.
4. `default_model`.

It is an error (exit code 2) to reach the end of that chain with nothing
set: either configure `default_model` or pass `--model`.

`small_model` is used for cheap background work — session titles and
conversation compaction — falling back to the run's already-resolved model
when unset. `default_model` and `small_model` are each a `provider/model`
ref or a `[model_aliases]` name.

## Model aliases

`[model_aliases]` gives short names to models, usable anywhere a model ref
is accepted: `--model`, `default_model`, `small_model`, and
`agents.<name>.model` (TOML or markdown frontmatter). A value containing
`/` is always a `provider/model` ref; anything else must be an alias, and
an undefined alias is a configuration error (exit code 2) naming the key
that used it:

```toml
[model_aliases]
haiku  = "anthropic/claude-haiku-4-5-20251001"
sonnet = "anthropic/claude-sonnet-4-6"
opus   = "anthropic/claude-opus-4-5-20251101"
```

## Per-agent models

Agents can be pinned to a specific (usually cheaper or more capable)
model. For example, giving the read-only `explore` subagent a cheap model
and `plan` a strong one:

```toml
[agents.explore]
model = "haiku"

[agents.plan]
model = "opus"
```

## Reasoning effort

Models that accept a reasoning effort (their catalog entry lists effort
levels, shown by `jig models`) get one on every request. The level
requested comes from, highest first:

1. The session's effort: `--effort`, or "Switch effort…" in the TUI's
   ctrl+p picker (its "Default" item clears it).
2. The running agent's own `effort` (`agents.<name>.effort`, TOML or
   markdown frontmatter).
3. `default_effort`.
4. Otherwise the model's catalog default.

A subagent spawned by `task` skips step 1: it uses its own agent's
`effort`, then `default_effort`, then its model's default. Session titles
and compaction use the small model's lowest level.

The requested level is clamped to the nearest level the model supports (a
tie goes to the lower one), so switching to a model with fewer levels
never errors, and switching back restores the session's choice. Models
without effort levels (older budget-token thinking models, and custom
providers without `efforts`) get no effort setting at all. The TUI's
status bar shows the level in use after the model
(`build · claude-opus-5-5 · high`).

Levels are `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. A
custom provider can declare the levels its models accept with
`efforts = ["low", "medium", "high"]` under `[providers.<id>]`.
