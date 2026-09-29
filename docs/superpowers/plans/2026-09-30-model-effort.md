# Model Reasoning Effort Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let jig choose a reasoning effort for the model (config default, per-agent, `--effort`, per-session TUI picker), defaulting to the catalog's `default_reasoning_effort`, clamped to the model's levels, and sent to every provider through fantasy's provider options.

**Architecture:** A new `core.Effort` type plus pure helpers (`ParseEffort`, `EffectiveEffort`, `LowestEffort`). Services resolve the *requested* level (session > agent > `default_effort`) into `ext.RunContext.Effort`; the Runner turns it into the *effective* level with the run's `ModelInfo` and puts it on `LLMRequest.Effort`; `client/llm` maps that onto each provider type's `fantasy.ProviderOptions`. The TUI computes the same effective level for its status bar and offers a "Switch effort…" picker level.

**Tech Stack:** Go, charm.land/fantasy v0.45.2, charm.land/catwalk v0.52.56, SQLite store, bubbletea v2 TUI.

**Spec:** `docs/superpowers/specs/2026-09-30-model-effort-design.md`

## Global Constraints

- Every commit passes `make check` (build, `go test ./... -race` + jigtest-tagged tests, golangci-lint with and without `--build-tags jigtest`, `gofmt -l .` empty).
- Effort scale, lowest first: `none, minimal, low, medium, high, xhigh, max`. Nothing else is a valid level.
- `ParseEffort` trims and lowercases its input; `""` parses to `""` (unset).
- Precedence for a primary run: session > agent `effort` > `default_effort` > catalog default. Subagents: agent `effort` > `default_effort` > catalog default (never the parent session's).
- Small-model calls (titles, compaction) send `core.LowestEffort(info)`.
- A model with no levels (`len(info.Efforts) == 0`) gets `""`: nothing is sent.
- Clamping picks the nearest level by scale rank; a tie goes to the lower level.
- `LLMRequest.Effort` is always the effective level. `client/llm` never validates it; with `""` it sets no provider option.
- Architecture limits (`internal/archtest`): source files ≤ 500 lines, structs ≤ 15 fields and ≤ 20 methods, `internal/app` funcs ≤ 40 lines, no package-level mutable vars, no `time.Now`/`time.Sleep` in tests. `internal/ui/app.go` is at 499 lines and `ui.sessionState` already has 20 methods: add at most one line to `app.go` and **no methods** to `sessionState` (use free functions).
- `internal/ui` does no I/O; every port call runs in a `tea.Cmd` in `internal/ui/cmds.go`. Untrusted strings pass `ansi.SanitizeLine` before rendering.

## Review Focus

- `--effort High` / ` high ` (mixed case, stray spaces) is accepted as `high`, not rejected. → Task 1 `TestParseEffort`.
- A session whose stored `effort` is not on the scale (hand-edited DB, a level from a newer jig) still resumes and runs with the model's default, no error. → Task 7 `TestSend_UnknownStoredEffortIgnored`.
- Switching to a model with no effort levels sends nothing and hides effort in the status bar; switching back to the first model shows the session's choice again. → Task 6 `TestRunner_RequestEffort` (no-levels row) and Task 11 `TestApp_EffortSurvivesModelRoundTrip`.
- A custom provider with `efforts = [...]` and no `default_effort` sends nothing until a level is chosen (not its lowest level). → Task 1 `TestEffectiveEffort` ("levels, no default" row) and Task 2 `TestCustomProvider_Efforts`.
- Choosing "Model default" in the TUI when the agent has its own `effort` falls back to the agent's level, and the status bar says so. → Task 11 `TestPicker_EffortDefaultFallsBackToAgent`.

**Behavior note:** Opus 5 / Sonnet 5 already think today (at the API's default effort, with thinking text hidden: the stored reasoning parts are all empty). After this change they get the catalog default effort (`medium` for `claude-opus-5-5`, `high` for most others) and summarized, visible thinking. This is intended (spec §1, decision 2).

**Known risk (not testable offline):** fantasy sends Anthropic effort as adaptive thinking plus `output_config.effort`. Older Claude models that the catalog lists with levels (e.g. `claude-opus-4-5-20251101`) may reject adaptive thinking. Per the spec, such a rejection surfaces as an ordinary `LLMError`; note it in the final report if a live check shows it.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/effort.go` (new) | `Effort`, scale, `ParseEffort`, `Known`, `EffectiveEffort`, `LowestEffort` |
| `internal/core/{model,llm,session,agent,config,ports}.go`, `internal/core/ext/ext.go` | new `Effort`/`Efforts`/`DefaultEffort` fields; `SessionService.SetEffort` |
| `internal/client/catalog/convert.go` | catwalk levels → `ModelInfo`; custom provider `efforts` |
| `internal/data/config/{dto,merge}.go`, `internal/data/agentfs/agentfs.go` | parse and merge `default_effort`, `providers.*.efforts`, agent `effort` |
| `internal/app/env.go` | startup validation of `default_effort` and provider `efforts` |
| `internal/service/agents/{merge,resolve}.go` | agent `effort` validation; `ResolveEffort` |
| `internal/data/store/sessions.go`, `migrations/0002_session_effort.sql` (new) | persist `sessions.effort` |
| `internal/service/session/session.go`, `internal/service/agent/complete.go` | `SetEffort`; small-model lowest effort |
| `internal/service/agent/{request,runner}.go` | effective effort on every request; `run start` log attrs |
| `internal/service/chat/chat.go`, `internal/service/task/task.go` | requested effort for primary runs and subagents; `--effort` stored |
| `internal/client/llm/effort.go` (new), `providers.go`, `stream.go`, `anthropic_cache.go` | per-provider-type prepare hooks |
| `internal/service/trust/{trust,effects}.go` | keep `default_effort`; list new keys as effects |
| `internal/bubbles/statusbar/{statusbar,view}.go` | `State.Effort` |
| `internal/ui/{status,session,app,send,mode_picker,pickerlevels,cmds}.go`, `internal/ui/actions/actions.go` | status bar effort; `effort.switch` action and `efforts` picker level |
| `internal/app/{cli,app,headless,models,tui}.go` | `--effort`; `jig models` levels; `ui.Options.DefaultEffort` |
| `internal/client/llm/jigtest/jigtest.go`, `e2e/effort_test.go` (new) | `ExpectEffort`; end-to-end check |
| `docs/config.example.toml`, `AGENTS.md` | docs |

### Task 1: Core effort type, helpers, and fields

**Files:**
- Create: `internal/core/effort.go`, `internal/core/effort_test.go`
- Modify: `internal/core/model.go` (`ModelInfo`), `internal/core/llm.go` (`LLMRequest`), `internal/core/session.go` (`Session`), `internal/core/agent.go` (`Agent`), `internal/core/config.go` (`Config`, `ProviderConfig`, `AgentConfig`), `internal/core/ports.go` (`SendRequest`), `internal/core/ext/ext.go` (`RunContext`)

**Interfaces:**
- Produces:
  - `type Effort string`; consts `EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax`
  - `func EffortLevels() []Effort`, `func (e Effort) Known() bool`, `func ParseEffort(s string) (Effort, error)`
  - `func EffectiveEffort(info ModelInfo, want Effort) Effort`, `func LowestEffort(info ModelInfo) Effort`
  - Fields: `ModelInfo.Efforts []Effort`, `ModelInfo.DefaultEffort Effort`, `LLMRequest.Effort Effort`, `Session.Effort Effort`, `Agent.Effort Effort`, `Config.DefaultEffort string`, `ProviderConfig.Efforts []string`, `AgentConfig.Effort string`, `SendRequest.Effort string`, `ext.RunContext.Effort core.Effort`

- [ ] **Step 1: Write the failing tests** — `internal/core/effort_test.go`:

```go
package core

import (
	"strings"
	"testing"
)

func TestParseEffort(t *testing.T) {
	tests := []struct {
		in      string
		want    Effort
		wantErr bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"high", EffortHigh, false},
		{"High", EffortHigh, false},
		{" xhigh ", EffortXHigh, false},
		{"none", EffortNone, false},
		{"turbo", "", true},
		{"hi", "", true},
	}
	for _, tt := range tests {
		got, err := ParseEffort(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseEffort(%q) = %q, %v; want %q, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
	_, err := ParseEffort("turbo")
	if err == nil || !strings.Contains(err.Error(), "none, minimal, low, medium, high, xhigh, max") {
		t.Errorf("ParseEffort(turbo) err = %v, want it to list the levels", err)
	}
}

func TestEffortKnown(t *testing.T) {
	for _, e := range EffortLevels() {
		if !e.Known() {
			t.Errorf("%q.Known() = false", e)
		}
	}
	for _, e := range []Effort{"", "turbo", "High"} {
		if e.Known() {
			t.Errorf("%q.Known() = true", e)
		}
	}
}

func TestEffectiveEffort(t *testing.T) {
	levels := ModelInfo{Efforts: []Effort{EffortLow, EffortMedium, EffortHigh}, DefaultEffort: EffortMedium}
	sparse := ModelInfo{Efforts: []Effort{EffortLow, EffortHigh}, DefaultEffort: EffortHigh}
	noDefault := ModelInfo{Efforts: []Effort{EffortLow, EffortHigh}}
	tests := []struct {
		name string
		info ModelInfo
		want Effort
		out  Effort
	}{
		{"no levels, no want", ModelInfo{}, "", ""},
		{"no levels, want high", ModelInfo{DefaultEffort: EffortHigh}, EffortHigh, ""},
		{"unset uses default", levels, "", EffortMedium},
		{"levels, no default", noDefault, "", ""},
		{"exact", levels, EffortLow, EffortLow},
		{"clamp down", levels, EffortMax, EffortHigh},
		{"clamp up", levels, EffortNone, EffortLow},
		{"tie goes lower", sparse, EffortMedium, EffortLow},
		{"unknown want uses default", levels, "turbo", EffortMedium},
	}
	for _, tt := range tests {
		if got := EffectiveEffort(tt.info, tt.want); got != tt.out {
			t.Errorf("%s: EffectiveEffort(%v, %q) = %q, want %q", tt.name, tt.info.Efforts, tt.want, got, tt.out)
		}
	}
}

func TestLowestEffort(t *testing.T) {
	if got := LowestEffort(ModelInfo{}); got != "" {
		t.Errorf("no levels: %q, want empty", got)
	}
	info := ModelInfo{Efforts: []Effort{EffortMedium, EffortLow, EffortHigh}}
	if got := LowestEffort(info); got != EffortLow {
		t.Errorf("LowestEffort = %q, want low", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/core/ -run 'Effort' -v`
Expected: FAIL to compile (`undefined: Effort`, `ParseEffort`, …).

- [ ] **Step 3: Implement** — `internal/core/effort.go`:

```go
package core

import (
	"fmt"
	"slices"
	"strings"
)

// Effort is a reasoning effort level. "" means unset: no effort is sent.
type Effort string

// The effort scale, lowest first (see EffortLevels).
const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// EffortLevels returns every level on the scale, lowest first.
func EffortLevels() []Effort {
	return []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
}

// rank is e's position on the scale, or -1 when e is not on it.
func (e Effort) rank() int { return slices.Index(EffortLevels(), e) }

// Known reports whether e is on the scale ("" is not).
func (e Effort) Known() bool { return e.rank() >= 0 }

// ParseEffort parses s, trimmed and lowercased, as an Effort. "" parses
// to "" (unset); anything off the scale is an error naming the levels.
func ParseEffort(s string) (Effort, error) {
	e := Effort(strings.ToLower(strings.TrimSpace(s)))
	if e == "" || e.Known() {
		return e, nil
	}
	names := make([]string, 0, len(EffortLevels()))
	for _, l := range EffortLevels() {
		names = append(names, string(l))
	}
	return "", fmt.Errorf("unknown effort %q (want one of: %s)", s, strings.Join(names, ", "))
}

// EffectiveEffort is the level a request to a model described by info
// carries when want was asked for: "" when the model has no levels; the
// catalog default when want is unset (or off the scale); want itself when
// the model lists it; else the model's level nearest want on the scale,
// the lower one on a tie.
func EffectiveEffort(info ModelInfo, want Effort) Effort {
	if len(info.Efforts) == 0 {
		return ""
	}
	if !want.Known() {
		return info.DefaultEffort
	}
	if slices.Contains(info.Efforts, want) {
		return want
	}
	best, bestDist := Effort(""), -1
	for _, e := range info.Efforts {
		r := e.rank()
		if r < 0 {
			continue
		}
		d := r - want.rank()
		if d < 0 {
			d = -d
		}
		if bestDist < 0 || d < bestDist || (d == bestDist && r < best.rank()) {
			best, bestDist = e, d
		}
	}
	if best == "" {
		return info.DefaultEffort
	}
	return best
}

// LowestEffort is the lowest level info lists, or "" when it lists none.
func LowestEffort(info ModelInfo) Effort {
	var low Effort
	for _, e := range info.Efforts {
		if e.Known() && (low == "" || e.rank() < low.rank()) {
			low = e
		}
	}
	return low
}
```

Add the fields (keep the surrounding alignment; run `gofmt -w` after):

`internal/core/model.go` — in `ModelInfo`, after `SupportsImages bool`:
```go
	// Efforts are the reasoning effort levels the model accepts, in
	// catalog order; none means effort is not controllable.
	Efforts       []Effort
	DefaultEffort Effort // the catalog's default level, "" if none
```

`internal/core/llm.go` — in `LLMRequest`, after `MaxOutputTokens int64`:
```go
	// Effort is the effective reasoning effort (already defaulted and
	// clamped against the model's levels); "" sends none.
	Effort Effort
```

`internal/core/session.go` — in `Session`, after `Model string`:
```go
	Effort    Effort // the session's chosen effort; "" = none chosen
```

`internal/core/agent.go` — in `Agent`, after `ModelAlias string`:
```go
	Effort      Effort
```

`internal/core/config.go` — in `Config`, after `SmallModel string`: `DefaultEffort string // TOML: default_effort`; in `ProviderConfig`, after `ImageModels`: `Efforts []string // TOML: efforts; effort levels of every model in Models`; in `AgentConfig`, after `Model string`: `Effort string`.

`internal/core/ports.go` — in `SendRequest`, after `Model string`:
```go
	// Effort is a reasoning effort level ("" leaves the session's);
	// it is stored on the session like Model.
	Effort string
```

`internal/core/ext/ext.go` — in `RunContext`, after `Model core.ModelRef`:
```go
	// Effort is the requested reasoning effort (not yet clamped to the
	// model's levels; the Runner does that per request).
	Effort core.Effort
```

- [ ] **Step 4: Run to verify it passes**

Run: `gofmt -w internal/core && go build ./... && go test ./internal/core/... ./internal/archtest/...`
Expected: PASS (the new fields break no existing code; all struct literals are keyed).

- [ ] **Step 5: Commit**

```bash
git add internal/core
git commit -m "feat(core): reasoning effort type, scale, and clamping helpers"
```

---

### Task 2: Catalog carries effort levels

**Files:**
- Modify: `internal/client/catalog/convert.go`
- Test: `internal/client/catalog/convert_test.go`

**Interfaces:**
- Consumes: `core.ParseEffort`, `ModelInfo.Efforts/DefaultEffort`, `ProviderConfig.Efforts` (Task 1)
- Produces: `func parseEfforts(levels []string) []core.Effort` (package-private); catalog `ModelInfo`s with levels

- [ ] **Step 1: Write the failing tests** — append to `internal/client/catalog/convert_test.go`:

```go
func TestConvertModel_Efforts(t *testing.T) {
	m := convertModel("anthropic", catwalk.Model{
		ID:                     "claude-x",
		CanReason:              true,
		ReasoningLevels:        []string{"low", "medium", "bogus", "high"},
		DefaultReasoningEffort: "high",
	})
	want := []core.Effort{core.EffortLow, core.EffortMedium, core.EffortHigh}
	if !slices.Equal(m.Efforts, want) || m.DefaultEffort != core.EffortHigh {
		t.Errorf("Efforts = %v default %q, want %v default high", m.Efforts, m.DefaultEffort, want)
	}

	none := convertModel("anthropic", catwalk.Model{ID: "claude-old", CanReason: true, DefaultReasoningEffort: "bogus"})
	if none.Efforts != nil || none.DefaultEffort != "" {
		t.Errorf("no levels: Efforts = %v default %q, want none", none.Efforts, none.DefaultEffort)
	}
}

func TestCustomProvider_Efforts(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(Options{
		CachePath: filepath.Join(t.TempDir(), "catalog.json"),
		Clock:     clk,
		Custom: map[string]core.ProviderConfig{
			"local": {Type: "openai-compat", Models: []string{"a", "b"}, Efforts: []string{"low", "High"}},
		},
	})
	for _, id := range []string{"a", "b"} {
		m, ok := c.Model(core.ModelRef{Provider: "local", Model: id})
		if !ok {
			t.Fatalf("Model(local/%s) not found", id)
		}
		if !slices.Equal(m.Efforts, []core.Effort{core.EffortLow, core.EffortHigh}) || m.DefaultEffort != "" {
			t.Errorf("local/%s Efforts = %v default %q, want [low high] and no default", id, m.Efforts, m.DefaultEffort)
		}
	}
}
```

Add `"slices"` and `"charm.land/catwalk/pkg/catwalk"` to the test imports if missing.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/client/catalog/ -run 'Efforts' -v`
Expected: FAIL (`Efforts = [] default ""`).

- [ ] **Step 3: Implement** — in `internal/client/catalog/convert.go`:

In `convertModel`, before `return`, add `def, _ := core.ParseEffort(m.DefaultReasoningEffort)` and add to the literal:
```go
		Efforts:          parseEfforts(m.ReasoningLevels),
		DefaultEffort:    def,
```
(`ParseEffort` returns `""` with its error, so an unknown default becomes `""`.)

In `customModels`, add to the literal: `Efforts: parseEfforts(cfg.Efforts),` and update its doc comment: "Every model gets cfg.Efforts as its effort levels, with no default."

Add at the end of the file:
```go
// parseEfforts keeps the levels that are on jig's effort scale, in
// order, dropping any others; nil when none are.
func parseEfforts(levels []string) []core.Effort {
	var out []core.Effort
	for _, l := range levels {
		if e, err := core.ParseEffort(l); err == nil && e != "" {
			out = append(out, e)
		}
	}
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/client/catalog/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/catalog
git commit -m "feat(catalog): carry reasoning levels and default effort"
```

---

### Task 3: Config keys, agent frontmatter, startup validation

**Files:**
- Modify: `internal/data/config/dto.go`, `internal/data/config/merge.go`, `internal/data/agentfs/agentfs.go`, `internal/app/env.go`, `docs/config.example.toml`
- Test: `internal/data/config/load_test.go`, `internal/data/config/merge_test.go`, `internal/data/agentfs/agentfs_test.go`, `internal/app/env_test.go` (create if absent; else the file holding env tests)

**Interfaces:**
- Consumes: `Config.DefaultEffort`, `ProviderConfig.Efforts`, `AgentConfig.Effort`, `core.ParseEffort` (Task 1)
- Produces: TOML `default_effort`, `[providers.<id>] efforts`, `[agents.<name>] effort`, frontmatter `effort:`; `func validateEfforts(cfg core.Config) error` in `internal/app`

- [ ] **Step 1: Write the failing tests**

Append to `internal/data/config/load_test.go`:
```go
func TestLoad_EffortKeys(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
default_effort = "high"

[providers.local]
type = "openai-compat"
models = ["m"]
efforts = ["low", "high"]

[agents.plan]
effort = "max"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
default_effort = "low"
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Global.DefaultEffort != "high" {
		t.Errorf("Global.DefaultEffort = %q, want high", loaded.Global.DefaultEffort)
	}
	if got := loaded.Global.Providers["local"].Efforts; !reflect.DeepEqual(got, []string{"low", "high"}) {
		t.Errorf("Providers[local].Efforts = %v, want [low high]", got)
	}
	if got := loaded.Global.Agents["plan"].Effort; got != "max" {
		t.Errorf("Agents[plan].Effort = %q, want max", got)
	}
	if got := Merge(loaded.Global, loaded.Project).DefaultEffort; got != "low" {
		t.Errorf("merged DefaultEffort = %q, want the project's low", got)
	}
}
```

Append to `internal/data/config/merge_test.go`:
```go
func TestMerge_EffortFields(t *testing.T) {
	lo := core.Config{
		DefaultEffort: "high",
		Providers:     map[string]core.ProviderConfig{"p": {Efforts: []string{"low"}}},
		Agents:        map[string]core.AgentConfig{"a": {Effort: "low"}},
	}
	hi := core.Config{
		Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{"high"}}},
		Agents:    map[string]core.AgentConfig{"a": {Effort: "max"}},
	}
	got := Merge(lo, hi)
	if got.DefaultEffort != "high" {
		t.Errorf("DefaultEffort = %q, want lo's high (hi leaves it unset)", got.DefaultEffort)
	}
	if !reflect.DeepEqual(got.Providers["p"].Efforts, []string{"high"}) {
		t.Errorf("Providers[p].Efforts = %v, want [high]", got.Providers["p"].Efforts)
	}
	if got.Agents["a"].Effort != "max" {
		t.Errorf("Agents[a].Effort = %q, want max", got.Agents["a"].Effort)
	}
}
```

Append to `internal/data/agentfs/agentfs_test.go`:
```go
func TestAgentDiscover_Effort(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "thinker", "---\ndescription: thinks\neffort: high\n---\nBody\n")
	agents, warns := Discover([]string{dir})
	if len(warns) != 0 {
		t.Fatalf("warnings = %v", warns)
	}
	if got := agents["thinker"].Effort; got != "high" {
		t.Errorf("Effort = %q, want high", got)
	}
}
```

Create (or append to) `internal/app/env_test.go`:
```go
package app

import (
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestValidateEfforts(t *testing.T) {
	tests := []struct {
		name    string
		cfg     core.Config
		wantErr string
	}{
		{"empty", core.Config{}, ""},
		{"valid", core.Config{DefaultEffort: "High", Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{"low", "max"}}}}, ""},
		{"bad default", core.Config{DefaultEffort: "turbo"}, "config: default_effort"},
		{"bad provider level", core.Config{Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{"low", "turbo"}}}}, `config: providers.p.efforts: unknown effort "turbo"`},
		{"empty provider level", core.Config{Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{""}}}}, "config: providers.p.efforts"},
	}
	for _, tt := range tests {
		err := validateEfforts(tt.cfg)
		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want nil", tt.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want it to contain %q", tt.name, err, tt.wantErr)
		}
	}
}
```
(If `internal/app` already has an `env_test.go` with a different package clause or imports, merge into it instead.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/data/config/ ./internal/data/agentfs/ ./internal/app/ -run 'Effort' -v`
Expected: FAIL (fields empty; `undefined: validateEfforts`).

- [ ] **Step 3: Implement**

`internal/data/config/dto.go`: in `tomlFile` after `SmallModel`: ``DefaultEffort string `toml:"default_effort"` ``; in `providerDTO` after `ImageModels`: ``Efforts []string `toml:"efforts"` ``; in `agentDTO` after `Model`: ``Effort string `toml:"effort"` ``.

`internal/data/config/merge.go`:
- `applyScalars`, after the `small_model` block:
```go
	if md.IsDefined("default_effort") {
		s.cfg.DefaultEffort = dto.DefaultEffort
	}
```
- `mergeProviderFields`, after `image_models`:
```go
	if md.IsDefined("providers", name, "efforts") {
		dst.Efforts = p.Efforts
	}
```
- `mergeAgentFields`, after the `model` block:
```go
	if md.IsDefined("agents", name, "effort") {
		dst.Effort = a.Effort
		touched = true
	}
```
- `mergeConfigScalars`, after `SmallModel`:
```go
	if hi.DefaultEffort != "" {
		dst.DefaultEffort = hi.DefaultEffort
	}
```
- `mergeProviderConfigFields`, after `ImageModels`:
```go
	if hi.Efforts != nil {
		dst.Efforts = hi.Efforts
	}
```
- `mergeAgentConfigFields`, after `Model`:
```go
	if hi.Effort != "" {
		dst.Effort = hi.Effort
		touched = true
	}
```

`internal/data/agentfs/agentfs.go`: in `agentMeta` after `Model`: ``Effort string `yaml:"effort"` ``; in `readAgent`'s literal after `Model: meta.Model,`: `Effort: meta.Effort,`.

`internal/app/env.go`: in `loadEnv`, right after the `validateModels` check:
```go
	if err := validateEfforts(e.merged); err != nil {
		return env{}, configError{err}
	}
```
and after `validateModels`:
```go
// validateEfforts checks default_effort and, in provider-ID order, every
// level in a provider's efforts list against the effort scale.
func validateEfforts(cfg core.Config) error {
	if _, err := core.ParseEffort(cfg.DefaultEffort); err != nil {
		return fmt.Errorf("config: default_effort: %w", err)
	}
	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, s := range cfg.Providers[id].Efforts {
			if e, err := core.ParseEffort(s); err != nil || e == "" {
				return fmt.Errorf("config: providers.%s.efforts: unknown effort %q", id, s)
			}
		}
	}
	return nil
}
```

`docs/config.example.toml`: after the `small_model` line add:
```toml

# The reasoning effort requested when neither the session, the agent,
# nor --effort picks one: none, minimal, low, medium, high, xhigh, or
# max. It is clamped to the nearest level the model supports; models
# without effort levels ignore it. Unset, each model uses its catalog
# default ("high" for most recent Claude models, but "medium" for
# claude-opus-5-5), which may differ from the provider's own default.
default_effort = "high"
```
After the `[providers.<id>.options]` comment add:
```toml

# A provider jig's catalog doesn't know (a local openai-compatible
# server, say) can list the effort levels its models accept:
#   [providers.local]
#   type = "openai-compat"
#   base_url = "http://localhost:8000/v1"
#   models = ["qwen3-32b"]
#   efforts = ["low", "medium", "high"]
```
Under `[agents.plan]`, after `model = "opus"`:
```toml
# ...and let it think harder than the default.
effort = "max"
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/data/... ./internal/app/...`
Expected: PASS (including `TestLoad_ExampleConfigParses`).

- [ ] **Step 5: Commit**

```bash
git add internal/data internal/app/env.go internal/app/env_test.go docs/config.example.toml
git commit -m "feat(config): default_effort, agent effort, provider efforts"
```

---

### Task 4: Agents validate effort and resolve the requested level

**Files:**
- Modify: `internal/service/agents/merge.go`, `internal/service/agents/resolve.go`
- Test: `internal/service/agents/merge_test.go`, `internal/service/agents/resolve_test.go`

**Interfaces:**
- Consumes: `AgentConfig.Effort`, `Agent.Effort`, `Config.DefaultEffort`, `core.ParseEffort`, `Effort.Known` (Task 1)
- Produces: `func (s *Service) ResolveEffort(a core.Agent, session core.Effort) core.Effort`

- [ ] **Step 1: Write the failing tests**

Append to `internal/service/agents/merge_test.go`:
```go
func TestMerge_Effort(t *testing.T) {
	svc, err := New(core.Config{}, Sources{GlobalTOML: map[string]core.AgentConfig{"plan": {Effort: "High"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.Get("plan"); a.Effort != core.EffortHigh {
		t.Errorf("plan.Effort = %q, want high", a.Effort)
	}

	_, err = New(core.Config{}, Sources{ProjectMD: map[string]core.AgentConfig{"custom": {Effort: "turbo"}}})
	if err == nil || !strings.HasPrefix(err.Error(), `agent "custom": unknown effort "turbo"`) {
		t.Errorf("err = %v, want an unknown-effort error naming the agent", err)
	}
}
```

Append to `internal/service/agents/resolve_test.go`:
```go
func TestResolveEffort(t *testing.T) {
	svc, err := New(core.Config{DefaultEffort: "medium"}, Sources{})
	if err != nil {
		t.Fatal(err)
	}
	none, err := New(core.Config{}, Sources{})
	if err != nil {
		t.Fatal(err)
	}
	withEffort := core.Agent{Name: "a", Effort: core.EffortLow}
	tests := []struct {
		name    string
		svc     *Service
		agent   core.Agent
		session core.Effort
		want    core.Effort
	}{
		{"session wins", svc, withEffort, core.EffortMax, core.EffortMax},
		{"agent next", svc, withEffort, "", core.EffortLow},
		{"default_effort last", svc, core.Agent{Name: "a"}, "", core.EffortMedium},
		{"nothing set", none, core.Agent{Name: "a"}, "", ""},
		{"unknown session value skipped", svc, withEffort, "turbo", core.EffortLow},
	}
	for _, tt := range tests {
		if got := tt.svc.ResolveEffort(tt.agent, tt.session); got != tt.want {
			t.Errorf("%s: ResolveEffort = %q, want %q", tt.name, got, tt.want)
		}
	}
}
```
Add `"strings"` to `merge_test.go` imports if missing.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/agents/ -run 'Effort' -v`
Expected: FAIL (`undefined: ResolveEffort`; `plan.Effort = ""`).

- [ ] **Step 3: Implement**

`internal/service/agents/merge.go`, in `mergeAgent` after the `ac.Model` block:
```go
	if ac.Effort != "" {
		e, err := core.ParseEffort(ac.Effort)
		if err != nil {
			return core.Agent{}, fmt.Errorf("agent %q: %w", base.Name, err)
		}
		out.Effort = e
	}
```
Update `New`'s doc comment: "…an unparsable model reference, an unknown effort, or a model alias…".

`internal/service/agents/resolve.go`, after `ResolveModel`:
```go
// ResolveEffort returns the requested reasoning effort for a run of a:
// the first level on the scale among session (the session's or
// --effort's choice; pass "" for a subagent), a's own Effort, and
// cfg.DefaultEffort, or "" (the model's catalog default) when none is.
// A value off the scale, such as a hand-edited stored one, is skipped.
func (s *Service) ResolveEffort(a core.Agent, session core.Effort) core.Effort {
	def, _ := core.ParseEffort(s.cfg.DefaultEffort)
	for _, e := range []core.Effort{session, a.Effort, def} {
		if e.Known() {
			return e
		}
	}
	return ""
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/service/agents/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/agents
git commit -m "feat(agents): agent effort and ResolveEffort precedence"
```

---

### Task 5: Persist session effort; `SetEffort`; small-model lowest effort

**Files:**
- Create: `internal/data/store/migrations/0002_session_effort.sql`
- Modify: `internal/data/store/sessions.go`, `internal/core/ports.go` (`SessionService`), `internal/service/session/session.go`, `internal/service/agent/complete.go`, `internal/ui/apptest_test.go` (`recSessions`), `internal/ui/details_test.go` (`fakeSessions`)
- Test: `internal/data/store/sessions_test.go`, `internal/service/session/ports_test.go`, `internal/service/session/compact_test.go`, `internal/service/session/session_test.go` (`fakeLLMs`), `internal/service/agent/complete_test.go`

**Interfaces:**
- Consumes: `Session.Effort`, `core.LowestEffort`, `core.ParseEffort` (Task 1)
- Produces:
  - `SessionService.SetEffort(ctx context.Context, id SessionID, effort Effort) error` ("" clears; publishes `event.SessionUpdated`)
  - `func Complete(ctx context.Context, m core.LLM, system, user string, effort core.Effort) (string, error)` in `service/agent`
  - UI test fakes: `effortCall{ID core.SessionID; Effort core.Effort}` and `recSessions.efforts []effortCall`

- [ ] **Step 1: Write the failing tests**

Append to `internal/data/store/sessions_test.go`:
```go
func TestSessions_EffortRoundTrips(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	sess := core.Session{ID: "ses_e", Effort: core.EffortHigh, CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1)}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, "ses_e")
	if err != nil || got.Effort != core.EffortHigh {
		t.Fatalf("GetSession = %+v, %v; want effort high", got, err)
	}
	sess.Effort = core.EffortLow
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessions(ctx, "", 0)
	if err != nil || len(list) != 1 || list[0].Effort != core.EffortLow {
		t.Fatalf("ListSessions = %+v, %v; want one session with effort low", list, err)
	}
}
```

Append to `internal/service/session/ports_test.go`:
```go
func TestSetEffort_SetsClearsAndPublishes(t *testing.T) {
	f := newFixture(t, defaultCfg())
	sess := f.create("build")
	ctx := context.Background()

	if err := f.svc.SetEffort(ctx, sess.ID, "turbo"); err == nil {
		t.Error("SetEffort(turbo): want error")
	}
	if err := f.svc.SetEffort(ctx, sess.ID, core.EffortHigh); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Get(ctx, sess.ID); got.Effort != core.EffortHigh {
		t.Errorf("Effort = %q, want high", got.Effort)
	}
	evs := f.rec.all()
	su, ok := evs[len(evs)-1].(event.SessionUpdated)
	if !ok || su.Info.Effort != core.EffortHigh || su.RootID != sess.ID {
		t.Errorf("last event = %#v, want SessionUpdated with effort high", evs[len(evs)-1])
	}
	if err := f.svc.SetEffort(ctx, sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Get(ctx, sess.ID); got.Effort != "" {
		t.Errorf("after clear Effort = %q, want empty", got.Effort)
	}
}
```

In `internal/service/session/session_test.go`, give `fakeLLMs` an `infos map[core.ModelRef]core.ModelInfo` field and make `For` return it:
```go
	info := f.infos[ref]
	info.Ref = ref
	return c, info, nil
```
(replacing `return c, core.ModelInfo{Ref: ref}, nil`; a nil map read is fine).

Append to `internal/service/session/compact_test.go`:
```go
func TestCompact_SendsSmallModelsLowestEffort(t *testing.T) {
	f := newFixture(t, defaultCfg())
	small := llmtest.New(llmtest.Text("the summary"))
	f.llms.clients[smallModel] = small
	f.llms.clients[mainModel] = llmtest.New()
	f.llms.infos = map[core.ModelRef]core.ModelInfo{
		smallModel: {Efforts: []core.Effort{core.EffortMedium, core.EffortLow, core.EffortHigh}, DefaultEffort: core.EffortHigh},
	}
	sess := f.create("build")
	f.save(sess.ID, core.RoleUser, text("fix the bug"))
	f.save(sess.ID, core.RoleAssistant, text("looking"))

	if err := f.svc.Compact(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if reqs := small.Requests(); len(reqs) != 1 || reqs[0].Effort != core.EffortLow {
		t.Errorf("small-model requests = %+v, want one with effort low", reqs)
	}
}
```

In `internal/service/agent/complete_test.go`, change the two existing calls to pass a trailing `""` (`Complete(context.Background(), llm, "be brief", "say hi", "")` and `Complete(context.Background(), llm, "s", "u", "")`), and append:
```go
func TestComplete_SendsEffort(t *testing.T) {
	llm := llmtest.New(llmtest.Text("ok"))
	if _, err := Complete(context.Background(), llm, "s", "u", core.EffortLow); err != nil {
		t.Fatal(err)
	}
	if reqs := llm.Requests(); len(reqs) != 1 || reqs[0].Effort != core.EffortLow {
		t.Errorf("requests = %+v, want one with effort low", reqs)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/data/store/ ./internal/service/session/ ./internal/service/agent/ -run 'Effort' -v`
Expected: FAIL to compile (`SetEffort` undefined, `Complete` arity) / effort not stored.

- [ ] **Step 3: Implement**

`internal/data/store/migrations/0002_session_effort.sql`:
```sql
ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT '';
```

`internal/data/store/sessions.go`:
- `CreateSession`: columns `(id, parent_id, title, agent, model, effort, cwd, created_at, updated_at)`, `VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, args add `string(sess.Effort)` after `sess.Model`.
- `UpdateSession`: `SET title = ?, agent = ?, model = ?, effort = ?, updated_at = ?`, args add `string(sess.Effort)` after `sess.Model`; doc comment "updates title, agent, model, effort, and updated_at".
- `GetSession`, `ListSessions`, `ListRootsByCwd`: select `id, parent_id, title, agent, model, effort, cwd, created_at, updated_at`.
- `scanSessionRow`:
```go
	var id, parentID, effort string
	var created, updated int64
	if err := row.Scan(&id, &parentID, &sess.Title, &sess.Agent, &sess.Model, &effort, &sess.Cwd, &created, &updated); err != nil {
		return core.Session{}, err
	}
	sess.Effort = core.Effort(effort)
```

`internal/core/ports.go`, in `SessionService` after `Configure`:
```go
	// SetEffort sets id's reasoning effort; "" clears it back to the
	// agent's, default_effort, or the model's default.
	SetEffort(ctx context.Context, id SessionID, effort Effort) error
```

`internal/service/session/session.go`, after `Configure`:
```go
// SetEffort stores effort (a level on the scale, or "" to clear) as id's
// reasoning effort and publishes event.SessionUpdated after the save.
func (s *Service) SetEffort(ctx context.Context, id core.SessionID, effort core.Effort) error {
	if effort != "" && !effort.Known() {
		return fmt.Errorf("session: unknown effort %q", effort)
	}
	var saved core.Session
	if err := s.modify(ctx, id, func(sess *core.Session) {
		sess.Effort = effort
		saved = *sess
	}); err != nil {
		return err
	}
	s.d.Bus.Publish(event.SessionUpdated{Base: event.Base{SessionID: id, RootID: id}, Info: saved})
	return nil
}
```
Update `Update`'s doc comment to "stores sess's title, agent, model, effort, and UpdatedAt as given."
In `complete`, replace `llm, _, err := s.d.LLMs.For(model)` with `llm, info, err := s.d.LLMs.For(model)` and the call with `agent.Complete(ctx, llm, a.Prompt, user, core.LowestEffort(info))`; doc comment: "…on sess's small model at its lowest effort level…".

`internal/service/agent/complete.go`: signature `func Complete(ctx context.Context, m core.LLM, system, user string, effort core.Effort) (string, error)`, add `Effort: effort,` to the `LLMRequest` literal, and add to the doc comment: "effort is sent as the request's Effort (\"\" for none)."

UI test fakes (the interface grew): in `internal/ui/apptest_test.go` add
```go
// effortCall is one recorded SessionService.SetEffort call.
type effortCall struct {
	ID     core.SessionID
	Effort core.Effort
}
```
a field `efforts []effortCall` on `recSessions` (update its comment: "recording Configure, SetEffort, and Rename calls"), and
```go
func (f *recSessions) SetEffort(_ context.Context, id core.SessionID, e core.Effort) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.efforts = append(f.efforts, effortCall{ID: id, Effort: e})
	return nil
}
```
In `internal/ui/details_test.go` add `func (f fakeSessions) SetEffort(context.Context, core.SessionID, core.Effort) error { return nil }`.

- [ ] **Step 4: Run to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/data/store/... ./internal/service/session/... ./internal/service/agent/... ./internal/ui/... ./internal/archtest/...`
Expected: PASS. (`session.Service` now has 20 methods, the archtest maximum: do not add another.)

- [ ] **Step 5: Commit**

```bash
git add internal/data/store internal/core/ports.go internal/service/session internal/service/agent/complete.go internal/service/agent/complete_test.go internal/ui/apptest_test.go internal/ui/details_test.go
git commit -m "feat(session): persist effort, SetEffort, small model at lowest effort"
```

---

### Task 6: Runner sends the effective effort

**Files:**
- Modify: `internal/service/agent/request.go`, `internal/service/agent/runner.go`
- Test: `internal/service/agent/runner_test.go`, `internal/service/agent/log_test.go`

**Interfaces:**
- Consumes: `ext.RunContext.Effort`, `core.EffectiveEffort`, `LLMRequest.Effort` (Task 1)
- Produces: every `LLMRequest` from `Runner.Run` carries `Effort = core.EffectiveEffort(runInfo, rc.Effort)`; the `run start` log line carries `effort=` and `effort_sent=`

- [ ] **Step 1: Write the failing tests**

Append to `internal/service/agent/runner_test.go`:
```go
func TestRunner_RequestEffort(t *testing.T) {
	levels := testInfo()
	levels.Efforts = []core.Effort{core.EffortLow, core.EffortMedium, core.EffortHigh}
	levels.DefaultEffort = core.EffortMedium
	tests := []struct {
		name string
		info core.ModelInfo
		want core.Effort
		sent core.Effort
	}{
		{"unset uses catalog default", levels, "", core.EffortMedium},
		{"exact level", levels, core.EffortLow, core.EffortLow},
		{"clamped to the model", levels, core.EffortMax, core.EffortHigh},
		{"model without levels sends none", testInfo(), core.EffortHigh, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := llmtest.New(llmtest.Text("ok"))
			f := newFixture(t, llm)
			f.deps.LLMs = fakeSource{llm: llm, info: tt.info}
			f.rc.Effort = tt.want
			if _, err := NewRunner(f.deps).Run(context.Background(), f.rc, "q"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if reqs := llm.Requests(); len(reqs) != 1 || reqs[0].Effort != tt.sent {
				t.Errorf("request effort = %+v, want %q", reqs, tt.sent)
			}
		})
	}
}
```

Append to `internal/service/agent/log_test.go`:
```go
func TestRunnerLog_RunStartEffort(t *testing.T) {
	llm := llmtest.New(llmtest.Text("ok"))
	f := newFixture(t, llm)
	info := testInfo()
	info.Efforts = []core.Effort{core.EffortLow, core.EffortHigh}
	f.deps.LLMs = fakeSource{llm: llm, info: info}
	f.rc.Effort = core.EffortMax
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	if _, err := NewRunner(f.deps).Run(context.Background(), f.rc, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantContains(t, only(t, buf, "run start"), "effort=max", "effort_sent=high")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/agent/ -run 'Effort' -v`
Expected: FAIL (request effort `""`; log line lacks `effort=`).

- [ ] **Step 3: Implement**

`internal/service/agent/request.go`, in `buildRequest`'s literal after `MaxOutputTokens`:
```go
		Effort:          core.EffectiveEffort(st.info, rc.Effort),
```

`internal/service/agent/runner.go`, replace the `run start` debug call:
```go
	debug(ctx, r.d.Log, "run", "run start", "model", rc.Model.String(), "max_steps", st.maxSteps,
		"effort", string(rc.Effort), "effort_sent", string(core.EffectiveEffort(info, rc.Effort)))
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/service/agent/...`
Expected: PASS (the existing `TestRunnerLog_TwoStepRun` still matches: it checks substrings).

- [ ] **Step 5: Commit**

```bash
git add internal/service/agent
git commit -m "feat(agent): send the clamped effective effort on every request"
```

---

### Task 7: Chat and task resolve the requested effort

**Files:**
- Modify: `internal/service/chat/chat.go`, `internal/service/task/task.go`
- Test: `internal/service/chat/chat_test.go`, `internal/service/task/task_test.go`

**Interfaces:**
- Consumes: `SendRequest.Effort`, `Session.Effort`, `ext.RunContext.Effort`, `core.ParseEffort` (Task 1); `(*agents.Service).ResolveEffort` (Task 4); the Runner's effective effort (Task 6); store persistence of `Session.Effort` (Task 5)
- Produces: `chat.Agents` and `task.Agents` interfaces gain `ResolveEffort(a core.Agent, session core.Effort) core.Effort`; `--effort` stored on the session

- [ ] **Step 1: Write the failing tests**

In `internal/service/chat/chat_test.go`, give `fakeLLMs` an `infos map[core.ModelRef]core.ModelInfo` field and make `For` end with:
```go
	info := f.infos[ref]
	info.Ref = ref
	return c, info, nil
```
Then append:
```go
// withLevels gives the main model effort levels low/medium/high, default medium.
func (f *fixture) withLevels() {
	f.llms.mu.Lock()
	defer f.llms.mu.Unlock()
	f.llms.infos = map[core.ModelRef]core.ModelInfo{mainModel(): {
		Efforts:       []core.Effort{core.EffortLow, core.EffortMedium, core.EffortHigh},
		DefaultEffort: core.EffortMedium,
	}}
}

func TestSend_EffortFlagStoredAndReused(t *testing.T) {
	main := llmtest.New(llmtest.Text("one"), llmtest.Text("two"))
	f := newFixture(t, main, llmtest.New(llmtest.Text("Title")))
	f.withLevels()
	ctx := context.Background()

	res, err := f.svc.Send(ctx, core.SendRequest{Text: "hi", Effort: "Low"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.get(res.SessionID).Effort; got != core.EffortLow {
		t.Errorf("session effort = %q, want low", got)
	}
	if _, err := f.svc.Send(ctx, core.SendRequest{SessionID: res.SessionID, Text: "again"}); err != nil {
		t.Fatal(err)
	}
	reqs := main.Requests()
	if len(reqs) != 2 || reqs[0].Effort != core.EffortLow || reqs[1].Effort != core.EffortLow {
		t.Errorf("request efforts = %+v, want low twice", reqs)
	}
}

func TestSend_NoEffortUsesCatalogDefault(t *testing.T) {
	main := llmtest.New(llmtest.Text("one"))
	f := newFixture(t, main, llmtest.New(llmtest.Text("Title")))
	f.withLevels()
	if _, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if reqs := main.Requests(); len(reqs) != 1 || reqs[0].Effort != core.EffortMedium {
		t.Errorf("requests = %+v, want effort medium", reqs)
	}
}

func TestSend_BadEffortIsConfigError(t *testing.T) {
	f := newFixture(t, llmtest.New(), llmtest.New())
	_, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hi", Effort: "turbo"})
	wantConfigError(t, err)
	if n := len(f.roots()); n != 0 {
		t.Errorf("sessions = %d, want none created", n)
	}
}

func TestSend_UnknownStoredEffortIgnored(t *testing.T) {
	main := llmtest.New(llmtest.Text("one"), llmtest.Text("two"))
	f := newFixture(t, main, llmtest.New(llmtest.Text("Title")))
	f.withLevels()
	ctx := context.Background()
	res, err := f.svc.Send(ctx, core.SendRequest{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	sess := f.get(res.SessionID)
	sess.Effort = "turbo"
	if err := f.sessions.Update(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Send(ctx, core.SendRequest{SessionID: res.SessionID, Text: "again"}); err != nil {
		t.Fatalf("Send on a session with an unknown stored effort: %v", err)
	}
	if reqs := main.Requests(); len(reqs) != 2 || reqs[1].Effort != core.EffortMedium {
		t.Errorf("requests = %+v, want the second at the default medium", reqs)
	}
}
```

In `internal/service/task/task_test.go`, add to `fakeAgents` a field `defaultEffort core.Effort` and:
```go
func (f *fakeAgents) ResolveEffort(a core.Agent, session core.Effort) core.Effort {
	for _, e := range []core.Effort{session, a.Effort, f.defaultEffort} {
		if e != "" {
			return e
		}
	}
	return ""
}
```
and append:
```go
func TestTask_ChildEffortIsItsAgentsNotTheParents(t *testing.T) {
	opus := core.ModelRef{Provider: "anthropic", Model: "opus"}
	tests := []struct {
		name     string
		sub      core.Effort
		fallback core.Effort
		want     core.Effort
	}{
		{"agent effort", core.EffortLow, core.EffortMedium, core.EffortLow},
		{"default_effort", "", core.EffortMedium, core.EffortMedium},
		{"catalog default", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := exploreAgent(core.ModelRef{})
			sub.Effort = tt.sub
			agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}, defaultEffort: tt.fallback}
			runner := &fakeRunner{}
			tool := New(&fakeSessions{}, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)
			rc := ext.RunContext{SessionID: "parent1", Model: opus, Effort: core.EffortMax}
			res, err := tool.Run(context.Background(), rc, mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"}))
			if err != nil || res.IsError {
				t.Fatalf("Run = %+v, %v", res, err)
			}
			if runner.gotRC.Effort != tt.want {
				t.Errorf("child rc.Effort = %q, want %q (never the parent's max)", runner.gotRC.Effort, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/chat/ ./internal/service/task/ -run 'Effort' -v`
Expected: FAIL (efforts empty; `--effort turbo` accepted).

- [ ] **Step 3: Implement**

`internal/service/chat/chat.go`:
- `Agents` interface: add `ResolveEffort(a core.Agent, session core.Effort) core.Effort` (doc: "Agents looks up agents and resolves their models and efforts.").
- `plan` struct: add
```go
	effort     core.Effort // requested effort for the run
	effortFlag core.Effort // req.Effort, parsed; stored on the session
```
  and extend its doc: "…the agent, model, and requested effort…".
- `prepare`, right after the `LLMs.For` preflight:
```go
	sessEffort := p.sess.Effort
	if req.Effort != "" {
		e, err := core.ParseEffort(req.Effort)
		if err != nil {
			return plan{}, &ConfigError{Err: err}
		}
		p.effortFlag, sessEffort = e, e
	}
	p.effort = s.d.Agents.ResolveEffort(a, sessEffort)
```
  and extend its doc comment: "…resolves the model and the requested effort, and preflights…".
- `commit`, after the model-flag block:
```go
	if p.effortFlag != "" && p.sess.Effort != p.effortFlag {
		p.sess.Effort, dirty = p.effortFlag, true
	}
```
  doc: "stores the agent, model and effort flags, and placeholder title on it".
- `Send`: add `Effort: p.effort,` to the `ext.RunContext` literal after `Model`.

`internal/service/task/task.go`:
- `Agents` interface: add `ResolveEffort(a core.Agent, session core.Effort) core.Effort`.
- `childRC` literal: add `Effort: t.agents.ResolveEffort(sub, ""),` after `Model: model,` (a subagent never inherits the parent session's effort).
- the `task spawn` debug call: add `"effort", string(childRC.Effort)` after `"model", model.String()`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/service/... ./internal/app/...`
Expected: PASS (`*agents.Service` satisfies both interfaces; the compile-time assertions in the test files check it).

- [ ] **Step 5: Commit**

```bash
git add internal/service/chat internal/service/task
git commit -m "feat(chat,task): resolve requested effort; store --effort on the session"
```

---

### Task 8: Map effort onto each provider's options

**Files:**
- Create: `internal/client/llm/effort.go`, `internal/client/llm/effort_test.go`
- Modify: `internal/client/llm/providers.go`, `internal/client/llm/stream.go`, `internal/client/llm/anthropic_cache.go`
- Test: `internal/client/llm/providers_test.go`

**Interfaces:**
- Consumes: `LLMRequest.Effort` (Task 1)
- Produces: `adapter.prepare func(*fantasy.Call, core.LLMRequest)`; prepare hooks `anthropicPrepare`, `openaiPrepare`, `openaiCompatPrepare`, `openrouterPrepare`, `googlePrepare`

- [ ] **Step 1: Write the failing tests** — `internal/client/llm/effort_test.go`:

```go
package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/gammons/jig/internal/core"
)

// adapterOf builds the adapter the factory of type typ makes.
func adapterOf(t *testing.T, typ string) *adapter {
	t.Helper()
	for _, f := range Factories() {
		if f.Type() != typ {
			continue
		}
		c, err := f.New(core.ProviderInfo{Type: typ, Endpoint: "http://example.invalid"}, core.ProviderConfig{APIKey: "k"}, "m")
		if err != nil {
			t.Fatalf("New(%s): %v", typ, err)
		}
		a, ok := c.(*adapter)
		if !ok {
			t.Fatalf("%s: got %T, want *adapter", typ, c)
		}
		return a
	}
	t.Fatalf("no factory of type %q", typ)
	return nil
}

func prepared(t *testing.T, typ string, effort core.Effort) fantasy.Call {
	t.Helper()
	req := core.LLMRequest{
		Effort:   effort,
		System:   []string{"sys"},
		Messages: []core.Message{{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}}},
	}
	call := ToFantasy(req)
	if a := adapterOf(t, typ); a.prepare != nil {
		a.prepare(&call, req)
	}
	return call
}

func TestPrepare_SetsEffortPerProviderType(t *testing.T) {
	checks := map[string]func(t *testing.T, o fantasy.ProviderOptions){
		"anthropic": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[anthropic.Name].(*anthropic.ProviderOptions)
			if !ok || p.Effort == nil || *p.Effort != anthropic.EffortHigh {
				t.Errorf("anthropic options = %#v, want Effort high", o[anthropic.Name])
			}
		},
		"openai": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[openai.Name].(*openai.ProviderOptions)
			if !ok || p.ReasoningEffort == nil || *p.ReasoningEffort != openai.ReasoningEffortHigh {
				t.Errorf("openai options = %#v, want ReasoningEffort high", o[openai.Name])
			}
		},
		"openai-compat": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[openaicompat.Name].(*openaicompat.ProviderOptions)
			if !ok || p.ReasoningEffort == nil || *p.ReasoningEffort != openai.ReasoningEffortHigh {
				t.Errorf("openai-compat options = %#v, want ReasoningEffort high", o[openaicompat.Name])
			}
		},
		"openrouter": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[openrouter.Name].(*openrouter.ProviderOptions)
			if !ok || p.Reasoning == nil || p.Reasoning.Effort == nil || *p.Reasoning.Effort != openrouter.ReasoningEffortHigh {
				t.Errorf("openrouter options = %#v, want Reasoning.Effort high", o[openrouter.Name])
			}
		},
		"google": func(t *testing.T, o fantasy.ProviderOptions) {
			p, ok := o[google.Name].(*google.ProviderOptions)
			if !ok || p.ThinkingConfig == nil || p.ThinkingConfig.ThinkingLevel == nil || *p.ThinkingConfig.ThinkingLevel != "HIGH" ||
				p.ThinkingConfig.IncludeThoughts == nil || !*p.ThinkingConfig.IncludeThoughts {
				t.Errorf("google options = %#v, want ThinkingLevel HIGH with thoughts", o[google.Name])
			}
		},
	}
	for typ, check := range checks {
		t.Run(typ, func(t *testing.T) {
			check(t, prepared(t, typ, core.EffortHigh).ProviderOptions)
			if o := prepared(t, typ, "").ProviderOptions; len(o) != 0 {
				t.Errorf("no effort: ProviderOptions = %#v, want none", o)
			}
		})
	}
}

func TestPrepare_AnthropicKeepsCacheWithEffort(t *testing.T) {
	call := prepared(t, "anthropic", core.EffortHigh)
	if anthropic.GetCacheControl(call.Prompt[len(call.Prompt)-1].ProviderOptions) == nil {
		t.Error("last message lost its cache_control breakpoint")
	}
}

func TestAnthropicFactory_SendsEffortOnStream(t *testing.T) {
	fixture, err := os.ReadFile("testdata/anthropic/text_and_tool.sse")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	c, err := anthropicFactory{}.New(core.ProviderInfo{Type: "anthropic"}, core.ProviderConfig{APIKey: "k", BaseURL: srv.URL}, "claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}
	req := core.LLMRequest{Effort: core.EffortLow, Messages: []core.Message{{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}}}}
	for range c.Stream(context.Background(), req) {
	}
	for _, want := range []string{`"effort":"low"`, `"adaptive"`} {
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("request body lacks %s: %s", want, body)
		}
	}
}
```

In `internal/client/llm/providers_test.go`, replace `TestOpenAIFactory_DoesNotWireCacheControlHook` (openai now has a prepare hook, for effort) with:
```go
// TestOpenAIFactory_NeverAppliesCacheControl proves a non-anthropic
// factory Type never gets Anthropic's cache-control breakpoints.
func TestOpenAIFactory_NeverAppliesCacheControl(t *testing.T) {
	call := prepared(t, "openai", core.EffortHigh)
	for i, m := range call.Prompt {
		if anthropic.GetCacheControl(m.ProviderOptions) != nil {
			t.Errorf("message %d carries cache_control", i)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/client/llm/ -run 'Prepare|Effort|CacheControl' -v`
Expected: FAIL to compile (`a.prepare(&call, req)`: too many arguments).

- [ ] **Step 3: Implement**

`internal/client/llm/stream.go`: `prepare func(*fantasy.Call, core.LLMRequest)` in `adapter` (doc: "prepare, if set, mutates the fantasy.Call built from each request before it is sent, given the request, e.g. to apply Anthropic's prompt caching or a provider's effort options; …"), and in `Stream`: `a.prepare(&call, req)`.

`internal/client/llm/providers.go`: `newModel(p fantasy.Provider, model string, prepare func(*fantasy.Call, core.LLMRequest))`; pass `anthropicPrepare` (replacing `applyAnthropicCacheToCall`; keep the comment about keying on the factory's Type, now "Anthropic prompt caching and effort are wired here…"), `openaiPrepare`, `openaiCompatPrepare`, `openrouterPrepare`, `googlePrepare` in the respective factories instead of `nil`.

`internal/client/llm/anthropic_cache.go`: delete `applyAnthropicCacheToCall` (its job moves to `anthropicPrepare`); first run `grep -rn applyAnthropicCacheToCall internal/` and repoint any test that calls it to `anthropicPrepare(&call, core.LLMRequest{})`.

`internal/client/llm/effort.go`:
```go
package llm

import (
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/gammons/jig/internal/core"
)

// The prepare hooks below map req.Effort, already clamped to the
// model's levels by the Runner, onto each provider type's call options.
// With no effort they set nothing, leaving the provider's default.

// setProviderOption stores opts under name in call's provider options.
func setProviderOption(call *fantasy.Call, name string, opts fantasy.ProviderOptionsData) {
	if call.ProviderOptions == nil {
		call.ProviderOptions = fantasy.ProviderOptions{}
	}
	call.ProviderOptions[name] = opts
}

// anthropicPrepare applies prompt caching and, with an effort, adaptive
// thinking at that output effort (fantasy asks for summarized thinking
// where a model hides it by default).
func anthropicPrepare(call *fantasy.Call, req core.LLMRequest) {
	applyAnthropicCache(call.Prompt)
	if req.Effort == "" {
		return
	}
	e := anthropic.Effort(req.Effort)
	setProviderOption(call, anthropic.Name, &anthropic.ProviderOptions{Effort: &e})
}

func openaiPrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	e := openai.ReasoningEffort(req.Effort)
	setProviderOption(call, openai.Name, &openai.ProviderOptions{ReasoningEffort: &e})
}

func openaiCompatPrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	e := openai.ReasoningEffort(req.Effort)
	setProviderOption(call, openaicompat.Name, &openaicompat.ProviderOptions{ReasoningEffort: &e})
}

func openrouterPrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	e := openrouter.ReasoningEffort(req.Effort)
	setProviderOption(call, openrouter.Name, &openrouter.ProviderOptions{Reasoning: &openrouter.ReasoningOptions{Effort: &e}})
}

// googlePrepare sets Gemini's thinking level and asks for thought
// summaries, so its reasoning shows like Claude's.
func googlePrepare(call *fantasy.Call, req core.LLMRequest) {
	if req.Effort == "" {
		return
	}
	level := strings.ToUpper(string(req.Effort))
	include := true
	setProviderOption(call, google.Name, &google.ProviderOptions{
		ThinkingConfig: &google.ThinkingConfig{ThinkingLevel: &level, IncludeThoughts: &include},
	})
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/client/llm/...`
Expected: PASS. If `TestAnthropicFactory_SendsEffortOnStream` finds `"effort"` but not `"adaptive"` (or vice versa), print the body and match fantasy's actual JSON for `output_config.effort` and `thinking.type` rather than weakening the test.

- [ ] **Step 5: Commit**

```bash
git add internal/client/llm
git commit -m "feat(llm): send reasoning effort through each provider's options"
```

---

### Task 9: Trust keeps `default_effort` and lists the new keys

**Files:**
- Modify: `internal/service/trust/trust.go`, `internal/service/trust/effects.go`
- Test: `internal/service/trust/effects_test.go`, `internal/service/trust/trust_test.go`

**Interfaces:**
- Consumes: `Config.DefaultEffort`, `ProviderConfig.Efforts`, `AgentConfig.Effort` (Task 1)
- Produces: effects `default_effort → <v>`, `providers.<id>.efforts → <list>`, `agents.<name> effort → <v>`

- [ ] **Step 1: Write the failing tests**

In `internal/service/trust/effects_test.go`, `everyKindLayers`: add `DefaultEffort: "high",` to `Project`, `Efforts: []string{"low", "high"},` to the `anthropic` provider, and `Effort: "low",` to the TOML `reviewer` agent. In `TestEffects_SortedAndComplete`'s `want`, insert (keeping sort order):
- `"agents.reviewer effort → low",` after `"agents.reviewer description",`
- `"default_effort → high",` before `"default_model → anthropic/claude-opus-5-5",`
- `"providers.anthropic.efforts → low, high",` after `"providers.anthropic.base_url → https://x",`

In `internal/service/trust/trust_test.go`, `TestRestrict_KeepsAliasesAndModels`: add `DefaultEffort: "high",` to `project` (it must still come back unchanged, with nothing dropped).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/service/trust/ -v -run 'Effects_SortedAndComplete|KeepsAliasesAndModels'`
Expected: FAIL (missing effects; `DefaultEffort` dropped).

- [ ] **Step 3: Implement**

`internal/service/trust/trust.go`: in `restrictScalars`' `kept` literal add `DefaultEffort: p.DefaultEffort,`; in `Restrict`'s doc list: "DefaultModel, SmallModel, DefaultEffort, ModelAliases, and Theme are kept." and "Providers (including ImageModels and Efforts), …".

`internal/service/trust/effects.go`:
- `configEffects`, after `small_model`: `out = scalar(out, "default_effort", p.DefaultEffort)`
- `providerEffects`, after the `ImageModels` block:
```go
	if pc.Efforts != nil {
		out = append(out, Effect{Key: prefix + "efforts", Value: list(pc.Efforts)})
	}
```
- `agentEffects`, after the `a.Model` block:
```go
		if a.Effort != "" {
			add("effort", a.Effort)
		}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/service/trust/... ./internal/app/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/trust
git commit -m "feat(trust): keep default_effort; list effort keys as effects"
```

---

### Task 10: Status bar shows the effective effort

**Files:**
- Modify: `internal/bubbles/statusbar/statusbar.go`, `internal/bubbles/statusbar/view.go`, `internal/ui/status.go`, `internal/ui/session.go`, `internal/ui/app.go`, `internal/ui/mode_picker.go` (`freshSession`), `internal/ui/send.go`
- Test: `internal/bubbles/statusbar/statusbar_test.go`, `internal/ui/app_test.go`

**Interfaces:**
- Consumes: `core.EffectiveEffort`, `Session.Effort`, `Agent.Effort`, `ModelInfo.Efforts` (Task 1)
- Produces:
  - `statusbar.State.Effort string`
  - `ui.Options.DefaultEffort core.Effort`; `catalog.defaultEffort core.Effort`
  - `func newSessionState(id core.SessionID, clk clock.Clock, cat catalog) *sessionState` (signature change)
  - free functions in `internal/ui/status.go`: `lookupModel(providers []core.ProviderStatus, ref string) (core.ModelInfo, bool)`, `requestedEffort(s *sessionState) core.Effort`, `shownEffort(s *sessionState) core.Effort`, `effortControllable(s *sessionState) bool`
  - test helper `effortCatalog() fakeCatalog` in `internal/ui/app_test.go` (used by Task 11)

- [ ] **Step 1: Write the failing tests**

Append to `internal/bubbles/statusbar/statusbar_test.go`:
```go
func TestStatus_EffortFollowsModel(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetWidth(140)
	st := runningState()
	st.Effort = "high"
	m.Set(st)
	if got := xansi.Strip(m.View()); !strings.Contains(got, "coder · sonnet · high") {
		t.Errorf("view = %q, want coder · sonnet · high", got)
	}
	st.Effort = ""
	m.Set(st)
	if got := xansi.Strip(m.View()); strings.Contains(got, "sonnet ·") {
		t.Errorf("view = %q, want no effort after the model", got)
	}
}
```
(Add `"strings"` to its imports if missing.)

Append to `internal/ui/app_test.go`:
```go
// effortCatalog has a configured anthropic with sonnet (low…max, default
// high) and haiku (no effort levels).
func effortCatalog() fakeCatalog {
	return fakeCatalog{
		{Configured: true, Info: core.ProviderInfo{ID: "anthropic", Name: "Anthropic", Models: []core.ModelInfo{
			{
				Ref: core.ModelRef{Provider: "anthropic", Model: "claude-sonnet-5"}, Name: "Claude Sonnet 5", ContextWindow: 200000,
				Efforts:       []core.Effort{core.EffortLow, core.EffortMedium, core.EffortHigh, core.EffortMax},
				DefaultEffort: core.EffortHigh,
			},
			{Ref: core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5"}, Name: "Claude Haiku 4.5", ContextWindow: 200000},
		}}},
	}
}

func TestApp_StatusShowsEffectiveEffort(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(effortCatalog()), withResume(resumed(), nil, nil))
	if v := xansi.Strip(ta.view()); !strings.Contains(v, "claude-sonnet-5 · high") {
		t.Errorf("want the catalog default high after the model:\n%s", v)
	}

	withDefault := newTestApp(t, withCatalog(effortCatalog()), withResume(resumed(), nil, nil),
		func(c *testConfig) { c.opts.DefaultEffort = core.EffortXHigh })
	if v := xansi.Strip(withDefault.view()); !strings.Contains(v, "claude-sonnet-5 · high") {
		t.Errorf("default_effort xhigh must clamp to the tie's lower level, high:\n%s", v)
	}

	haiku := resumed()
	haiku.Model = "anthropic/claude-haiku-4-5"
	haiku.Effort = core.EffortMax
	noLevels := newTestApp(t, withCatalog(effortCatalog()), withResume(haiku, nil, nil))
	if v := xansi.Strip(noLevels.view()); strings.Contains(v, "claude-haiku-4-5 ·") {
		t.Errorf("a model without levels shows no effort:\n%s", v)
	}
}
```
(`xansi`/`strings` imports: add if `app_test.go` lacks them. `xhigh` sits between `high` and `max`, both listed, so the tie goes to `high`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/bubbles/statusbar/ ./internal/ui/ -run 'Effort' -v`
Expected: FAIL to compile (`st.Effort`, `c.opts.DefaultEffort` undefined).

- [ ] **Step 3: Implement**

`internal/bubbles/statusbar/statusbar.go`, `State`: replace `Agent, Model string` with
```go
	Agent, Model string
	Effort       string // reasoning effort; "" hides it
```
`internal/bubbles/statusbar/view.go`, `texts`:
```go
	if s.Agent != "" || s.Model != "" {
		t[segAgent] = s.Agent + " · " + s.Model
		if s.Effort != "" {
			t[segAgent] += " · " + s.Effort
		}
	}
```
Update the package doc's "then agent/model" to "then agent/model/effort".

`internal/ui/session.go`:
- `catalog` struct: add `defaultEffort core.Effort` and mention "the configured default model and effort" in its doc.
- `newSessionState(id core.SessionID, clk clock.Clock, cat catalog)`, literal `cat: cat` (doc: "…and the configured defaults in cat.").
- `adopt`, after the `Model` fallback:
```go
	if s.info.Effort == "" {
		s.info.Effort = prev.Effort
	}
```
  and its doc: "…the agent, model, and effort chosen before the send are kept…".

`internal/ui/app.go`: in `Options`, after `DefaultModel`, add the single line
```go
	DefaultEffort       core.Effort       // resolved default_effort, for the status bar
```
and change the `sess:` line in `New` to `sess:  newSessionState(o.Session, o.Clock, catalog{defaultModel: o.DefaultModel, defaultEffort: o.DefaultEffort}),` (net +1 line: the file must stay ≤ 500).

`internal/ui/mode_picker.go`, `freshSession`: `s := newSessionState(id, old.clk, old.cat)` and `s.info.Agent, s.info.Model, s.info.Effort = old.info.Agent, old.info.Model, old.info.Effort` (doc: "keeping old's cached catalog, agent, model, and effort").

`internal/ui/send.go`: `req.Agent, req.Model, req.Effort = a.sess.info.Agent, a.sess.info.Model, string(a.sess.info.Effort)` (doc: "…created with the chosen agent, model, and effort.").

`internal/ui/status.go`: in `status`, add `Effort: ansi.SanitizeLine(string(shownEffort(s))),` after `Model`. Replace `contextWindow`'s body with
```go
	m, _ := lookupModel(s.cat.providers, ref)
	return m.ContextWindow
```
and append (free functions: `sessionState` is at the 20-method limit):
```go
// lookupModel finds ref ("provider/model") in the cached catalog.
func lookupModel(providers []core.ProviderStatus, ref string) (core.ModelInfo, bool) {
	mr, err := core.ParseModelRef(ref)
	if err != nil {
		return core.ModelInfo{}, false
	}
	for _, p := range providers {
		if p.Info.ID != mr.Provider {
			continue
		}
		for _, m := range p.Info.Models {
			if m.Ref == mr {
				return m, true
			}
		}
	}
	return core.ModelInfo{}, false
}

// requestedEffort mirrors chat's precedence for a primary run: the
// session's choice, then the agent's effort, then default_effort.
func requestedEffort(s *sessionState) core.Effort {
	if s.info.Effort.Known() {
		return s.info.Effort
	}
	if i := s.agentIndex(); i >= 0 && s.cat.agents[i].Effort.Known() {
		return s.cat.agents[i].Effort
	}
	return s.cat.defaultEffort
}

// shownEffort is the level the next send's requests carry: the requested
// one defaulted and clamped against the current model's catalog entry.
func shownEffort(s *sessionState) core.Effort {
	m, ok := lookupModel(s.cat.providers, s.modelRef())
	if !ok {
		return ""
	}
	return core.EffectiveEffort(m, requestedEffort(s))
}

// effortControllable reports whether the current model lists effort levels.
func effortControllable(s *sessionState) bool {
	m, ok := lookupModel(s.cat.providers, s.modelRef())
	return ok && len(m.Efforts) > 0
}
```

`internal/app/tui.go` passes `DefaultEffort` in Task 12.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/bubbles/... ./internal/ui/... ./internal/archtest/...`
Expected: PASS. Existing golden frames are unchanged (their catalogs have no levels); if one changes, inspect the diff before regenerating with `JIG_UPDATE_GOLDEN=1`.

- [ ] **Step 5: Commit**

```bash
git add internal/bubbles/statusbar internal/ui
git commit -m "feat(ui): status bar shows the effective reasoning effort"
```

---

### Task 11: "Switch effort…" picker action

**Files:**
- Modify: `internal/ui/actions/actions.go`, `internal/ui/pickerlevels.go`, `internal/ui/mode_picker.go`, `internal/ui/cmds.go`
- Test: `internal/ui/actions/actions_test.go`, `internal/ui/picker_test.go`; golden `internal/ui/testdata/golden/picker_root.ansi` (regenerated)

**Interfaces:**
- Consumes: `SessionService.SetEffort`, `recSessions.efforts` (Task 5); `lookupModel`, `effortControllable`, `effortCatalog()` (Task 10)
- Produces: `actions.EffortSwitch ID = "effort.switch"`; picker level `levelEfforts = "efforts"`; `effortDefaultID = "default"`; `effortItems(m core.ModelInfo, chosen core.Effort) []picker.Item`; `setEffortCmd(ctx, p Ports, id core.SessionID, e core.Effort) tea.Cmd`; `rootItems(c, km, mode, info, effortOK bool)`

- [ ] **Step 1: Write the failing tests**

`internal/ui/actions/actions_test.go`: change `len(all) != 18` → `19` (message "want 19 (18 builtins + 1 ext)"), `len(c.All()) != 17` → `18` (message "want 18 builtins"), and add `{EffortSwitch, "Switch effort…", "Agent & model", true},` after the `ModelSwitch` row.

Append to `internal/ui/picker_test.go`:
```go
// pickEffort opens "Switch effort…" and chooses the item matching query.
func pickEffort(ta *testApp, query string) {
	ta.key("ctrl+p")
	ta.typeText("switch effort")
	ta.key("enter")
	ta.typeText(query)
	ta.key("enter")
}

func TestPicker_EffortLevelListsModelLevels(t *testing.T) {
	t.Parallel()
	sess := resumed()
	sess.Effort = core.EffortLow
	ta := newTestApp(t, withCatalog(effortCatalog()), withResume(sess, nil, nil))
	items := loadItems(ta, picker.Level{ID: levelEfforts})
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if want := []string{"default", "low", "medium", "high", "max"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if items[0].Title != "Model default (high)" || items[0].Current {
		t.Errorf("default item = %+v, want titled with the catalog default and not current", items[0])
	}
	if !itemByID(t, items, "low").Current {
		t.Error("the session's low is not marked current")
	}
}

func TestPicker_EffortSwitchSetsSessionEffort(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(effortCatalog()), withResume(resumed(), nil, nil))
	pickEffort(ta, "low")
	if ta.app.sess.info.Effort != core.EffortLow {
		t.Errorf("effort = %q, want low", ta.app.sess.info.Effort)
	}
	if want := []effortCall{{ID: "ses_r", Effort: core.EffortLow}}; !slices.Equal(ta.sessions.efforts, want) {
		t.Errorf("SetEffort calls = %+v, want %+v", ta.sessions.efforts, want)
	}
	if v := xansi.Strip(ta.view()); !strings.Contains(v, "claude-sonnet-5 · low") {
		t.Errorf("status bar lacks the new effort:\n%s", v)
	}
	pickEffort(ta, "default")
	if ta.app.sess.info.Effort != "" || ta.sessions.efforts[1].Effort != "" {
		t.Errorf("after Model default: effort = %q, calls = %+v; want cleared", ta.app.sess.info.Effort, ta.sessions.efforts)
	}
}

func TestPicker_EffortSwitchWithoutSessionSendsIt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(effortCatalog()), func(c *testConfig) { c.opts.DefaultModel = "anthropic/claude-sonnet-5" })
	pickEffort(ta, "max")
	if len(ta.sessions.efforts) != 0 {
		t.Errorf("SetEffort calls = %+v, want none without a session", ta.sessions.efforts)
	}
	ta.typeText("hi")
	ta.key("enter")
	if got := ta.chat.sends[0].Effort; got != "max" {
		t.Errorf("new session sent with effort %q, want max", got)
	}
}

func TestPicker_EffortDisabledWithoutLevels(t *testing.T) {
	t.Parallel()
	haiku := resumed()
	haiku.Model = "anthropic/claude-haiku-4-5"
	ta := newTestApp(t, withCatalog(effortCatalog()), withResume(haiku, nil, nil))
	it := itemByID(t, loadItems(ta, rootLevel()), "effort.switch")
	if !it.Disabled || it.Drill != nil {
		t.Errorf("effort.switch = %+v, want disabled for a model without levels", it)
	}
	if cmd := (pickerCtl{ta.app}).action(actions.EffortSwitch); cmd != nil {
		t.Error("action(effort.switch) returned a Cmd, want none for a model without levels")
	}
	if ta.app.w.picker.IsOpen() || !strings.Contains(ta.app.view.hint, "no effort levels") {
		t.Errorf("open=%v hint=%q; want a hint and no picker", ta.app.w.picker.IsOpen(), ta.app.view.hint)
	}
}

func TestPicker_EffortDefaultFallsBackToAgent(t *testing.T) {
	t.Parallel()
	sess := resumed()
	sess.Effort = core.EffortMax
	ta := newTestApp(t, withCatalog(effortCatalog()), withResume(sess, nil, nil),
		func(c *testConfig) { c.agents = fakeAgents{{Name: "build", Mode: core.ModePrimary, Effort: core.EffortLow}} })
	if v := xansi.Strip(ta.view()); !strings.Contains(v, "claude-sonnet-5 · max") {
		t.Errorf("the session's max should win over the agent's low:\n%s", v)
	}
	pickEffort(ta, "default")
	if v := xansi.Strip(ta.view()); !strings.Contains(v, "claude-sonnet-5 · low") {
		t.Errorf("cleared, the agent's low should show:\n%s", v)
	}
}

func TestApp_EffortSurvivesModelRoundTrip(t *testing.T) {
	t.Parallel()
	sess := resumed()
	sess.Effort = core.EffortMax
	ta := newTestApp(t, withCatalog(effortCatalog()), withResume(sess, nil, nil))
	switchModel := func(q string) {
		ta.key("ctrl+p")
		ta.typeText("switch model")
		ta.key("enter")
		ta.typeText(q)
		ta.key("enter")
	}
	switchModel("haiku")
	if v := xansi.Strip(ta.view()); strings.Contains(v, "claude-haiku-4-5 ·") {
		t.Errorf("haiku has no levels; want no effort shown:\n%s", v)
	}
	switchModel("sonnet")
	if v := xansi.Strip(ta.view()); !strings.Contains(v, "claude-sonnet-5 · max") {
		t.Errorf("back on sonnet, the session's max should show again:\n%s", v)
	}
}
```
Import `github.com/gammons/jig/internal/ui/actions` in `picker_test.go` if missing.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/ui/... -run 'Effort|Catalogue' -v`
Expected: FAIL to compile (`levelEfforts`, `actions.EffortSwitch` undefined).

- [ ] **Step 3: Implement**

`internal/ui/actions/actions.go`: const `EffortSwitch ID = "effort.switch"` after `ModelSwitch`; in `builtinActions` after the `ModelSwitch` entry: `{ID: EffortSwitch, Title: "Switch effort…", Group: "Agent & model", Drill: true},`.

`internal/ui/pickerlevels.go`:
- const `levelEfforts = "efforts"` in the level block; and
```go
// effortDefaultID is the efforts level's "Model default" item: choosing
// it clears the session's effort.
const effortDefaultID = "default"
```
- `levelAction`: `case levelEfforts: return actions.EffortSwitch`
- `drillLevel`: `case actions.EffortSwitch: return picker.Level{ID: levelEfforts, Title: "Effort"}, true`
- `levels.load`: root call becomes `rootItems(a.opts.Actions, a.opts.Keymap, keymapMode(a.view.pick.prev), a.sess.info, effortControllable(a.sess))`; add
```go
	case levelEfforts:
		m, _ := lookupModel(a.sess.cat.providers, a.sess.modelRef())
		return itemsCmd(level.ID, effortItems(m, a.sess.info.Effort))
```
- `rootItems(c *actions.Catalogue, km actions.Keymap, mode string, info core.Session, effortOK bool)`; doc: "…renaming needs a session, and switching effort a model with levels."; inside the loop after the rename check:
```go
		if act.ID == actions.EffortSwitch && !effortOK {
			it.Drill, it.Disabled = nil, true
		}
```
- after `modelDetail`:
```go
// effortItems lists "Model default (<its level>)" and then m's levels
// in catalog order; chosen (the session's stored effort, "" for none)
// is marked current.
func effortItems(m core.ModelInfo, chosen core.Effort) []picker.Item {
	title := "Model default"
	if m.DefaultEffort != "" {
		title += " (" + string(m.DefaultEffort) + ")"
	}
	out := []picker.Item{{ID: effortDefaultID, Title: title, Current: chosen == ""}}
	for _, e := range m.Efforts {
		out = append(out, picker.Item{ID: string(e), Title: string(e), Current: e == chosen})
	}
	return out
}
```
(Levels are parsed `core.Effort` scale values, never raw catalog text, so they need no sanitizing.)

`internal/ui/mode_picker.go`:
- `chosen`: `case levelEfforts: cmds = append(cmds, p.setEffort(first))`
- `action`: add to the switch
```go
	case actions.EffortSwitch:
		if !effortControllable(a.sess) {
			a.view.hint = "this model has no effort levels"
			return nil
		}
```
- after `setModel`:
```go
// setEffort makes the chosen level (none, for the Model default item)
// the effort the next send uses, and the session's.
func (p pickerCtl) setEffort(id string) tea.Cmd {
	a := p.a
	e := core.Effort(id)
	if id == effortDefaultID {
		e = ""
	}
	a.sess.info.Effort = e
	if a.sess.info.ID == "" {
		return nil
	}
	return setEffortCmd(a.ctx, a.ports, a.sess.info.ID, e)
}
```

`internal/ui/cmds.go`, after `configureCmd`:
```go
// setEffortCmd sets id's reasoning effort ("" clears it) through p.Sessions.
func setEffortCmd(ctx context.Context, p Ports, id core.SessionID, e core.Effort) tea.Cmd {
	return func() tea.Msg {
		if err := p.Sessions.SetEffort(ctx, id, e); err != nil {
			return errMsg{what: "effort", err: err}
		}
		return nil
	}
}
```

- [ ] **Step 4: Run to verify they pass, then refresh the root picker golden**

Run: `go test ./internal/ui/...`
Expected: only `picker_root` golden differs (a new "Switch effort…" row under "Agent & model"). Regenerate and inspect:
```bash
JIG_UPDATE_GOLDEN=1 go test ./internal/ui/ -run TestPicker_GoldenRoot
git diff --stat internal/ui/testdata/golden
go test ./internal/ui/... ./internal/archtest/...
```
Expected: PASS; the golden diff shows only the added row (and rows shifted below it).

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): Switch effort… picker action"
```

---

### Task 12: `--effort`, `jig models` levels, TUI default, end-to-end

**Files:**
- Create: `e2e/effort_test.go`
- Modify: `internal/app/cli.go`, `internal/app/app.go` (usage text), `internal/app/headless.go`, `internal/app/models.go`, `internal/app/tui.go`, `internal/client/llm/jigtest/jigtest.go`
- Test: `internal/app/cli_test.go`, `internal/app/models_test.go` (create if absent), `internal/client/llm/jigtest/jigtest_test.go`

**Interfaces:**
- Consumes: everything above; `ui.Options.DefaultEffort` (Task 10)
- Produces: `runOpts.effort`; `jigtest.Turn.ExpectEffort *string`; `func effortRange(m core.ModelInfo) string`; `func configuredEffort(cfg core.Config) core.Effort`

- [ ] **Step 1: Write the failing tests**

Append to `internal/app/cli_test.go`:
```go
func TestParseRun_Effort(t *testing.T) {
	o, err := parseRun([]string{"--effort", "high", "fix", "it"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.effort != "high" || o.prompt != "fix it" {
		t.Errorf("opts = %+v, want effort high and prompt %q", o, "fix it")
	}
}
```
(Add `"io"` to imports if missing.)

Create or append `internal/app/models_test.go`:
```go
package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestPrintProvider_ShowsEffortLevels(t *testing.T) {
	var buf bytes.Buffer
	printProvider(&buf, core.ProviderInfo{ID: "anthropic", Models: []core.ModelInfo{
		{Ref: core.ModelRef{Provider: "anthropic", Model: "opus"}, ContextWindow: 200000,
			Efforts: []core.Effort{core.EffortLow, core.EffortHigh, core.EffortMax}, DefaultEffort: core.EffortHigh},
		{Ref: core.ModelRef{Provider: "anthropic", Model: "haiku"}, ContextWindow: 200000},
	}}, "(configured)")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[1], "  effort low…max (high)") || strings.Contains(lines[2], "effort") {
		t.Errorf("output =\n%s\nwant opus with 'effort low…max (high)' and haiku without effort", buf.String())
	}
}
```

Append to `internal/client/llm/jigtest/jigtest_test.go`:
```go
func TestStream_ExpectEffort(t *testing.T) {
	want := "low"
	turn := Turn{Text: "ok", ExpectEffort: &want}
	l, _, _ := newLLM(t, Script{Models: map[string][]Turn{"m1": {turn, turn}}}, "m1")
	if _, err := collect(context.Background(), l, core.LLMRequest{Effort: core.EffortLow}); err != nil {
		t.Fatalf("matching effort: %v", err)
	}
	_, err := collect(context.Background(), l, core.LLMRequest{Effort: core.EffortHigh})
	assertLLMError(t, err, `effort "high", want "low"`, false)
}
```

Create `e2e/effort_test.go`:
```go
//go:build jigtest

package e2e

import (
	"fmt"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// effortConfig is jigtestConfig with effort levels on the jigtest models.
func effortConfig(script string) string {
	return fmt.Sprintf(`default_model = "jigtest/m1"
small_model = "jigtest/title"

[providers.jigtest]
type = "jigtest"
models = ["m1", "title"]
efforts = ["low", "medium", "high"]
options = { script = %q }
`, script)
}

func TestE2E_EffortReachesProvider(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{{"low", "low"}, {"Max", "high"}} {
		t.Run(tc.flag, func(t *testing.T) {
			env := newEnv(t)
			want := tc.want
			script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
				"m1": {{Text: "ok", ExpectEffort: &want}},
			}})
			writeConfig(t, env, effortConfig(script))
			stdout, stderr, code := runPrompt(t, env, "--effort", tc.flag, "hi")
			wantCode(t, code, 0, stdout, stderr)
		})
	}
}

func TestE2E_BadEffortExitsConfigError(t *testing.T) {
	env := newEnv(t)
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{"m1": {{Text: "ok"}}}})
	writeConfig(t, env, effortConfig(script))
	stdout, stderr, code := runPrompt(t, env, "--effort", "turbo", "hi")
	wantCode(t, code, 2, stdout, stderr)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/app/ ./internal/client/llm/jigtest/ -run 'Effort' -v && make build && go test -tags jigtest ./e2e/ -run Effort -v`
Expected: FAIL to compile (`o.effort`, `ExpectEffort` undefined).

- [ ] **Step 3: Implement**

`internal/app/cli.go`: `runOpts` gets `effort string` after `model`; `runUsage` becomes ``usage: jig run [--agent A] [--model M] [--effort E] [--yes] [--trust-project] [--session ID] [--cwd DIR] [--attach PATH]... <prompt...>``; after the `model` flag:
```go
	fs.StringVar(&o.effort, "effort", "", "reasoning effort: none, minimal, low, medium, high, xhigh, or max")
```
`internal/app/app.go`: in the usage text, `jig run [--agent A] [--model M] [--effort E] [--yes] …`.

`internal/app/headless.go`: add `Effort: opts.effort,` after `Model: opts.model,` in the `SendRequest`.

`internal/app/models.go`, `printProvider`:
```go
	for _, m := range p.Models {
		line := fmt.Sprintf("  %s  ctx %dk  $%.2f/$%.2f per 1M",
			m.Ref.String(), m.ContextWindow/1000, m.CostIn, m.CostOut)
		if len(m.Efforts) > 0 {
			line += "  effort " + effortRange(m)
		}
		printLine(w, line)
	}
```
and
```go
// effortRange is m's effort levels as "low…max (high)": lowest to
// highest as listed, then the catalog default when it has one.
func effortRange(m core.ModelInfo) string {
	s := string(m.Efforts[0])
	if len(m.Efforts) > 1 {
		s += "…" + string(m.Efforts[len(m.Efforts)-1])
	}
	if m.DefaultEffort != "" {
		s += " (" + string(m.DefaultEffort) + ")"
	}
	return s
}
```

`internal/app/tui.go`: in the `ui.Options` literal add `DefaultEffort: configuredEffort(e.cfg()),` (append it to the `Aliases:`/`DefaultModel:` line if the function would pass 40 lines), and after `defaultModel`:
```go
// configuredEffort is default_effort, parsed (loadEnv validated it).
func configuredEffort(cfg core.Config) core.Effort {
	e, _ := core.ParseEffort(cfg.DefaultEffort)
	return e
}
```

`internal/client/llm/jigtest/jigtest.go`: in `Turn`, after `ExpectPromptContains`:
```go
	// ExpectEffort, if set, is the exact effort the request must carry.
	ExpectEffort *string
```
and in `Stream`, after the `checkPrompt` block:
```go
		if turn.ExpectEffort != nil && string(req.Effort) != *turn.ExpectEffort {
			yield(core.StreamEvent{}, fatal(fmt.Errorf("jigtest: effort %q, want %q", req.Effort, *turn.ExpectEffort)))
			return
		}
```

- [ ] **Step 4: Run to verify they pass**

Run: `make check`
Expected: PASS, including `e2e` (`--effort Max` clamps to `high`; `turbo` exits 2).

- [ ] **Step 5: Commit**

```bash
git add internal/app internal/client/llm/jigtest e2e
git commit -m "feat(app): --effort flag, effort levels in jig models, e2e"
```

---

### Task 13: Document the invariant and shared helpers

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Edit `AGENTS.md`**

In the architecture paragraph on `core.SessionService`, change "covering listing, `Rename`, and `Configure`" to "covering listing, `Rename`, `Configure`, and `SetEffort`".

In **Invariants**, after the "Model strings go through `agents.ParseRef`…" bullet, add:
```markdown
- Reasoning effort: services put the *requested* level on
  `ext.RunContext.Effort` (primary runs: session > agent `effort` >
  `default_effort`, via `agents.Service.ResolveEffort`; subagents skip the
  session). The Runner sets `LLMRequest.Effort` to
  `core.EffectiveEffort(runModelInfo, rc.Effort)`: the catalog default when
  unset, clamped to the model's levels, "" for a model without levels.
  `client/llm` only maps that value onto provider options and never
  validates it; "" sets no option. Small-model calls send
  `core.LowestEffort`.
```

In **Shared code**, add a row:
```markdown
| Parse, clamp, or default a reasoning effort | `core.ParseEffort(s)`, `core.EffectiveEffort(info, want)`, `core.LowestEffort(info)`, `Effort.Known()` in `internal/core/effort.go` |
```

- [ ] **Step 2: Verify**

Run: `make check`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: reasoning effort invariant and helpers"
```

