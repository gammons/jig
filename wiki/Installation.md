# Installation

## Go

```bash
go install github.com/gammons/jig/cmd/jig@latest
```

## Build from source

```bash
git clone https://github.com/gammons/jig.git
cd jig
make build    # produces bin/jig
```

## Optional dependencies

- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) — `glob` and
  `grep` use it when it is on `PATH`, and fall back to a pure-Go search
  otherwise.
- A [Nerd Font](https://www.nerdfonts.com/) — the TUI's status bar uses
  powerline arrows.

## First run

jig needs a `default_model` (or a `--model` flag on every run) before it
can call an LLM. The built-in `anthropic` provider needs no other config,
since it resolves its API key from `$ANTHROPIC_API_KEY`:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
mkdir -p ~/.config/jig
echo 'default_model = "anthropic/claude-sonnet-4-6"' > ~/.config/jig/config.toml

jig run "list the files in this directory"   # headless
jig                                           # the TUI
```

`jig models` lists every provider and model in the catalog, and whether
jig has credentials for it. See [[Configuration]] for other providers and
the full config file, and [[Models and Effort|Models-and-Effort]] for how
the model is chosen.
