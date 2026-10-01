# Import opencode Sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `jig import opencode [--db PATH] [--dry-run]` copies every opencode session (with subagents, todos, images) into jig's store, keeping opencode's IDs, so the sessions can be listed, viewed, and resumed.

**Architecture:** `internal/data/opencode` reads opencode's SQLite DB read-only and translates each session into `core` values. `internal/service/importer` drives the loop (skip if present, images → blobs, save, tally) through interfaces it declares. `internal/data/store` gains a one-transaction `ImportSession`. `internal/app/import.go` wires it into the CLI.

**Tech Stack:** Go, `database/sql` + `modernc.org/sqlite` (already a dependency), jig's `core`, `pathid`, `media`, `blobfs`.

**Spec:** `docs/superpowers/specs/2026-10-01-opencode-import-design.md` — read it first; §-references below point into it.

## Global Constraints

- Every commit passes `make check` (build, `go test ./... -race`, jigtest-tagged tests, golangci-lint both ways, gofmt).
- Layer rules (`internal/archtest`): `data/opencode` imports only stdlib, `internal/core`, `internal/pathid`, `modernc.org/sqlite`; `service/importer` imports only stdlib and `internal/core`; only `internal/app` sees concrete types.
- No package-level `var`s; no `time.Now`/`time.Sleep` in `_test.go` files (use fixed `time.UnixMilli(...)` values).
- Non-test files ≤ 500 lines; every func in `internal/app` ≤ 40 lines.
- opencode's DB is opened with `file:<path>?mode=ro` and never written.
- Opencode IDs (`ses_…`, `msg_…`, `toolu_…`) are stored unchanged.
- `core.Media.Data` is never persisted: it is cleared before `ImportSession`.
- Every opencode-derived string printed by `internal/app` goes through `printLine` (which sanitizes).
- Exit codes: `exitOK` 0, `exitRunFailed` 1 (any session failed), `exitConfig` 2 (usage, missing/unreadable DB, no `session_v2`).
- Subagent result format is exactly `<task_result session_id="<child>">\n<body>\n</task_result>` (`task.wrapResult`).

## Review Focus

1. **A session with no messages** (4 exist in the author's DB): it imports as an empty session, is listed, and `jig run --session` on it works. Test in Task 6 (e2e fixture includes one).
2. **`--db` pointed at a SQLite file that isn't opencode's** (e.g. jig's own `jig.db`): exits 2 with a "not an opencode database" message and writes nothing. Test in Task 4 (`TestOpen_NotOpencode`) and Task 6 (e2e).
3. **A tool call with `status: "error"` and no `error.message`**: the result is `IsError` with output `tool error` rather than an empty string. Test in Task 3.
4. **A user message with only file attachments and empty `text`**: no empty `PartText` is emitted; a message with neither text nor files is dropped (and counted) rather than saved empty. Test in Task 3.
5. **Control characters in a failed session's error text or an untranslated tool name**: the summary prints them sanitized. Test in Task 6 (`TestPrintSummary_Sanitizes`).

---

### Task 1: Store — `ImportSession` and `SessionExists`

**Files:**
- Create: `internal/data/store/import.go`
- Test: `internal/data/store/import_test.go`

**Interfaces:**
- Produces:
  - `func (s *Store) ImportSession(ctx context.Context, sess core.Session, msgs []core.Message, todos []core.Todo) error`
  - `func (s *Store) SessionExists(ctx context.Context, id core.SessionID) (bool, error)`

- [ ] **Step 1: Write the failing tests** in `import_test.go` (use the package's existing test-store helper from `store_test.go`):
  - `TestImportSession_RoundTrip`: import a session (with `ParentID` of an already-created parent, `Effort: "high"`), two messages (a user message with a `PartText`; an assistant message with `PartReasoning`, `PartToolCall`, `PartToolResult` whose `Result.Media` has `{MIME: "image/png", Ref: "<64 hex>"}`), and two todos. Assert `GetSession` equals the input session, `ListMessages` equals the input messages (deep-equal, order kept), `ListTodos` equals the todos.
  - `TestImportSession_ExistingIDFails`: create a session via `CreateSession`, then `ImportSession` with the same ID → non-nil error; `ListMessages` for that ID is still empty.
  - `TestImportSession_RollsBackOnFailure`: import with a second message whose `SessionID` is a non-existent session (FK violation) → error; afterwards `SessionExists` is false and `ListTodos` is empty.
  - `TestSessionExists`: false before, true after `CreateSession`.

- [ ] **Step 2: Run** `go test ./internal/data/store -run 'TestImportSession|TestSessionExists'` — expect FAIL (undefined methods).

- [ ] **Step 3: Implement** in `import.go`: one `withTx`; a plain `INSERT INTO sessions (...)` with the same columns `CreateSession` writes (including `effort`); for each message `upsertMessage` then `insertPart` per part; then the todos `INSERT` as in `ReplaceTodos`. `SessionExists` is `SELECT 1 FROM sessions WHERE id = ?` mapping `sql.ErrNoRows` to `false, nil`.

- [ ] **Step 4: Run** the same command — expect PASS.

- [ ] **Step 5: Commit** `feat(store): ImportSession and SessionExists`

---

### Task 2: opencode tool translation

**Files:**
- Create: `internal/data/opencode/tools.go` (package doc comment lives here)
- Test: `internal/data/opencode/tools_test.go`

**Interfaces:**
- Produces:
  - `func translateCall(name string, input json.RawMessage) (jigName string, jigInput json.RawMessage, translated bool)` — `translated` is false for tools outside spec §5.4's table.
  - `func translateTaskResult(output string, metadataSessionID string) string`

- [ ] **Step 1: Write the failing tests** (table-driven, compare inputs with `json` unmarshalled into `map[string]any`):
  - `TestTranslateCall`: one row per spec §5.4 table row, e.g.
    - `edit` `{"filePath":"/a","oldString":"x","newString":"y","replaceAll":true}` → `edit` `{"path":"/a","old_string":"x","new_string":"y","replace_all":true}`, translated.
    - `shell` `{"command":"ls","timeout":5000,"workdir":"/w"}` → `bash` `{"command":"ls","timeout_ms":5000,"workdir":"/w"}`.
    - `todowrite` → `todo`, input unchanged. `skill` `{"name":"s"}` → `{"id":"s"}`.
    - `task` `{"subagent_type":"general","description":"d","prompt":"p","task_id":"ses_c"}` → `task` `{"agent":"general","description":"d","prompt":"p","session_id":"ses_c"}`.
    - `subagent` `{"agent":"explore","description":"d","prompt":"p","sessionID":"ses_c"}` → `task` with `session_id`.
    - `grep` unchanged, translated=true; `webfetch` unchanged, translated=false.
  - `TestTranslateCall_NoOverwrite`: `read` `{"path":"/keep","filePath":"/other"}` → `{"path":"/keep","filePath":"/other"}`.
  - `TestTranslateCall_NonObjectInput`: input `"oops"` → returned unchanged (byte-equal).
  - `TestTranslateTaskResult`:
    - metadata ID `ses_c`, output `<task id="ses_x" state="completed">\n<task_result>\nbody\n</task_result>\n</task>` → `<task_result session_id="ses_c">\nbody\n</task_result>`.
    - no metadata ID, same output → uses `ses_x`.
    - no ID anywhere, output `plain` → `plain` unchanged.
    - ID but no `<task_result>` tags, output `plain` → `<task_result session_id="ses_c">\nplain\n</task_result>`.

- [ ] **Step 2: Run** `go test ./internal/data/opencode -run 'TestTranslate(Call|TaskResult)'` — expect FAIL.

- [ ] **Step 3: Implement** in `tools.go`. Keep the rename table as a function returning a fresh map (no package vars). Re-marshal the input map (`encoding/json` sorts keys; that's fine). Trim the body between the tags with `strings.TrimSpace`.

- [ ] **Step 4: Run** the same command — expect PASS.

- [ ] **Step 5: Commit** `feat(opencode): translate tool names and inputs`

---

### Task 3: opencode session and message translation

**Files:**
- Create: `internal/data/opencode/translate.go`
- Test: `internal/data/opencode/translate_test.go`

**Interfaces:**
- Consumes: `translateCall`, `translateTaskResult` (Task 2).
- Produces (all exported for Task 4/5/6):
  - `type Stats struct { SkippedMessages []string /* "<msg id>: <err>" */; DroppedTypes map[string]int; UntranslatedTools map[string]int; UnimportedAttachments int; EmptyMessages int }`
  - `type sessionRow struct { ID, ParentID, Directory, Title, Agent, ModelJSON string; Created, Updated int64 }`
  - `type messageRow struct { ID, Type, Data string; Created int64 }`
  - `type translator struct { agents map[string]bool; sess core.Session; msgs []core.Message; stats Stats; lastDir string }`
  - `func newTranslator(row sessionRow, agents map[string]bool) *translator`
  - `func (t *translator) add(m messageRow)`
  - `func (t *translator) finish() (core.Session, []core.Message, Stats)`

- [ ] **Step 1: Write the failing tests**, each building a `translator` from a `sessionRow` and feeding hand-written `messageRow`s (JSON shapes from spec §2):
  - `TestSession_Fields`: model JSON `{"id":"claude-opus-5-5","providerID":"anthropic","variant":"high"}` → `Model "anthropic/claude-opus-5-5"`, `Effort "high"`; variant `default` → `""`; variant `weird` → `""`; `CreatedAt`/`UpdatedAt` = `time.UnixMilli(row.Created/Updated)`; empty title → `"(untitled)"`.
  - `TestSession_Agent`: agent `build` with agents `{build}` → `build`; agent `sisyphus`, root → `build`; same, with `ParentID` set → `general`.
  - `TestSession_CwdFromLocationSwitch`: directory `/a`; a `location-switched` message with `location.directory` `/b` → `Cwd == pathid.Key("/b")`; `DroppedTypes["location-switched"] == 1`.
  - `TestUser_TextAndFiles`: text `hi` + files: an image/png (base64 of a few bytes) → `PartAttachment` with `Media.Data` decoded and `MIME "image/png"`; `text/plain` → `PartAttachment{Path: name, Content: decoded}`; `application/pdf` → `PartText "[attachment not imported: x.pdf (application/pdf)]"` and `UnimportedAttachments == 1`. Status `complete`, role user, agent/model from session.
  - `TestUser_FilesOnly` (Review Focus 4): empty text + one image → exactly one part, the attachment; empty text + no files → no message, `EmptyMessages == 1`.
  - `TestSynthetic`: user `a`, synthetic `ctx` → one user message with parts `[text a, text ctx]`; synthetic first with no user before it → its own user message.
  - `TestCompaction`: `{"status":"completed","summary":"S"}` → assistant message with exactly `[]core.Part{{Kind: core.PartCompaction, Text: "S"}}`; `{"status":"pending"}` → dropped, `DroppedTypes["compaction"] == 1`.
  - `TestAssistant_UsageStatus`: tokens `{input:2, output:760, reasoning:84, cache:{read:98437, write:840}}`, cost `0.04` → `Usage{2, 844, 98437, 840}`, `CostUSD 0.04`; `finish:"error"` → `StatusFailed`; no `time.completed` → `StatusInterrupted`; otherwise `StatusComplete`.
  - `TestAssistant_Parts`: content `[reasoning "r", reasoning "", text "t", tool edit completed]` → parts `[reasoning r, text t, tool_call(edit, translated input), tool_result]`. The result has `CallID` = tool id, `Name "edit"`, `Output` = text items joined with `\n`.
  - `TestAssistant_ToolError` (Review Focus 3): `status:"error"`, `error.message:"boom"` → `IsError`, `Output "boom"`; `status:"error"` and no error → `IsError`, `Output "tool error"`.
  - `TestAssistant_ToolRunning`: `status:"running"` → call part only, no result.
  - `TestAssistant_ToolFileContent`: content item `{type:"file", uri:"data:image/png;base64,<b64>"}` → `Result.Media[0]` with decoded `Data`; `data:application/pdf;base64,…` → output gains `\n[file not imported: application/pdf]`.
  - `TestAssistant_TaskResultRewritten`: a `task` tool with `metadata.sessionId "ses_c"` → output starts with `<task_result session_id="ses_c">`.
  - `TestAssistant_UntranslatedCounted`: tool `webfetch` → `UntranslatedTools["webfetch"] == 1`.
  - `TestBadJSON`: data `{` → message skipped; `SkippedMessages` has one entry starting with the message ID.
  - `TestDroppedTypes`: `system`, `idle`, `model-switched`, `bogus` → no messages; each counted once.

- [ ] **Step 2: Run** `go test ./internal/data/opencode` — expect FAIL.

- [ ] **Step 3: Implement** `translate.go` per spec §5.1–§5.3. Decode `data` into small private structs (`userData`, `assistantData`, `toolState`, …) instead of `map[string]any`. Message `ID`/`CreatedAt` come from the row. Decode `data:` URIs with `strings.Cut` on `,` and `base64.StdEncoding`; a decode failure counts as not imported. Keep the file ≤ 500 lines; split `assistant.go` out if needed.

- [ ] **Step 4: Run** `go test ./internal/data/opencode` — expect PASS.

- [ ] **Step 5: Commit** `feat(opencode): translate sessions and messages into core values`

---

### Task 4: opencode reader and test fixture

**Files:**
- Create: `internal/data/opencode/source.go`
- Create: `internal/data/opencode/opencodetest/fixture.go` (non-test package so `e2e/` can reuse it)
- Test: `internal/data/opencode/source_test.go`

**Interfaces:**
- Consumes: `newTranslator`, `add`, `finish`, `Stats` (Task 3).
- Produces:
  - `type NotOpencodeError struct{ Path string }`, whose `Error()` is `"<path>: not an opencode database (no session_v2 table)"`. It's a type rather than a sentinel `var` because the archtest forbids package vars.
  - `type Options struct { Agents []string }`
  - `type Item struct { Session core.Session; Messages []core.Message; Todos []core.Todo; Stats Stats }`
  - `func Open(ctx context.Context, path string, opts Options) (*Source, error)`. It stats the path first, so a missing file returns an error wrapping `fs.ErrNotExist`, and returns `*NotOpencodeError` when `session_v2` or `session_message` is absent.
  - `func (s *Source) Each(ctx context.Context, fn func(Item) error) error`
  - `func (s *Source) Close() error`
  - `opencodetest`: `type Session struct { ID, ParentID, Directory, Title, Agent, Model string; Created, Updated int64; Messages []Message; Todos []Todo }`, `type Message struct { ID, Type, Data string; Seq int; Created int64 }`, `type Todo struct { Content, Status, Priority string; Position int }`, `func Write(path string, sessions []Session) error` — creates the DB with spec §2's `session_v2` (only the columns the reader uses, plus `project_id`, `slug`, `version` NOT NULL with dummy values), `session_message`, `todo` DDL, and inserts the rows.

- [ ] **Step 1: Write the failing tests** using `opencodetest.Write` into `t.TempDir()`:
  - `TestEach_ParentsBeforeChildren`: root `ses_b` (created 2), root `ses_a` (created 1), child `ses_c` of `ses_b`, grandchild `ses_d` of `ses_c` → order `ses_a, ses_b, ses_c, ses_d`.
  - `TestEach_OrphanAndCycleBecomeRoots`: `ses_o` with parent `ses_missing`; `ses_x`↔`ses_y` parents of each other → all three yielded exactly once, and `Each` terminates.
  - `TestEach_MessagesBySeqTodosByPosition`: messages inserted out of order with seq 2,1 → yielded in seq order; todos positions 1,0 → yielded `[pos0, pos1]` with `Content`/`Status` set.
  - `TestEach_StopsOnFnError`: `fn` returns `errStop` on the first item → `Each` returns it, `fn` called once.
  - `TestEach_Cancelled`: cancelled ctx → `Each` returns `context.Canceled`.
  - `TestOpen_ReadOnly`: after `Open`, an `Exec("INSERT INTO kv …")` through the source's `db` (in-package test) fails; the fixture file's bytes are unchanged after a full `Each`.
  - `TestOpen_Missing`: non-existent path → `errors.Is(err, fs.ErrNotExist)`.
  - `TestOpen_NotOpencode` (Review Focus 2): a SQLite file with only a `sessions` table → `var ne *NotOpencodeError; errors.As(err, &ne)`.

- [ ] **Step 2: Run** `go test ./internal/data/opencode/...` — expect FAIL.

- [ ] **Step 3: Implement** `source.go`. DSN: `file:<path>?mode=ro&_pragma=busy_timeout(5000)`. Check for both tables via `sqlite_master`. In `Each`: load `(id, parent_id, time_created)` for all sessions into memory, build children lists sorted by `(time_created, id)`, walk roots (no parent, or parent unknown) depth-first with a `visited` set, then make a second pass yielding any unvisited session (cycles) as a root, with its own subtree. Per session: read its `session_v2` row, stream `session_message` rows `ORDER BY seq` into the translator, read `todo` `ORDER BY position`, call `fn`. Check `ctx.Err()` before each session.

- [ ] **Step 4: Run** `go test ./internal/data/opencode/... && go test ./internal/archtest` — expect PASS.

- [ ] **Step 5: Commit** `feat(opencode): read-only session reader and test fixture`

---

### Task 5: Import service

**Files:**
- Create: `internal/service/importer/importer.go`
- Test: `internal/service/importer/importer_test.go`

**Interfaces:**
- Produces:
  - `type Stats struct { SkippedMessages []string; DroppedTypes map[string]int; UntranslatedTools map[string]int; UnimportedAttachments int; EmptyMessages int }` (same fields as `opencode.Stats`; `internal/app` copies across)
  - `type Item struct { Session core.Session; Messages []core.Message; Todos []core.Todo; Stats Stats }`
  - `type Source interface { Each(ctx context.Context, fn func(Item) error) error }`
  - `type Store interface { SessionExists(ctx context.Context, id core.SessionID) (bool, error); ImportSession(ctx context.Context, s core.Session, msgs []core.Message, todos []core.Todo) error }`
  - `type Media interface { Process(data []byte) (core.Media, core.ImageInfo, error) }`
  - `type Options struct { DryRun bool; Progress func(done int) }` — `Progress` (may be nil) called after every 100th item.
  - `type Failure struct { ID core.SessionID; Err error }`
  - `type Summary struct { Imported, Skipped, OrphansAsRoots int; Failed []Failure; Stats Stats /* summed across items */ }`
  - `func Run(ctx context.Context, src Source, st Store, med Media, opts Options) (Summary, error)`

- [ ] **Step 1: Write the failing tests** with a slice-backed fake `Source`, a map-backed fake `Store` (can be told to fail a given ID), and a fake `Media` (returns `{MIME: "image/png", Ref: "ref-<len>"}`, or an error for data `bad`):
  - `TestRun_ImportsAndSkips`: store already has `ses_a`; items `ses_a`, `ses_b` → `Imported 1`, `Skipped 1`, store got only `ses_b`.
  - `TestRun_FailureContinues`: store fails `ses_a` → `Failed == [{ses_a, err}]`, `ses_b` still imported, `Run` returns nil error.
  - `TestRun_FailedParentChildBecomesRoot`: `ses_p` fails, child `ses_c` (ParentID `ses_p`) → saved with `ParentID ""`, `OrphansAsRoots 1`. A child whose parent was *skipped* (already present) keeps its ParentID.
  - `TestRun_MediaToBlobs`: a user `PartAttachment{Media: {Data: "img"}}` and a `PartToolResult` with `Result.Media[{Data: "img2"}]` → saved with `Ref`s set and `Data == nil` in both.
  - `TestRun_MediaFailure`: attachment data `bad` → saved part is `PartText` `[image not imported: <err>]`; result media `bad` → dropped from `Result.Media`, `Output` ends with `\n[image not imported: <err>]`.
  - `TestRun_DryRun`: `DryRun: true` → neither `Process` nor `ImportSession` called; `Imported` still counts what would be imported; `SessionExists` is still consulted.
  - `TestRun_Cancelled`: ctx cancelled inside the second item's `ImportSession` → `Run` returns `context.Canceled` with `Imported 1`.
  - `TestRun_StatsSummed` and `TestRun_Progress`: two items' stats add up; 250 items → `Progress` called with 100 and 200.

- [ ] **Step 2: Run** `go test ./internal/service/importer` — expect FAIL.

- [ ] **Step 3: Implement** `importer.go`. Track the IDs imported in this run, plus failed IDs, in maps; a child is an orphan when its parent is in the failed set, or is neither imported now nor `SessionExists`. Copy the messages' parts before mutating them, so the source's slices aren't modified. A source error other than one from `fn` is returned as is.

- [ ] **Step 4: Run** `go test ./internal/service/importer ./internal/archtest` — expect PASS.

- [ ] **Step 5: Commit** `feat(importer): import service with skip, orphan, and media handling`

---

### Task 6: CLI wiring, e2e, docs

**Files:**
- Create: `internal/app/import.go`
- Test: `internal/app/import_test.go`, `e2e/import_test.go`
- Modify: `internal/app/app.go` (`usage` gains `  jig import opencode [--db PATH] [--dry-run]`; `Run` gains `case "import": return importCmd(ctx, args[1:], std, getenv)`)
- Modify: `AGENTS.md` (package map: `internal/data/opencode/`, `internal/data/opencode/opencodetest/`, `internal/service/importer/`; shared-code row for `ImportSession`), `wiki/Home.md` (link), create `wiki/Importing-from-opencode.md` (spec §8)

**Interfaces:**
- Consumes: Tasks 1, 4, 5.
- Produces: `func importCmd(ctx context.Context, args []string, std Stdio, getenv func(string) string) int`; `func defaultOpencodeDB(getenv func(string) string) string`; `func printSummary(w io.Writer, s importer.Summary, dryRun bool)`; a `sourceAdapter{*opencode.Source}` implementing `importer.Source`.

- [ ] **Step 1: Write the failing unit tests** in `import_test.go`:
  - `TestDefaultOpencodeDB`: `XDG_DATA_HOME=/x` → `/x/opencode/opencode.db`; unset with `HOME=/h` → `/h/.local/share/opencode/opencode.db`.
  - `TestImportCmd_Usage`: `[]` and `["claude"]` → exit 2, stderr contains `usage: jig import opencode`.
  - `TestPrintSummary`: a summary with 3 imported, 1 skipped, 1 failure, 1 orphan, `DroppedTypes{"idle":2}`, `UntranslatedTools{"webfetch":4}` → output contains `imported: 3`, `already present: 1`, `failed: 1`, `ses_bad`, `idle: 2`, `webfetch: 4`; with `dryRun` the first line says `dry run: nothing was written`.
  - `TestPrintSummary_Sanitizes` (Review Focus 5): a failure error and a tool name containing `\x1b[31m` and `\n` → output contains no `\x1b` from them and each failure stays on one line.

- [ ] **Step 2: Write the failing e2e test** `e2e/import_test.go` (`//go:build jigtest`). The fixture, written with `opencodetest.Write`, holds:
  - a root `ses_root` in `env.work` with a user message, an assistant step with an `edit` tool call (status completed), a `task` call whose metadata names `ses_child`, and a final text step;
  - a child `ses_child`;
  - an empty session `ses_empty` in `env.work` (Review Focus 1).

  Tests:
  - `TestE2E_ImportOpencode`:
    - `jig import opencode --db <fixture>` exits 0, and stdout contains `imported: 3`.
    - `sessionIDs` contains `ses_root` and `ses_empty`, but not `ses_child`.
    - Running it again exits 0, and stdout contains `already present: 3`.
    - `runPrompt(t, env, "--session", "ses_root", "continue")` with `jigtestConfig` and a one-text-reply script exits 0. Then the same for `ses_empty`.
  - `TestE2E_ImportOpencode_DryRun`: exits 0, and `sessionIDs` is empty afterwards.
  - `TestE2E_ImportOpencode_BadDB`:
    - A missing path exits 2.
    - The jig data dir's own `jig.db` (created by a prior `jig sessions`) exits 2, and stderr contains `not an opencode database`.

- [ ] **Step 3: Run** `go test ./internal/app -run 'Import|Summary|OpencodeDB'` and `go test -tags jigtest ./e2e -run ImportOpencode` — expect FAIL.

- [ ] **Step 4: Implement** `import.go`:
  - Parse the flags with `flag.NewFlagSet("import opencode", ContinueOnError)`. Load the environment with `loadEnv("", getenv, staticTrust(false))` and open the store with `openStore`.
  - Agent names come from `agents.New(e.cfg(), discover(e, std.Err).sources)`, using its `Primary()` and `Subagents()`.
  - `opencode.Open` errors map to exit 2.
  - Build the media pipeline with `media.New(blobfs.New(e.blobsDir()))`.
  - `Progress` prints `imported N sessions…` lines to stderr.
  - Exit 1 if `len(Failed) > 0`, and 1 for a source error (with `printLine`).
  - Split helpers so each func is ≤ 40 lines.

  Then update `AGENTS.md` and the wiki.

- [ ] **Step 5: Run** `make check` — expect all four targets to pass.

- [ ] **Step 6: Manual check** (not automated; record the results in the commit body):
  - `go build -o bin/jig ./cmd/jig && /usr/bin/time -v bin/jig import opencode --dry-run` against the real `~/.local/share/opencode/opencode.db`.
  - Note: sessions counted, run time, max RSS, and the untranslated-tool list.
  - Expect about 4,045 sessions and memory well below the 4.6 GB file size.

- [ ] **Step 7: Commit** `feat(app): jig import opencode` (then `docs: importing from opencode` if docs are a separate commit)
