# jig: Model Reasoning Effort

- **Status:** draft, awaiting review
- **Date:** 2026-09-30
- **Builds on:** `docs/superpowers/specs/2026-09-27-jig-v1-core-design.md` and `docs/superpowers/specs/2026-09-27-jig-plan2-tui-design.md` (implemented).

## 1. Intent

**What the author asked for**

- jig has no concept of effort. Find out what effort it uses today, and build a way for jig to select an effort for the model.

**What jig does today**

jig never sends an effort or thinking setting. `llm.ToFantasy` sets only the prompt, tools, and `MaxOutputTokens`; the only provider option jig ever sets is Anthropic cache control. So every provider's API default applies:

| Provider type | Sent today | Effect |
|---|---|---|
| `anthropic` | no `thinking`, no `output_config.effort` | The API's server-side defaults. The Opus 5 / Sonnet 5 families **already think** on every turn, at the API's default effort (not verified here; Anthropic documents `high`), with thinking display **omitted**: the local store holds 111 reasoning parts across 175 `claude-opus-5-5` assistant messages and 527 for `claude-sonnet-5`, every one `{"text":""}`. That is why the TUI shows empty "thinking" lines. (fantasy asks for a summarized display only when jig sends thinking or effort options.) Older models without adaptive thinking do not think unless asked. |
| `openai` | no `reasoning_effort` (Chat Completions; the Responses API is not enabled) | OpenAI's per-model default. |
| `google` | no `thinking_config` | Gemini's default thinking; thought text is not returned. |
| `openrouter`, `openai-compat` | no `reasoning` field | The upstream model's default. |

The data needed already exists and is dropped:

- catwalk's `Model` carries `reasoning_levels` (e.g. `["low","medium","high","xhigh","max"]`) and `default_reasoning_effort`. `internal/client/catalog/convert.go` keeps only `CanReason`.
- fantasy v0.45.2 supports effort per provider: `anthropic.ProviderOptions.Effort` (adaptive thinking plus `output_config.effort`), `openai.ProviderOptions.ReasoningEffort`, `openaicompat.ProviderOptions.ReasoningEffort`, `openrouter.ProviderOptions.Reasoning.Effort`, and `google.ProviderOptions.ThinkingConfig.ThinkingLevel`.

**Decisions made in review**

1. **Effort mirrors model.** A global config default (`default_effort`), a per-agent `effort`, a `jig run --effort` flag, and a per-session "Switch effort…" picker action in the TUI, persisted on the session.
2. **When nothing is chosen, jig sends the model's catalog `default_reasoning_effort`.** For the Opus 5 / Sonnet 5 families this does not turn thinking on (they already think); it makes the effort explicit and makes the thinking visible as a summary. It can also *change* the level: catwalk's default for `claude-opus-5-5` is `medium`, possibly below the API's own default. This is accepted; set `default_effort` (or an agent `effort`) to pin a level. For Claude models that only think when asked, it does turn thinking on: a behavior and cost change.
3. **Subagents** use their agent's `effort`, else `default_effort`, else the catalog default. They never inherit the parent session's choice.
4. **Small-model calls** (titles, compaction) send the model's lowest level.
5. **An unsupported level is clamped** to the nearest supported level on the scale `none < minimal < low < medium < high < xhigh < max`; on a tie, the lower one. The stored choice is kept, so switching back to a model that supports it restores it.
6. **A model with no catalog levels is not effort-controllable** (e.g. `claude-sonnet-4-5`, `claude-haiku-4-5`, `gemini-2.5-*`, which take budget tokens). jig sends nothing for it, the picker action is disabled, and the status bar omits effort.
7. **Precedence for primary runs is session > agent `effort` > `default_effort` > catalog default.** An explicit in-session choice (picker or `--effort`) wins, so "Switch effort…" is never a silent no-op. Note this is the reverse of `ResolveModel`, which puts agent before session; that model inconsistency (the status bar's `modelRef` already puts session first) is out of scope.
8. **Approach A:** effort is its own field next to `Model`. The service layer resolves the *requested* level; the Runner turns it into the *effective* level with the model's `ModelInfo`; `client/llm` only maps an already-valid level onto provider options.

**Success criteria**

- With no config, a Claude model that has catalog levels runs with adaptive thinking at its catalog default effort, and its thinking shows as summarized text in the TUI's reasoning blocks (today those blocks are empty for Opus 5 / Sonnet 5).
- `jig run --effort low …` sends `low` (or the nearest supported level) to the provider, and stores it on the session.
- In the TUI, "Switch effort…" lists the current model's levels, the status bar shows `agent · model · <level>`, and the choice survives a restart (it is on the session).
- Switching to a model without the chosen level clamps; switching back restores it.
- A model without levels behaves exactly as today (nothing sent).
- `make check` passes, including the architecture tests and performance budgets.

## 2. Scope

**In scope**

- `core.Effort`, `ModelInfo` levels, and the pure helpers (§3).
- Config (`default_effort`, agent `effort`, custom provider `efforts`), the `--effort` flag, the session column, and `SessionService.SetEffort` (§4).
- Resolution in `chat`, `task`, `session`, and the Runner (§5).
- Per-provider mapping in `client/llm` (§6).
- The TUI picker action and status bar segment (§7).
- `jig models` shows levels.

**Out of scope**

- Mapping effort onto thinking budget tokens for models without levels.
- Switching OpenAI to the Responses API (which would surface OpenAI reasoning text).
- Per-model levels in `[providers.x]` config beyond one `efforts` list per custom provider (§4.1).
- A default keybinding or a "cycle effort" action (a user can bind `effort.switch` via `[keybinds]`).
- Fixing `ResolveModel` vs. `modelRef` precedence for models.

## 3. Core types (`internal/core/effort.go`)

```go
// Effort is a reasoning effort level. "" means unset.
type Effort string

const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)
```

- `ParseEffort(s string) (Effort, error)`: trims and lowercases `s` (so `--effort High` works); `""` → `""`, a scale value → itself, anything else → an error naming the valid levels.
- `EffortLevels() []Effort`: the scale, lowest first (a fresh slice; no package var).
- `(Effort).Known() bool`: whether it is on the scale; `(Effort).rank() int`: its position, -1 if not.
- `ModelInfo` gains `Efforts []Effort` (ordered as the catalog lists them) and `DefaultEffort Effort`.
- `EffectiveEffort(info ModelInfo, want Effort) Effort`:
  - `len(info.Efforts) == 0` → `""`.
  - `want == ""` → `info.DefaultEffort` (which may be `""`: then nothing is sent).
  - `want` in `info.Efforts` → `want`.
  - otherwise the level in `info.Efforts` with the smallest rank distance from `want`; on a tie, the lower rank.
- `LowestEffort(info ModelInfo) Effort`: the lowest-ranked entry of `info.Efforts`, or `""`.

`core` stays stdlib-only.

## 4. Configuration, persistence, and ports

### 4.1 Config (`internal/data/config`, `internal/data/agentfs`)

- Top level: `default_effort = "high"` → `core.Config.DefaultEffort Effort`.
- Agent: `[agents.<name>] effort = "low"` and agent frontmatter `effort: low` → `AgentConfig.Effort` → `core.Agent.Effort`. Merged like `model`.
- Custom provider: `[providers.<id>] efforts = ["low", "medium", "high"]` → `ProviderConfig.Efforts`. `customModels` gives every model in that provider's `models` list these levels, with no `DefaultEffort` (so nothing is sent until someone picks a level or sets `default_effort`). This is the one config route to levels for models catwalk doesn't know, needed for local reasoning servers and for the e2e test (§8). Catalog providers are unaffected.
- Every value goes through `core.ParseEffort` at load; a bad one is a config error at startup (same exit and message style as a bad model ref).
- `docs/config.example.toml` documents all three keys.

### 4.2 Trust (`internal/service/trust`)

- `Restrict` keeps an untrusted project's `default_effort`, as it keeps `default_model`. Agent `effort` follows the agent's existing treatment. Provider `efforts` follows the provider's existing treatment.
- `Effects` lists `default_effort`, each agent's `effort`, and each provider's `efforts`, so they appear in the trust dialog.

### 4.3 Store (`internal/data/store`)

- Migration `0002_session_effort.sql`: `ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT ''`.
- `core.Session` gains `Effort Effort`; `sessions.go` reads and writes it with `Model`.
- A stored value that fails `ParseEffort` (hand-edited DB, a level removed from the scale) is skipped by `ResolveEffort` as if unset; it never blocks a resume. The Runner's `run start` line shows the requested level that resulted.

### 4.4 Ports (`internal/core/ports.go`)

- `SendRequest` gains `Effort string`. `chat.prepare` parses it (a bad value is a `*ConfigError`, headless exit 2); `commit` stores it on the session, as it stores the `--model` flag.
- `SessionService` gains `SetEffort(ctx, id SessionID, effort Effort) error`, where `""` clears the session's choice. It runs under `session.Service`'s mutex like `Configure`. `Configure` is unchanged: its `""` already means "leave unchanged".

### 4.5 CLI (`internal/app`)

- `jig run --effort LEVEL`, passed as `SendRequest.Effort`. `runUsage` and the `app.go` usage text gain `[--effort E]`.
- `jig models <provider>` appends each model's levels and default when it has any, e.g. `effort low…max (high)`.

## 5. Resolution

**Requested level**

- `agents.Service.ResolveEffort(a core.Agent, session core.Effort) core.Effort`: the first `Known()` value of `session`, `a.Effort`, `cfg.DefaultEffort`; else `""`.
- `ext.RunContext` gains `Effort core.Effort`: the *requested* level.
- **Primary run:** `chat.prepare` sets `rc.Effort = ResolveEffort(agent, sessionEffort)`, where `sessionEffort` is the parsed `req.Effort` if set, else the session's stored effort.
- **Subagent:** the `task` tool sets `childRC.Effort = ResolveEffort(sub, "")` (agent `effort`, then `default_effort`). It never reads the parent's `rc.Effort`.

**Effective level**

- The Runner already calls `LLMs.For(rc.Model)` once per run and holds the `ModelInfo` in its run state. `agent/request.go` sets `LLMRequest.Effort = core.EffectiveEffort(st.info, rc.Effort)` on every request of the run; a catalog refresh applies from the next run.
- `core.LLMRequest` gains `Effort Effort`: always the effective level (`""` means send nothing).
- **Small model:** `session.complete` computes `core.LowestEffort(info)` from the `ModelInfo` that `LLMs.For(model)` already returns, and passes it through a new `effort core.Effort` parameter on `agent.Complete`.
- **Debug log:** the Runner's `run start` line gains `effort=<requested>` and `effort_sent=<effective>`.

## 6. Provider mapping (`internal/client/llm`)

- The adapter's `prepare func(*fantasy.Call)` becomes `prepare func(*fantasy.Call, core.LLMRequest)`, run after `ToFantasy`. `ToFantasy` stays provider-agnostic.
- Each factory supplies a `prepare` that sets `Call.ProviderOptions` when `req.Effort != ""`, and sets nothing otherwise (identical to today):

| Factory type | `ProviderOptions` entry |
|---|---|
| `anthropic` | `anthropic.Name: &anthropic.ProviderOptions{Effort: &e}`. fantasy sends adaptive thinking and `output_config.effort`, and requests summarized thinking display for families that default to omitted, so thinking shows as reasoning blocks. The cache-control hook runs in the same `prepare`, unchanged. |
| `openai` | `openai.Name: &openai.ProviderOptions{ReasoningEffort: &e}` |
| `openai-compat` | `openaicompat.Name: &openaicompat.ProviderOptions{ReasoningEffort: &e}` |
| `openrouter` | `openrouter.Name: &openrouter.ProviderOptions{Reasoning: &openrouter.ReasoningOptions{Effort: &e}}` |
| `google` | `google.Name: &google.ProviderOptions{ThinkingConfig: &google.ThinkingConfig{ThinkingLevel: &UPPER(e), IncludeThoughts: &true}}`. `IncludeThoughts` makes Gemini return thought summaries, so its reasoning shows like Claude's. |

- `none` passes through for OpenAI and OpenRouter, where it is a real "off" value. Anthropic and Gemini never list `none` in their catalog levels, so clamping means they never receive it.
- The adapter does no validation: the level was clamped against the catalog in the Runner. A provider that still rejects it (a stale catalog) returns an ordinary `LLMError`, like any 400 today; there is no retry without effort.

## 7. TUI (`internal/ui`, `internal/bubbles/statusbar`)

**Action and picker level**

- `actions.EffortSwitch ID = "effort.switch"`, titled "Switch effort…", group "Agent & model", `Drill: true`, placed after "Switch model…". No default key.
- `drillLevel` maps it to a new `levelEfforts` ("Effort") level. Its items come from the cached catalog (`s.cat.providers`) for the current model (`modelRef()`), with no port call:
  - `Model default (<catalog default>)` (or `Model default` when the catalog default is `""`), which clears the session's choice;
  - then each of the model's levels, in catalog order.
  - `Current` marks the item that matches the session's stored choice (the "Model default" item when there is none).
- `rootItems` shows the action disabled when the current model has no levels.
- On a choice, `pickerCtl.chosen` calls `SessionService.SetEffort` in a Cmd (`cmds.go`) when a session exists, and updates `s.info.Effort`. Without a session, it only sets `s.info.Effort` locally; the next `Send` carries it as `SendRequest.Effort` (as the model picker does with `Model`).

**Status bar**

- `statusbar.State` gains `Effort string`, drawn as a third item in the agent·model segment (`agent · model · high`), omitted when empty.
- `sessionState.effort()` computes it: `core.EffectiveEffort(info, want)`, where `info` is the current model's catalog entry and `want` follows §5's primary-run precedence (session, then the agent's `Effort` from `s.cat.agents`, then `DefaultEffort`).
- `ui.Options` gains `DefaultEffort core.Effort`, set in `internal/app/tui.go` next to `DefaultModel`.
- The effort string passes `ansi.SanitizeLine` like the model name (it originates in config or the store).

## 8. Testing

- **`core`:** table tests for `ParseEffort`; `EffectiveEffort` (no levels, `""` → default, `""` with no default, exact match, clamp up, clamp down, tie → lower); `LowestEffort`.
- **`client/catalog`:** `convert` carries `reasoning_levels` and `default_reasoning_effort`; `customModels` applies a provider's `efforts`.
- **`data/config`, `data/agentfs`:** the new keys parse and merge; a bad level fails the load.
- **`data/store`:** the migration applies; `effort` round-trips through `Create`/`Update`/`Get`.
- **`service/agents`:** `ResolveEffort` precedence.
- **`service/chat`:** `rc.Effort` for each precedence case; `--effort` is stored on the session; a bad `--effort` is a `*ConfigError`.
- **`service/task`:** a child's `rc.Effort` is its agent's `effort`, else `default_effort`, never the parent's.
- **`service/session`:** `SetEffort` sets and clears under the mutex; `complete` sends `LowestEffort`.
- **`service/agent`:** `LLMRequest.Effort` is the clamped effective level from `ModelInfo`; the `run start` debug line has both attrs (`logtest`).
- **`service/trust`:** `Effects` lists the new keys; `Restrict` keeps `default_effort`.
- **`client/llm`:** per factory, `prepare` with and without an effort yields the expected `ProviderOptions` (§6 table); the Anthropic cache hook still fires.
- **`ui`:** the efforts level (items, `Current`, clearing, disabled for a model without levels, no-session path) via `newTestApp`; status bar golden frames with and without effort (`JIG_UPDATE_GOLDEN=1` to regenerate); `actions` catalogue test includes the new action.
- **e2e (`jigtest`):** `jigtest.Turn` gains `ExpectEffort *string`, failing the turn when `req.Effort` differs. The e2e config gives the jigtest provider `efforts = ["low", "medium", "high"]`. `jig run --effort low` asserts `low` arrives; a second test with `--effort max` asserts it clamps to `high`.
- `make check` passes.

## 9. Files touched (expected)

- `internal/core/effort.go` (new), `model.go`, `llm.go`, `config.go`, `session.go`, `ports.go`; `internal/core/ext/ext.go` (`RunContext.Effort`); `internal/core/agent.go` (`Agent.Effort`).
- `internal/client/catalog/convert.go`; `internal/client/llm/providers.go`, `stream.go`, `anthropic_cache.go`; `internal/client/llm/jigtest/jigtest.go`.
- `internal/data/config/dto.go`, `merge.go`; `internal/data/agentfs/agentfs.go`; `internal/data/store/sessions.go`, `migrations/0002_session_effort.sql`.
- `internal/service/agents` (`resolve.go`, merge); `service/chat/chat.go`; `service/task/task.go`; `service/session/session.go`; `service/agent/request.go`, `complete.go`, `runner.go`; `service/trust/trust.go`, `effects.go`.
- `internal/ui/actions/actions.go`, `pickerlevels.go`, `mode_picker.go`, `cmds.go`, `status.go`, `session.go`; `internal/bubbles/statusbar`.
- `internal/app/cli.go`, `app.go`, `headless.go`, `tui.go`, `models.go`, `ports.go`.
- `docs/config.example.toml`; `AGENTS.md` (the invariant "`LLMRequest.Effort` is always the effective level, clamped by the Runner against `ModelInfo`; `client/llm` never validates it", and a Shared-code row for `core.EffectiveEffort`).
