# jig

> **A terminal AI coding harness.**
> Keyboard-driven, modal, and fast. One Go binary.
>
> Docs: [Wiki](https://github.com/gammons/jig/wiki)

jig runs AI agents with tools against your codebase, in an interactive
TUI (`jig`) or headless (`jig run`). The built-in tools are read, write,
edit, bash, glob, grep, todo, skill, and task, and any MCP server can add
more. It's written in Go, in the spirit of
[OpenCode](https://github.com/sst/opencode), and built on the Charm
stack: [fantasy](https://github.com/charmbracelet/fantasy) for LLM access,
[catwalk](https://github.com/charmbracelet/catwalk) for the model catalog,
and [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the UI.

## Why jig?

- **One self-contained binary.** Pure Go with no cgo: no Node or Bun
  runtime, no `node_modules`, and nothing else to install. It can be built
  fully static.
- **Modest memory.** An idle TUI uses about 50 MB, where opencode used about
  150 MB on the same machine. The transcript's render cache is capped (8 MB by
  default), so a long session doesn't keep growing.
- **No client/server split.** One process runs one session, and does it
  well. There is no background server, port, or daemon to manage.
- **Fast where it counts.** UI responsiveness has budgets, and benchmarks
  enforce them. On a 2,000-message session, a keystroke redraws in under
  1.5 ms and a streaming update in under 5 ms. A missed budget means the
  algorithm gets fixed; the budget is never raised.
- **Built-in agent-browser support.** If
  [agent-browser](https://github.com/vercel-labs/agent-browser) is on your
  `PATH`, jig loads its skills, lets its read-only commands run without
  asking, and shows its screenshots in the transcript.
- **Images, both ways.** Attach images with `@file` or `--attach`, or let
  the model `read` one. jig shows them inline in the terminal (kitty,
  sixel, or half-block).
- **Secure by default.** An untrusted repo's config can only tighten your
  settings, and a subagent can never exceed its parent's permissions.
  Text from models and tools is sanitized before it reaches your terminal.
- **Built for the keyboard.** jig is a vim-style modal TUI, and every action
  is in one ctrl+p picker. There are no slash commands to memorize.
- **Works with your Claude Code setup.** It reads `.claude/agents/*.md`,
  `SKILL.md` skills, `AGENTS.md`/`CLAUDE.md`, and `.mcp.json` as they are.

## Highlights

- Subagent transcripts stream live in a side column, and runs of
  read/grep/glob/bash calls fold into collapsible groups
- Permission cards answer with allow, always, deny, or deny with a
  message
- Any provider in the catwalk catalog, with model aliases, per-agent
  models, and reasoning effort
- MCP servers over stdio, HTTP, and SSE, with OAuth sign-in on request
- 59 built-in themes, plus your own custom themes

## Install

```bash
go install github.com/gammons/jig/cmd/jig@latest
```

Or build from a checkout: `make build` (produces `bin/jig`). See the
[Installation wiki page](https://github.com/gammons/jig/wiki/Installation).

## Quick start

```bash
export ANTHROPIC_API_KEY=sk-ant-...
mkdir -p ~/.config/jig
echo 'default_model = "anthropic/claude-sonnet-4-6"' > ~/.config/jig/config.toml

jig                                           # the TUI
jig run "list the files in this directory"   # headless
```

jig needs a `default_model` (or `--model`) before it can call an LLM. The
built-in `anthropic` provider reads `$ANTHROPIC_API_KEY`. See
[Configuration](https://github.com/gammons/jig/wiki/Configuration) for
other providers.

## Debugging

Set `JIG_DEBUG=1` to write a debug log to `jig-debug.log` in the working
directory. The file is truncated on each run, and it never contains keys,
prompts, or tool output. See
[Debugging](https://github.com/gammons/jig/wiki/Debugging).

## Documentation

Everything lives in the [**wiki**](https://github.com/gammons/jig/wiki):

- [Installation](https://github.com/gammons/jig/wiki/Installation): install, build from source, first run
- [Usage](https://github.com/gammons/jig/wiki/Usage): the TUI, `jig run`, and the other subcommands
- [Keybindings](https://github.com/gammons/jig/wiki/Keybindings): modes, keys, the picker, remapping, themes
- [Configuration](https://github.com/gammons/jig/wiki/Configuration): `config.toml`, merging, substitution, data paths
- [Models and Effort](https://github.com/gammons/jig/wiki/Models-and-Effort): which model runs, aliases, reasoning effort
- [Agents](https://github.com/gammons/jig/wiki/Agents): built-in agents, TOML and markdown agents
- [Skills](https://github.com/gammons/jig/wiki/Skills): `SKILL.md` discovery, superpowers, agent-browser
- [Permissions and Trust](https://github.com/gammons/jig/wiki/Permissions-and-Trust): allow/ask/deny rules, project trust
- [MCP Servers](https://github.com/gammons/jig/wiki/MCP-Servers): stdio/HTTP/SSE, `.mcp.json`, OAuth
- [Tools and Output Limits](https://github.com/gammons/jig/wiki/Tools-and-Output-Limits): built-in tools and output caps
- [Debugging](https://github.com/gammons/jig/wiki/Debugging): the debug log
- [Architecture](https://github.com/gammons/jig/wiki/Architecture): the layers; full details in [`AGENTS.md`](AGENTS.md)

The wiki's source is the [`wiki/`](wiki) directory in this repo. Edit it
there; it is published to the wiki on every push to `main`.

## Development

```
make build      # go build -o bin/jig ./cmd/jig
make test       # go test ./... -race, plus the jigtest-tagged e2e tests
make lint       # golangci-lint run, with and without --build-tags jigtest
make fmt-check  # gofmt -l . must be empty
make check      # all of the above
```

CI runs `make check` on Linux and macOS for every pull request. See
[`AGENTS.md`](AGENTS.md) for the architecture and conventions.

## Contributing

Contributions are welcome. A few ground rules:

- **AI-assisted PRs are accepted**, and in fact encouraged, but only if
  they're driven by a **frontier model** (e.g. Claude Opus, GPT-5, Gemini
  Pro) running with **high thinking effort**. Low-effort, small-model
  output that nobody reviewed tends to create more work than it saves, and
  will be closed.
- Ideally, drive the work with the
  [superpowers](https://github.com/obra/superpowers) framework (or an
  equivalent skills/TDD-disciplined workflow). Brainstorm the design
  first, write tests, then implement.
- **For large feature additions, open an issue first**, so we can agree on
  direction before you sink time into it. Bug fixes and small improvements
  can go straight to a PR.
- Whether human- or AI-written, **you are responsible for your PR.**
  Understand the diff, make sure `make check` passes, and be ready to
  explain your choices in review.

## License

[MIT](LICENSE) © Grant Ammons
