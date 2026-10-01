# jig: Import opencode Sessions

- **Status:** draft, awaiting review
- **Date:** 2026-10-01
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` (implemented).

## 1. Intent

**What the author asked for**

- Import existing opencode sessions into jig.

**Decisions made in review**

1. **Goal: a one-time migration whose sessions can be continued.** Imported sessions show in jig's session list, render in the transcript, and can be resumed with a model (`jig run --session`, or the TUI). There is no ongoing sync.
2. **Import everything in one run.** All root sessions and their subagent sessions, archived ones included.
3. **Approach: a built-in command** (`jig import opencode`), built from a tested translator in `data/` and an import service in `service/`, writing through jig's store. A throwaway script writing straight into jig's tables, and shelling out to `opencode export`, were rejected.
4. **Opencode IDs are kept.** A re-run skips sessions that already exist, so an interrupted import can simply be run again, and subagent links need no remapping.
5. **`synthetic` messages are kept**, as an extra text part on the user message before them (they are the `@file` contents the model saw).
6. **Tools both apps have are translated to jig's names and argument names**, including `todowrite` → `todo` and `shell` → `bash`. Other tools pass through unchanged.

**Non-goals**

- Writing to, or deleting from, opencode's database.
- Importing opencode's legacy `session`/`message`/`part` tables (see §2).
- Importing opencode's config, agents, permissions, auth, MCP setup, or share links.
- A TUI action for importing.
- Keeping Anthropic reasoning signatures (jig does not keep them for its own sessions either).

## 2. opencode's storage (as of v2.0.15)

One SQLite file, `$XDG_DATA_HOME/opencode/opencode.db` (default `~/.local/share/opencode/opencode.db`), in WAL mode. The author's is 4.6 GB.

Two schemas sit side by side. The legacy one (`session`, `message`, `part`: one row per part) and the current one (`session_v2`, `session_message`: one JSON blob per message with a `content[]` array). Every legacy session also exists in `session_v2` (3,847 of 3,847), and 198 newer sessions exist only there; v2.0.15 writes only the current schema. **The importer reads only `session_v2`, `session_message`, and `todo`.**

Relevant columns:

- `session_v2`: `id` (`ses_…`), `parent_id`, `directory`, `title`, `agent`, `model` (JSON `{"id","providerID","variant"}`), `time_created`, `time_updated` (unix ms).
- `session_message`: `id` (`msg_…`, time-sortable), `session_id`, `type`, `seq` (order within the session), `time_created`, `data` (JSON).
- `todo`: `session_id`, `content`, `status`, `priority`, `position`.

`session_message.type` values and their `data`:

| type | data |
|---|---|
| `user` | `{time, text, files[], agents[]}`; a file is `{name, mime, data (base64), source, mention}` |
| `assistant` | `{time{created, completed?}, agent, model, content[], finish, cost, tokens{input, output, reasoning, cache{read, write}}}` |
| `synthetic` | `{text, time}` — context opencode injected (e.g. an `@file`'s contents) |
| `compaction` | `{status, reason, summary}` |
| `system`, `idle`, `model-switched`, `location-switched` | bookkeeping; `location-switched` carries `location.directory` |

An assistant `content[]` item is `{type: "text", text}`, `{type: "reasoning", text, state{signature}}`, or `{type: "tool", id (toolu_…), name, state{status: "completed"|"error"|…, input, content[], error{message}, metadata}}`. A tool's `state.content[]` items are `{type: "text", text}` or `{type: "file", uri: "data:<mime>;base64,…"}`.

Observed across the author's whole history: ~130k tool calls, 99% `completed`, the rest `error`; `variant` values `high`, `default`, `medium`, `xhigh`, `max`, `low`.

## 3. Command

```
jig import opencode [--db PATH] [--dry-run]
```

- `--db` defaults to `<opencode data dir>/opencode.db`, where the data dir is `$XDG_DATA_HOME/opencode`, else `~/.local/share/opencode`.
- The database is opened read-only (`mode=ro`), never written; this is safe while opencode is running.
- Progress goes to stderr (one line per 100 sessions). At the end, a summary goes to stdout: sessions imported, already present (skipped), failed (with IDs), subagents imported as roots, messages skipped (bad JSON), dropped message types by count, untranslated tool names by count, unimported attachments by count.
- `--dry-run` reads and translates everything and prints the same summary, but writes nothing (no store rows, no blobs).
- Exit codes: 0 success; 1 (`exitRunFailed`) if any session failed; 2 (`exitConfig`) for a usage error, or a missing or unreadable database, or one without `session_v2`.
- `jig import` with no source, or an unknown source, prints usage and exits 2.
- Every opencode-derived string printed (titles, IDs, tool names, error text) goes through `printLine` / `ansi.SanitizeLine`.

## 4. Architecture

| Piece | Package | Responsibility |
|---|---|---|
| Reader + translator | `internal/data/opencode` | Opens the DB read-only and yields one session at a time (a `core.Session`, its `[]core.Message`, its `[]core.Todo`, plus per-session stats), streaming rows so memory stays bounded. Imports only stdlib, `internal/core`, `internal/pathid`, and the SQLite driver. |
| Import service | `internal/service/importer` | Drives the loop: skip if present, turn images into blobs, save, tally the summary. Declares small interfaces for what it uses (source, store, media). |
| Store | `internal/data/store` | New `ImportSession(ctx, sess, msgs, todos) error`, which writes everything in one transaction, and `SessionExists(ctx, id) (bool, error)`. |
| Wiring | `internal/app/import.go` | Flag parsing, `--db` default, constructing the pieces, printing the summary; an `import` case in `Run` and a line in `usage`. |

### 4.1 Reader

`opencode.Open(ctx, path, Options{Agents []string}) (*Source, error)` (`Agents`: the agent names jig knows, built-ins plus configured, used by §5.1's agent normalization) opens `file:<path>?mode=ro` and checks for `session_v2` and `session_message` (missing → an error the app maps to exit 2). `(*Source).Each(ctx, fn func(Item) error) error` walks sessions in an order where every parent precedes its children: roots by `time_created, id`, then each session's children recursively. The walk first loads only `(id, parent_id, time_created)` for every session (about 4k rows). Per session, it reads the `session_v2` row, then its `session_message` rows `ORDER BY seq` with a row cursor, translating as it goes, then its `todo` rows `ORDER BY position`. A cycle or a `parent_id` naming a missing session is broken by treating that session as a root. A non-nil error from `fn` stops the walk and is returned.

`Item` holds `Session core.Session`, `Messages []core.Message`, `Todos []core.Todo`, and `Stats` (skipped messages with their IDs and errors, dropped message types, untranslated tool names, unimported attachments). Media in `Messages` still carry decoded bytes in `Media.Data`; the importer replaces them with blob refs.

### 4.2 Store

`ImportSession` inserts the session row (`INSERT`, not upsert: an existing ID is a conflict error), then each message and its parts in order, then the todos, all in one `withTx`. It reuses `upsertMessage`/`insertPart`/`encodePart`, so the part encoding is identical to messages jig writes itself. `SessionExists` is a `SELECT 1 … WHERE id = ?`.

### 4.3 Import service

```go
// Item is one session as the source yields it (the same fields as opencode.Item).
type Item struct {
	Session  core.Session
	Messages []core.Message
	Todos    []core.Todo
	Stats    Stats
}
type Source interface { Each(ctx context.Context, fn func(Item) error) error }
type Store  interface {
	SessionExists(ctx context.Context, id core.SessionID) (bool, error)
	ImportSession(ctx context.Context, s core.Session, msgs []core.Message, todos []core.Todo) error
}
type Media interface { Process(data []byte) (core.Media, core.ImageInfo, error) } // media.Pipeline
```

`internal/app` adapts `*opencode.Source` to `Source` (copying `opencode.Item` into `importer.Item`), so the service never imports `data/opencode`. Its entry point is `importer.Run(ctx, src, store, media, Options{DryRun, Progress func(done int)}) (Summary, error)`. The returned error is only for ctx cancellation or a source error; per-session failures go in `Summary`.

For each item: if `ctx` is cancelled, stop and return the summary so far. If the session exists, count it as skipped. If its `ParentID` names a session that is neither in the store nor imported in this run (a failed parent), clear `ParentID` and count it as a subagent imported as a root. Every media item with bytes goes through `Media.Process`, whether it's a user `PartAttachment`'s `Media` or a `PartToolResult`'s `Result.Media` entry, and is replaced by the stored one (its `Data` cleared). A failure turns an attachment into a `PartText` `[image not imported: <reason>]`, or drops the result media and appends that text to the result's `Output`. Then call `ImportSession`; an error marks the session failed and the loop continues. `DryRun` skips both `Process` and `ImportSession` but tallies everything else.

## 5. Translation

### 5.1 Session

| jig field | Comes from |
|---|---|
| `ID`, `ParentID` | `id`, `parent_id` unchanged (`""` if null) |
| `Title` | `title` unchanged (sanitized when rendered, like every stored string); null or empty → `"(untitled)"` (`data/` can't call `session.PlaceholderTitle`, and `GenerateTitle` never overwrites a non-placeholder title, so this is final) |
| `Model` | `providerID + "/" + id` from `model`; `""` if absent |
| `Effort` | `variant` if `core.ParseEffort` accepts it and it is not `default`, else `""` |
| `Agent` | `agent` if it is in `Options.Agents`, else `build` for a root, `general` for a child |
| `Cwd` | `pathid.Key(d)`, where `d` is the `location.directory` of the session's last `location-switched` message, else `directory` |
| `CreatedAt`, `UpdatedAt` | `time_created`, `time_updated` |
| Todos | `content`, `status` in `position` order; `priority` dropped |

A model whose provider jig doesn't have configured is stored as-is. Resuming that session then fails with a clear `ConfigError` (exit 2 headless). In the TUI, the user picks another model.

### 5.2 Messages

| opencode type | Becomes |
|---|---|
| `user` | A `RoleUser` message: `PartText` with `text` (if non-empty), then one part per file: an image MIME → `PartAttachment` with `Media` (decoded bytes); a `text/*` MIME → `PartAttachment` with `Content`; anything else → `PartText` `[attachment not imported: <name> (<mime>)]` |
| `synthetic` | A `PartText` with its `text`, appended to the most recent user message. If none precedes it, it becomes its own user message. |
| `assistant` | A `RoleAssistant` message (§5.3) |
| `compaction` with `status: completed` | A `RoleAssistant` message with exactly one `PartCompaction` part holding `summary`, the shape `session.Compact` writes (other statuses are dropped and counted) |
| `system`, `idle`, `model-switched`, `location-switched`, unknown | Dropped and counted by type |

Every message gets `ID` = opencode's `id`, `SessionID`, and `CreatedAt` = `time_created`. User and compaction messages take `Agent`/`Model` from the session. `ListMessages` orders by `created_at, id`, matching opencode's `seq` order.

### 5.3 Assistant messages

- `Agent` = `agent` (normalized as in §5.1); `Model` = `providerID/id`.
- `Usage`: `Input` = `tokens.input`, `Output` = `tokens.output + tokens.reasoning`, `CacheRead` = `tokens.cache.read`, `CacheWrite` = `tokens.cache.write`. `CostUSD` = `cost`.
- `Status`: `finish == "error"` → `StatusFailed`; no `time.completed` → `StatusInterrupted`; else `StatusComplete`.
- `content[]` in order: `text` → `PartText`; `reasoning` → `PartReasoning` (the signature is dropped; empty text is skipped); `tool` → a `PartToolCall` (`ID` = `id`, translated `Name`/`Input`, §5.4), then, when `state.status` is `completed` or `error`, a `PartToolResult` right after it:
  - `Output` = the `text` items of `state.content` joined with `\n`; `error` → `IsError: true`, `Output` = `state.error.message` (plus any text content).
  - each `file` item with an image `data:` URI → a `core.Media` with its decoded bytes; a non-image or malformed URI → `[file not imported: <mime>]` appended to `Output`.
  - Any other status (still running or pending) → the call alone, with no result. `convertAssistantMessage` already sends an interrupted result for it.

### 5.4 Tool translation

Input JSON is decoded to `map[string]json.RawMessage`. Keys are renamed as below; other keys are kept unchanged (jig's tools ignore unknown keys). Input that is not a JSON object is kept as-is (`replayInput` sends `{}`).

| opencode name | jig name | input key renames |
|---|---|---|
| `read` | `read` | `filePath` → `path` |
| `edit` | `edit` | `filePath` → `path`, `oldString` → `old_string`, `newString` → `new_string`, `replaceAll` → `replace_all` |
| `write` | `write` | `filePath` → `path` |
| `bash`, `shell` | `bash` | `timeout` → `timeout_ms` |
| `grep`, `glob` | unchanged | none |
| `todowrite` | `todo` | none (`todos` items keep `content`/`status`; `priority` is ignored by jig) |
| `skill` | `skill` | `name` → `id` |
| `task` | `task` | `subagent_type` → `agent`, `task_id` → `session_id` |
| `subagent` | `task` | `sessionID` → `session_id` (`agent` already matches) |
| anything else | unchanged | none; counted as untranslated |

A rename never overwrites a key already present under the target name (e.g. a `read` call that already has `path`).

**Subagent results.** For a translated `task` call with a result, the child ID is `metadata.sessionId`, else the `id` attribute of a leading `<task id="…"` tag in the output. The body is the text between `<task_result>` and `</task_result>` if present, else the whole output. The result `Output` is rewritten to `task.wrapResult`'s format, `<task_result session_id="<child>">\n<body>\n</task_result>`, so `transcript.childOf` links it. If no child ID is found, the output is kept unchanged.

## 6. Error handling

| Situation | Behavior |
|---|---|
| DB missing, unreadable, or lacking `session_v2`/`session_message` | Nothing written; message on stderr; exit 2 |
| One `session_message` row with unparseable JSON | That message skipped and counted; a warning names the session and message IDs; the session still imports |
| `ImportSession` fails for a session | Its transaction rolls back; listed as failed; the run continues; exit 1 at the end; a re-run retries it |
| A child whose parent failed or is missing | Imported with `ParentID` cleared; counted |
| Bad base64 or a failed `media.Process` | `[image not imported: …]` text in its place |
| ctrl+c | Stops between sessions; every saved session is complete; a re-run continues |
| Opencode-derived text in progress, warning, or summary lines | `ansi.SanitizeLine` via `printLine`; stored content is kept unchanged, as model output is, and sanitized when rendered |

## 7. Testing

1. **Translator** (`data/opencode`, table tests on single hand-written JSON messages): each tool rename row; the no-overwrite rule; `shell`, `subagent`, and `todowrite` renames; subagent result rewriting (`metadata.sessionId`, the `<task id>` fallback, no ID); a synthetic message joining the preceding user message, and one with none; compaction completed and not completed; each status; usage and cost; variant → effort (`default`, an unknown variant); agent normalization; cwd from `location-switched`; user files (image, text, PDF); tool `file` content; empty reasoning; bad JSON.
2. **Reader** (`data/opencode`, a fixture DB built in `t.TempDir()` with the real `session_v2`/`session_message`/`todo` DDL): parents before children; an orphan and a cycle become roots; messages in `seq` order; todos in `position` order; read-only (a write through the same handle fails); a DB without `session_v2` errors.
3. **Store**: `ImportSession` round-trips through `GetSession`/`ListMessages`/`ListTodos`; an existing ID fails and leaves the store unchanged; a failure partway (e.g. a message with a missing `SessionID` inside the tx) rolls back everything; `SessionExists`.
4. **Import service** (fakes for source, store, media): skip if present; a failed session is counted and the next one still imports; a failed parent's children become roots; media go through `Process` and are saved with `Data` cleared; a `Process` error becomes the placeholder; cancel stops between items; dry run calls neither `Process` nor `ImportSession`; summary counts.
5. **e2e** (`e2e/`, built binary, fixture DB): `jig import opencode --db <fixture>` exits 0 and its summary matches; `jig sessions` lists the imported root; a second import reports all sessions skipped; `jig run --session <id> --cwd <its dir>` with a `jigtest` script exits 0 (the imported history is valid model input); a missing `--db` exits 2.
6. **Manual**: `jig import opencode --dry-run` against the author's real 4.6 GB DB, to check the summary, run time, and memory.

`internal/archtest` rules hold: `data/opencode` imports no `service/`/`ui/`; `service/importer` imports only `core` and its own interfaces; only `internal/app` sees the concrete types. Every commit passes `make check`.

## 8. Documentation

- `AGENTS.md`: add `internal/data/opencode/` and `internal/service/importer/` to the package map; add `ImportSession` (store) to the shared-code table.
- `wiki/`: a short "Importing from opencode" page: the command, what's translated, what's dropped, and re-running after a failure.
