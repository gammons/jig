# Keybindings

The TUI is modal, vim-style. The status bar's mode block shows which mode
you're in: **INSERT** (typing a prompt), **NORMAL** (navigating the
transcript), or **PICKER** (the ctrl+p picker is open). There are no slash
commands; every action is a key or a picker entry.

## Every mode

| Key | Action |
|---|---|
| `ctrl+z` | suspend jig |
| `ctrl+d` | quit when idle (in INSERT, only on an empty prompt; otherwise it deletes forward) |
| `ctrl+p` / `ctrl+t` | open the picker (INSERT and NORMAL) |
| `ctrl+b` | toggle the sidebar (INSERT and NORMAL) |

## INSERT

| Key | Action |
|---|---|
| `enter` | send (or queue, while a run is in progress) |
| `shift+enter` / `alt+enter` | newline |
| `up` / `down` | walk prompt history |
| `esc` | to NORMAL (clears a mouse selection first, if any) |
| `tab` / `shift+tab` | cycle primary agents |
| `@` | file picker (at the start of the input or after whitespace) |
| `ctrl+e` | edit the prompt in `$VISUAL` / `$EDITOR` |
| `ctrl+c` | cancel the run (and drop the queue), else clear the prompt, else quit |

## NORMAL

| Key | Action |
|---|---|
| `j` / `k` | move the selection down / up |
| `gg` / `G` | top / bottom |
| `ctrl+u` | half page up |
| `/`, `n` / `N` | search the transcript, next / previous match |
| `y` | yank (copy) the selected block |
| `o` | expand / collapse a group of read/grep/glob calls |
| `enter` | open the selected block's details (or a subagent's live transcript) in the side column |
| `tab` / `shift+tab`, `h` / `l` | with the column open: switch focus between main and the column |
| `q` / `esc` | pop the column's top entry (from main: close the column) |
| `ctrl+e` / `ctrl+y` | scroll the column's details pane |
| `gp` | jump to the next pending permission request |
| `a` `A` `d` `D` | answer a permission card (see [[Permissions and Trust|Permissions-and-Trust]]) |
| `i` | to INSERT |
| `?` | list keybindings |
| `ctrl+c` | cancel the run (never clears or quits) |

On terminals at least 120 columns wide, main and the column sit side by
side; narrower, only the focused one shows.

## Mouse

In INSERT and NORMAL, the wheel scrolls the transcript (or the column),
accelerating on fast scrolls, and press-drag-release selects text and
copies it to the clipboard (OSC 52).

## The picker

`ctrl+p` opens a fuzzy picker over every action: new/open/rename/compact
session, switch agent/model/effort, attach files, edit prompt, search,
yank, details, cancel, sidebar, theme, streamed reasoning on/off, MCP
servers, keybindings, and quit. Actions that lead to a list drill into it.
`esc` closes it.

## Remapping keys

`[keybinds]` maps `"<mode>.<key>"` to an action ID:

```toml
[keybinds]
"normal.x" = "transcript.yank"
"insert.ctrl+o" = "session.open"
```

Modes are `insert` and `normal`. Action IDs:

| Group | IDs |
|---|---|
| Session | `session.new`, `session.open`, `session.rename`, `session.compact` |
| Agent & model | `agent.switch`, `model.switch`, `effort.switch` |
| Prompt | `prompt.attach`, `prompt.editor` |
| Transcript | `transcript.search`, `transcript.yank`, `transcript.details`, `transcript.fold`, `run.cancel` |
| View | `view.sidebar`, `view.theme`, `view.reasoning` |
| MCP | `mcp.servers` |
| App | `help.keys`, `app.quit`, `picker.open` |

The structural keys can't be remapped (a binding for one is skipped with a
warning): `enter`, `esc`, `tab`, `shift+tab`, `ctrl+c`, `ctrl+d`, `ctrl+z`
everywhere; `shift+enter`, `alt+enter`, `@`, `up`, `down` in INSERT; and
the NORMAL navigation and permission keys (`j k g gg gp G ctrl+u q n N i a
A d D ctrl+e ctrl+y h l`).

An untrusted project's `[keybinds]` are ignored.

## Themes

jig ships 59 themes (ported from [slk](https://github.com/gammons/slk)),
from `Dark` (the default) and `Light` through Catppuccin, Dracula,
Gruvbox, Nord, Rosé Pine, Solarized, Tokyo Night, and more. Switch live
with ctrl+p → "Switch theme…" (the highlighted theme previews as you
move); your choice is remembered, and takes precedence over config. Or
set one in config:

```toml
theme = "tokyo night"   # case-insensitive
```

Custom themes are `*.toml` files in `$XDG_CONFIG_HOME/jig/themes/`:

```toml
name = "My Theme"       # optional; defaults to the file name

[colors]
primary = "#7aa2f7"
accent = "#bb9af7"
warning = "#e0af68"
error = "#f7768e"
background = "#1a1b26"
surface = "#24283b"
text = "#c0caf5"
text_muted = "#565f89"
border = "#3b4261"
```

Unset colors are filled in from the rest of the palette. Other keys:
`surface_dark`, `sidebar_background`, `sidebar_text`,
`sidebar_text_muted`, `selection_background`, `selection_foreground`,
`selection_bg_focused`, `selection_bg_unfocused`, `search_highlight_bg`,
`search_highlight_fg`, `compose_insert_bg`.
