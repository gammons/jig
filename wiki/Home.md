# jig Wiki

A terminal AI coding harness written in Go, in the spirit of OpenCode, built
on the Charm stack. Run agents with tools against your codebase in an
interactive TUI (`jig`) or headless (`jig run`).

Source: [github.com/gammons/jig](https://github.com/gammons/jig)

## Getting started

1. **[[Installation]]** — `go install`, build from source, first run.
2. **[[Configuration]]** — `config.toml` files, how they merge, substitution tokens, data paths.
3. **[[Models and Effort|Models-and-Effort]]** — picking models, aliases, per-agent models, reasoning effort.

## Using jig

- **[[Usage]]** — the TUI, `jig run`, and the other subcommands; flags and exit codes.
- **[[Keybindings]]** — modes, keys, the ctrl+p picker, remapping, themes.
- **[[Agents]]** — built-in agents, TOML and markdown agent definitions.
- **[[Skills]]** — `SKILL.md` discovery, using superpowers.
- **[[Permissions and Trust|Permissions-and-Trust]]** — allow/ask/deny rules, subagent limits, project trust.
- **[[MCP Servers|MCP-Servers]]** — stdio/HTTP/SSE servers, `.mcp.json`, OAuth sign-in.
- **[[Importing from opencode|Importing-from-opencode]]** — bring opencode sessions into jig.

## Reference

- **[[Tools and Output Limits|Tools-and-Output-Limits]]** — the built-in tools and how their output is capped.
- **[[Debugging]]** — `JIG_DEBUG` and the debug log.
- **[[Architecture]]** — layers, and where the detailed notes live.

## Project

- License: [MIT](https://github.com/gammons/jig/blob/main/LICENSE)
