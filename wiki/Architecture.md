# Architecture

Four layers around a core of value types and ports, with an extension
registry that every built-in registers through:

```
UI layer        the TUI (Bubble Tea) · headless renderer · widgets in internal/bubbles
Service layer   chat · sessions · agent loop · tools · permissions · trust · skills · MCP
Client layer    LLM providers (fantasy) · model catalog (catwalk) · shell · search · MCP transports
Data layer      SQLite sessions · TOML config · agent/skill/theme discovery · blobs · prefs · tokens
```

- The UI does no I/O: it reaches services only through ports in
  `internal/core`, and learns about runs from an in-process event bus.
- Services declare small interfaces for the client and data capabilities
  they use; only `internal/app` knows the concrete types.
- These layering rules are enforced by tests in `internal/archtest`.

## Further reading

The detailed, kept-current architecture notes — layer rules, invariants,
shared helpers, performance budgets, and how to add a tool, widget, or
picker action — live in
[`AGENTS.md`](https://github.com/gammons/jig/blob/main/AGENTS.md) in the
repo. Design specs and plans are in
[`docs/superpowers/`](https://github.com/gammons/jig/tree/main/docs/superpowers).

## Building and testing

```
make build      # go build -o bin/jig ./cmd/jig
make test       # go test ./... -race, plus the jigtest-tagged e2e tests
make lint       # golangci-lint run, with and without --build-tags jigtest
make fmt-check  # gofmt -l . must be empty
make check      # all of the above
```

CI runs `make check` on Linux and macOS for every push to `main` and every
pull request.
