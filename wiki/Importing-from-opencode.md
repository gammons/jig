# Importing from opencode

`jig import opencode` is a one-time migration: it reads every session out
of an [opencode](https://opencode.ai) installation's SQLite database and
writes it into jig's own store, so it shows up in `jig sessions`, renders
in the transcript, and can be continued with `jig run --session` or the
TUI. There is no ongoing sync — run it once, and again later only to pick
up sessions opencode created since.

## Usage

```
jig import opencode [--db PATH] [--dry-run]
```

- `--db PATH` — opencode's SQLite database. Defaults to
  `<opencode data dir>/opencode.db`, where the data dir is
  `$XDG_DATA_HOME/opencode`, else `~/.local/share/opencode`.
- `--dry-run` — reads and translates everything and prints the same
  summary, but writes nothing: no store rows, no blobs.

The database is opened read-only (`mode=ro`) and never written to, so
it's safe to run while opencode itself is running.

Progress goes to stderr, one line per 100 sessions. At the end, a summary
goes to stdout: sessions imported, already present (skipped on a re-run),
failed (with their IDs and errors), subagent sessions imported as roots
(their parent failed or wasn't found), messages skipped (unparseable
JSON), dropped message types by count, untranslated tool names by count,
attachments not imported, and empty messages dropped.

Exit codes: `0` success; `1` if any session failed to import; `2` for a
usage error, or a missing, unreadable, or non-opencode database.

## What's imported

- Every session, root and subagent, archived ones included. Opencode's
  session IDs are kept, so subagent links need no remapping and a re-run
  of an interrupted import simply picks up where it left off (sessions
  already present are skipped).
- User and assistant messages, including tool calls and their results.
  Tools both apps have are translated to jig's names and argument
  names (`todowrite` → `todo`, `shell` → `bash`, `filePath` → `path`,
  and so on); other tools pass through unchanged, counted as
  "untranslated tools" in the summary.
- `synthetic` messages (the `@file` contents opencode injected) are kept
  as an extra text part on the preceding user message.
- Completed compactions become a jig compaction message in the history.
  Todos import in position order.
- Attached images are decoded, scaled, and stored as jig blobs, the same
  as an image sent during a live run.

## What's dropped

- Opencode's legacy `session`/`message`/`part` tables — only the current
  `session_v2`/`session_message` schema is read.
- Bookkeeping message types (`system`, `idle`, `model-switched`;
  `location-switched` is used only to pick the session's working
  directory) and incomplete compactions — counted as "dropped message
  types".
- A message whose JSON doesn't parse is skipped and counted (named in the
  summary); the rest of its session still imports.
- An attachment or tool-result file jig can't decode becomes placeholder
  text (`[attachment not imported: …]`, `[file not imported: …]`) in its
  place, and is counted.
- Opencode's config, agents, permissions, auth, MCP setup, and share
  links are never read.

A session whose model names a provider jig doesn't have configured still
imports; resuming it fails with a clear configuration error until you
pick a model jig can use (`jig run --session <id> --model …`, or the TUI
lets you pick a different one).

## Re-running after a failure

If a session fails to import (summary line `failed: N`, with its ID and
error), everything up to that point is already committed. Just run the
command again: sessions already present are skipped, and only the failed
ones (and anything opencode added since) are attempted. The same applies
if you interrupt the import with ctrl+c — it stops cleanly between
sessions, and a re-run continues.
