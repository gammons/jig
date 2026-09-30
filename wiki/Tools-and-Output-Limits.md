# Tools and Output Limits

## Built-in tools

| Tool | What it does | Default permission |
|---|---|---|
| `read` | read a file (text, or an image the model can see) | `allow` |
| `write` | create or overwrite a file | `ask` |
| `edit` | replace text in a file | `ask` |
| `bash` | run a shell command | `ask` |
| `glob` | find files by pattern (gitignore-aware; ripgrep when available) | `allow` |
| `grep` | search file contents (gitignore-aware; ripgrep when available) | `allow` |
| `todo` | keep the agent's task list | `allow` |
| `skill` | list skills and load one ([[Skills]]) | `allow` |
| `task` | spawn a subagent ([[Agents]]) | `allow` |

MCP servers add more tools, named `mcp__<server>__<tool>`
([[MCP Servers|MCP-Servers]]). See [[Permissions and Trust|Permissions-and-Trust]]
to change the defaults.

`bash` runs every command in a new session with no controlling terminal,
so it can never read from or take over the TUI's terminal. Cancelling a
run (or a command timing out) kills the command's whole process group,
background children included.

## Output limits

Every tool result the model sees is capped at 50 KB (cut at a UTF-8
boundary, with `[output truncated at 50 KB]` appended). Some tools bound
their own output more tightly:

- `read` stops before 50 KB and says which `offset` to continue from.
- `grep` lines are cut to 500 characters, and the search stops once 100
  matches are found.
- `bash` keeps the last 30 KB of output and saves the full output to a
  private per-run temp directory (mode 0700, removed when jig exits).
