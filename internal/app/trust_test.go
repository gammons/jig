package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

// writeProject writes rel (relative to env's work dir) with content.
func (e *testEnv) writeProject(t *testing.T, rel, content string) {
	t.Helper()
	path := filepath.Join(e.workDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// countingDecider answers grant and counts its calls.
type countingDecider struct {
	grant bool
	calls int
}

func (d *countingDecider) decide(trustState) (bool, error) {
	d.calls++
	return d.grant, nil
}

func mustLoadEnv(t *testing.T, env *testEnv, decide trustDecider) env {
	t.Helper()
	e, err := loadEnv(env.workDir, env.getenv, decide)
	if err != nil {
		t.Fatalf("loadEnv: %v", err)
	}
	return e
}

func TestLoadEnv_NoProjectConfigNeverAsks(t *testing.T) {
	env := newTestEnv(t)
	d := &countingDecider{grant: true}
	e := mustLoadEnv(t, env, d.decide)
	if d.calls != 0 {
		t.Errorf("decider called %d times, want 0", d.calls)
	}
	if e.trust.trusted || e.trust.hash != "" {
		t.Errorf("trust = %+v, want untrusted with empty hash", e.trust)
	}
}

func TestLoadEnv_UntrustedRestricts(t *testing.T) {
	env := newTestEnv(t)
	env.writeProject(t, ".jig/config.toml", "[permissions]\nwrite = \"allow\"\n")
	d := &countingDecider{grant: false}
	e := mustLoadEnv(t, env, d.decide)
	if d.calls != 1 {
		t.Errorf("decider called %d times, want 1", d.calls)
	}
	if e.trust.trusted {
		t.Error("trusted = true, want false")
	}
	if got := e.cfg().Permissions["write"].Default; got == core.Allow {
		t.Errorf("cfg().Permissions[write].Default = %q, want not allow", got)
	}
	if len(e.trust.dropped) == 0 {
		t.Error("dropped is empty, want the write allow")
	}
	if len(e.trust.effects) == 0 {
		t.Error("effects is empty, want the write allow")
	}
}

func TestLoadEnv_GrantPersistsUntilConfigChanges(t *testing.T) {
	env := newTestEnv(t)
	env.writeProject(t, ".jig/config.toml", "[permissions]\nwrite = \"allow\"\n")

	d := &countingDecider{grant: true}
	e := mustLoadEnv(t, env, d.decide)
	if d.calls != 1 || !e.trust.trusted || len(e.trust.dropped) != 0 {
		t.Fatalf("first load: calls %d, trust %+v; want 1 call, trusted, nothing dropped", d.calls, e.trust)
	}
	if got := e.cfg().Permissions["write"].Default; got != core.Allow {
		t.Errorf("trusted cfg().Permissions[write].Default = %q, want allow", got)
	}

	d = &countingDecider{grant: false}
	e = mustLoadEnv(t, env, d.decide)
	if d.calls != 0 || !e.trust.trusted {
		t.Fatalf("second load: calls %d, trusted %v; want 0 calls, trusted", d.calls, e.trust.trusted)
	}

	env.writeProject(t, ".jig/config.toml", "[permissions]\nwrite = \"allow\"\n# edited\n")
	e = mustLoadEnv(t, env, d.decide)
	if d.calls != 1 || e.trust.trusted {
		t.Errorf("after edit: calls %d, trusted %v; want 1 call, untrusted", d.calls, e.trust.trusted)
	}
}

func TestLoadEnv_ProjectAgentFileIsHashed(t *testing.T) {
	env := newTestEnv(t)
	env.writeProject(t, ".claude/agents/x.md", "---\ndescription: one\n---\nbody\n")
	d := &countingDecider{}
	first := mustLoadEnv(t, env, d.decide).trust.hash
	if first == "" {
		t.Fatal("hash is empty with a project agent file")
	}
	env.writeProject(t, ".claude/agents/x.md", "---\ndescription: two\n---\nbody\n")
	if second := mustLoadEnv(t, env, d.decide).trust.hash; second == first {
		t.Error("hash unchanged after editing .claude/agents/x.md")
	}
}

func TestLoadEnv_UntrustedDoesNotExpandProjectEnv(t *testing.T) {
	env := newTestEnv(t)
	env.vars["SECRET"] = "s3cr3t-value"
	env.writeProject(t, ".jig/config.toml", "[agents.build]\nprompt = \"{env:SECRET}\"\n")
	env.writeProject(t, ".jig/agents/y.md", "---\ndescription: y\n---\n{env:SECRET}\n")
	e := mustLoadEnv(t, env, (&countingDecider{}).decide)

	if got := e.cfg().Agents["build"].Prompt; got != "{env:SECRET}" {
		t.Errorf("cfg().Agents[build].Prompt = %q, want the literal token", got)
	}
	for _, m := range []map[string]core.AgentConfig{e.cfg().Agents, e.layers.Project.Agents, e.layers.ProjectMD} {
		for name, ac := range m {
			if strings.Contains(ac.Prompt, "s3cr3t-value") {
				t.Errorf("agent %s prompt contains the secret: %q", name, ac.Prompt)
			}
		}
	}

	trusted := mustLoadEnv(t, env, (&countingDecider{grant: true}).decide)
	if got := trusted.cfg().Agents["build"].Prompt; got != "s3cr3t-value" {
		t.Errorf("trusted cfg().Agents[build].Prompt = %q, want the substituted value", got)
	}
}

func TestLoadEnv_SourcesComeFromRestrictedLayers(t *testing.T) {
	env := newTestEnv(t)
	env.writeProject(t, ".jig/config.toml", "[agents.build.permissions]\nwrite = \"allow\"\n")
	env.writeProject(t, ".jig/agents/y.md", "---\ndescription: y\npermissions:\n  bash: allow\n---\nbody\n")
	e := mustLoadEnv(t, env, (&countingDecider{}).decide)
	src := discover(e, &strings.Builder{}).sources
	if got := src.ProjectTOML["build"].Permissions["write"].Default; got == core.Allow {
		t.Errorf("ProjectTOML build write = %q, want the allow dropped", got)
	}
	if got := src.ProjectMD["y"].Permissions["bash"].Default; got == core.Allow {
		t.Errorf("ProjectMD y bash = %q, want the allow dropped", got)
	}
}
