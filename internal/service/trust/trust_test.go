package trust

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/permission"
)

func pats(kv ...string) map[string]core.Action {
	m := make(map[string]core.Action, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = core.Action(kv[i+1])
	}
	return m
}

func TestRestrict_DropsProvidersInstructionsSkillsIntegration(t *testing.T) {
	l := Layers{Project: core.Config{
		DefaultModel: "a/b",
		Providers: map[string]core.ProviderConfig{"evil": {
			BaseURL: "https://evil.example", APIKey: "k", ImageModels: []string{"m"},
		}},
		Instructions: []string{"/p/docs/rules.md"},
		SkillPaths:   []string{"/p/skills"},
		AgentBrowser: core.ToggleTrue,
	}}
	got, dropped := Restrict(l)

	want := core.Config{DefaultModel: "a/b"}
	if !reflect.DeepEqual(got.Project, want) {
		t.Errorf("Project = %#v, want %#v", got.Project, want)
	}
	wantDropped := []string{
		"instructions += /p/docs/rules.md",
		"integrations.agent_browser.enabled → true",
		"providers.evil.api_key → (set)",
		"providers.evil.base_url → https://evil.example",
		"providers.evil.image_models → m",
		"skills.paths += /p/skills",
	}
	if gotDropped := effectStrings(dropped); !reflect.DeepEqual(gotDropped, wantDropped) {
		t.Errorf("dropped =\n%s\nwant\n%s", strings.Join(gotDropped, "\n"), strings.Join(wantDropped, "\n"))
	}
}

func TestRestrict_KeepsAliasesAndModels(t *testing.T) {
	project := core.Config{
		DefaultModel: "a/big",
		SmallModel:   "a/small",
		ModelAliases: map[string]string{"fast": "a/small"},
		Theme:        "dark",
		Permissions:  core.PermissionRules{"bash": {Default: core.Deny}},
	}
	got, dropped := Restrict(Layers{Project: project})
	if !reflect.DeepEqual(got.Project, project) {
		t.Errorf("Project = %#v, want %#v", got.Project, project)
	}
	if dropped != nil {
		t.Errorf("dropped = %v, want nil", effectStrings(dropped))
	}
}

// An untrusted project's [keybinds] could remap keys the user relies on
// (the permission card's, run.cancel, app.quit), so Restrict drops them
// all and lists them as dropped.
func TestRestrict_DropsKeybinds(t *testing.T) {
	project := core.Config{Keybinds: map[string]string{"normal.x": "app.quit", "insert.ctrl+g": "run.cancel"}}
	got, dropped := Restrict(Layers{Project: project})
	if got.Project.Keybinds != nil {
		t.Errorf("Keybinds = %v, want nil", got.Project.Keybinds)
	}
	want := []string{`keybinds."insert.ctrl+g" → run.cancel`, `keybinds.normal.x → app.quit`}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, want) {
		t.Errorf("dropped = %q, want %q", g, want)
	}
}

func TestRestrict_TopLevelPermissions(t *testing.T) {
	l := Layers{
		Global: core.Config{Permissions: core.PermissionRules{"bash": {Patterns: pats("*", "allow", "git push*", "deny")}}},
		Project: core.Config{Permissions: core.PermissionRules{
			"bash":  {Default: core.Deny, Patterns: pats("gi* push --force", "ask", "curl *", "deny", "rm *", "allow")},
			"read":  {Default: core.Ask},
			"write": {Default: core.Allow},
		}},
	}
	got, dropped := Restrict(l)
	want := core.PermissionRules{
		"bash": {Default: core.Deny, Patterns: pats("curl *", "deny")},
		"read": {Default: core.Ask},
	}
	if !reflect.DeepEqual(got.Project.Permissions, want) {
		t.Errorf("Permissions = %v, want %v", got.Project.Permissions, want)
	}
	wantDropped := []string{
		`permissions.bash "gi* push --force" → ask`,
		`permissions.bash "rm *" → allow`,
		"permissions.write → allow",
	}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, wantDropped) {
		t.Errorf("dropped = %q, want %q", g, wantDropped)
	}
}

// A top-level project rule sits under every agent's own rules, so an ask
// pattern there could out-specify an agent's deny (or override an agent's
// deny default). It is kept only if no global agent denies that tool.
func TestRestrict_TopLevelAskCannotUndercutAgentDeny(t *testing.T) {
	tests := []struct {
		name string
		l    Layers
	}{
		{"md agent deny pattern", Layers{
			Global:   core.Config{Permissions: core.PermissionRules{"bash": {Patterns: pats("*", "allow")}}},
			GlobalMD: map[string]core.AgentConfig{"x": {Permissions: core.PermissionRules{"bash": {Patterns: pats("git push*", "deny")}}}},
		}},
		{"toml agent deny default", Layers{
			Global: core.Config{Agents: map[string]core.AgentConfig{"build": {Permissions: core.PermissionRules{"bash": {Default: core.Deny}}}}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.l.Project = core.Config{Permissions: core.PermissionRules{"bash": {Patterns: pats("gi* push --force", "ask", "*", "ask")}}}
			got, dropped := Restrict(tt.l)
			if got.Project.Permissions != nil {
				t.Errorf("Permissions = %v, want nil", got.Project.Permissions)
			}
			if len(dropped) != 2 {
				t.Errorf("dropped = %v, want both ask patterns", effectStrings(dropped))
			}
		})
	}
}

func TestRestrict_ProjectAgentCannotLoosenBuiltin(t *testing.T) {
	l := Layers{ProjectMD: map[string]core.AgentConfig{"plan": {
		Description: "Project plan.",
		Prompt:      "Plan things.",
		Permissions: core.PermissionRules{
			"write": {Default: core.Allow},
			"bash":  {Default: core.Deny},
		},
		Source: "/p/.claude/agents/plan.md",
	}}}
	got, dropped := Restrict(l)
	want := core.AgentConfig{
		Description: "Project plan.",
		Prompt:      "Plan things.",
		Permissions: core.PermissionRules{"bash": {Default: core.Deny}},
		Source:      "/p/.claude/agents/plan.md",
	}
	if !reflect.DeepEqual(got.ProjectMD["plan"], want) {
		t.Errorf("ProjectMD[plan] = %#v, want %#v", got.ProjectMD["plan"], want)
	}
	wantDropped := []string{"agents.plan (from /p/.claude/agents/plan.md) permissions.write → allow"}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, wantDropped) {
		t.Errorf("dropped = %q, want %q", g, wantDropped)
	}
}

func TestRestrict_GlobalAgentLayersFormTheBaseline(t *testing.T) {
	// The user's own reviewer denies bash; the project's ask on it would
	// weaken that deny, so it is dropped. The builtin plan's ask default
	// is raised to deny by GlobalMD, so a project ask default is dropped.
	l := Layers{
		Global: core.Config{Agents: map[string]core.AgentConfig{
			"reviewer": {Permissions: core.PermissionRules{"bash": {Default: core.Deny}}},
		}},
		GlobalMD: map[string]core.AgentConfig{"plan": {Permissions: core.PermissionRules{"edit": {Default: core.Deny}}}},
		Project: core.Config{Agents: map[string]core.AgentConfig{
			"reviewer": {Permissions: core.PermissionRules{"bash": {Patterns: pats("ls", "ask")}}},
			"plan":     {Permissions: core.PermissionRules{"edit": {Default: core.Ask}}},
		}},
	}
	got, dropped := Restrict(l)
	for name, a := range got.Project.Agents {
		if a.Permissions != nil {
			t.Errorf("agent %s Permissions = %v, want nil", name, a.Permissions)
		}
	}
	want := []string{"agents.plan permissions.edit → ask", `agents.reviewer permissions.bash "ls" → ask`}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, want) {
		t.Errorf("dropped = %q, want %q", g, want)
	}
}

func TestRestrict_NewProjectAgentBoundedByConfig(t *testing.T) {
	l := Layers{Project: core.Config{Agents: map[string]core.AgentConfig{"rev": {
		Mode:        "subagent",
		Tools:       []string{},
		Permissions: core.PermissionRules{"edit": {Default: core.Allow}},
	}}}}
	got, dropped := Restrict(l)
	want := core.AgentConfig{Mode: "subagent", Tools: []string{}}
	if !reflect.DeepEqual(got.Project.Agents["rev"], want) {
		t.Errorf("Agents[rev] = %#v, want %#v", got.Project.Agents["rev"], want)
	}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, []string{"agents.rev permissions.edit → allow"}) {
		t.Errorf("dropped = %q", g)
	}
}

func TestRestrict_ProjectAgentBaselineIncludesKeptTopLevel(t *testing.T) {
	l := Layers{Project: core.Config{
		Permissions: core.PermissionRules{"bash": {Default: core.Deny}},
		Agents:      map[string]core.AgentConfig{"rev": {Permissions: core.PermissionRules{"bash": {Default: core.Ask}}}},
	}}
	got, dropped := Restrict(l)
	if got.Project.Agents["rev"].Permissions != nil {
		t.Errorf("rev Permissions = %v, want nil", got.Project.Agents["rev"].Permissions)
	}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, []string{"agents.rev permissions.bash → ask"}) {
		t.Errorf("dropped = %q", g)
	}
}

func TestRestrict_TOMLThenMDLayering(t *testing.T) {
	l := Layers{
		Project: core.Config{Agents: map[string]core.AgentConfig{"rev": {
			Permissions: core.PermissionRules{"bash": {Default: core.Deny}},
		}}},
		ProjectMD: map[string]core.AgentConfig{"rev": {
			Permissions: core.PermissionRules{"bash": {Default: core.Ask}},
			Source:      "/p/.jig/agents/rev.md",
		}},
	}
	got, dropped := Restrict(l)
	if p := got.Project.Agents["rev"].Permissions; !reflect.DeepEqual(p, core.PermissionRules{"bash": {Default: core.Deny}}) {
		t.Errorf("TOML rev Permissions = %v", p)
	}
	if p := got.ProjectMD["rev"].Permissions; p != nil {
		t.Errorf("MD rev Permissions = %v, want nil", p)
	}
	want := []string{"agents.rev (from /p/.jig/agents/rev.md) permissions.bash → ask"}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, want) {
		t.Errorf("dropped = %q, want %q", g, want)
	}
}

// mutationFixture builds the same Layers on every call, so one copy can be
// passed to Restrict and another kept as the pristine expectation.
func mutationFixture() Layers {
	l := everyKindLayers()
	l.Global = core.Config{
		Permissions:  core.PermissionRules{"bash": {Patterns: pats("*", "allow")}},
		ModelAliases: map[string]string{"g": "a/g"},
		Agents: map[string]core.AgentConfig{"reviewer": {
			Tools: []string{"read"}, Permissions: core.PermissionRules{"edit": {Patterns: pats("*.md", "allow")}},
		}},
	}
	l.GlobalMD = map[string]core.AgentConfig{"plan": {Permissions: core.PermissionRules{"bash": {Patterns: pats("ls", "allow")}}}}
	l.Project.Permissions["bash"].Patterns["ls"] = core.Ask
	disabledFalse := false
	l.Project.MCP = core.MCPConfig{
		Servers: map[string]core.MCPServer{
			"stdio-srv": {
				Name: "stdio-srv", Source: "/p/.mcp.json", Transport: core.MCPStdio,
				Command: "npx", Args: []string{"-y", "pkg"},
				Env: map[string]string{"DEBUG": "1"},
			},
			"http-srv": {
				Name: "http-srv", Source: "/p/.jig/config.toml", Transport: core.MCPHTTP,
				URL:     "https://example.com/mcp",
				Headers: map[string]string{"Authorization": "Bearer tok"},
				OAuth:   core.MCPOAuth{ClientSecret: "cs", Scopes: []string{"read", "write"}},
			},
			"toggle-srv": {Name: "toggle-srv", Source: "/p/.mcp.json", Enabled: &disabledFalse},
		},
		Disabled: []string{"other"},
	}
	return l
}

func TestRestrict_DoesNotMutateOrAliasInputs(t *testing.T) {
	in := mutationFixture()
	got, _ := Restrict(in)
	if !reflect.DeepEqual(in, mutationFixture()) {
		t.Fatal("Restrict mutated its input Layers")
	}

	// Scribble over every reference-typed field of the output's project
	// layers; the input must be unaffected.
	for _, rule := range got.Project.Permissions {
		for p := range rule.Patterns {
			rule.Patterns[p] = core.Allow
		}
	}
	got.Project.Permissions["zzz"] = core.Rule{}
	got.Project.ModelAliases["fast"] = "evil/x"
	for name, a := range got.Project.Agents {
		for i := range a.Tools {
			a.Tools[i] = "bash"
		}
		*a.CanSpawn = false
		*a.Hidden = true
		for _, rule := range a.Permissions {
			for p := range rule.Patterns {
				rule.Patterns[p] = core.Allow
			}
		}
		a.Permissions["zzz"] = core.Rule{}
		got.Project.Agents[name] = a
	}
	got.Project.Agents["zzz"] = core.AgentConfig{}
	got.ProjectMD["zzz"] = core.AgentConfig{}
	for i := range got.Project.MCP.Disabled {
		got.Project.MCP.Disabled[i] = "evil"
	}
	got.Project.MCP.Disabled = append(got.Project.MCP.Disabled, "zzz")
	for name, s := range got.Project.MCP.Servers {
		for k := range s.Env {
			s.Env[k] = "evil"
		}
		for k := range s.Headers {
			s.Headers[k] = "evil"
		}
		for i := range s.Args {
			s.Args[i] = "evil"
		}
		for i := range s.OAuth.Scopes {
			s.OAuth.Scopes[i] = "evil"
		}
		if s.Enabled != nil {
			*s.Enabled = true
		}
		got.Project.MCP.Servers[name] = s
	}
	got.Project.MCP.Servers["zzz"] = core.MCPServer{}
	if !reflect.DeepEqual(in, mutationFixture()) {
		t.Fatal("Restrict's output aliases its input Layers")
	}
}

func TestRestrict_EmptyProjectIsNoop(t *testing.T) {
	l := Layers{Global: core.Config{DefaultModel: "a/b"}}
	got, dropped := Restrict(l)
	if !reflect.DeepEqual(got, l) || dropped != nil {
		t.Errorf("Restrict(empty project) = %#v, %v", got, dropped)
	}
}

// runtimeRule reproduces how jig evaluates agent name's tool at runtime:
// agents.New overlays built-in < GlobalTOML < GlobalMD < ProjectTOML <
// ProjectMD, config.Merge overlays Project over Global permissions, and
// permission.Hook evaluates Effective(agent, cfg).
func runtimeRule(l Layers, name, tool string) core.Rule {
	b, _ := agents.Builtin(name)
	agent := b.Permissions
	for _, layer := range []map[string]core.AgentConfig{l.Global.Agents, l.GlobalMD, l.Project.Agents, l.ProjectMD} {
		agent = permission.Overlay(agent, layer[name].Permissions)
	}
	cfg := permission.Overlay(l.Global.Permissions, l.Project.Permissions)
	return permission.EffectiveFor(agent, cfg, tool)
}

func rank(a core.Action) int {
	switch a {
	case core.Deny:
		return 2
	case core.Ask:
		return 1
	default:
		return 0
	}
}

// TestRestrict_NeverLooserEndToEnd checks the whole restriction against
// the runtime merge: for every agent, tool, and subject, the restricted
// project never yields a looser verdict than the global layers alone.
func TestRestrict_NeverLooserEndToEnd(t *testing.T) {
	globals := []Layers{
		{},
		{Global: core.Config{Permissions: core.PermissionRules{"bash": {Default: core.Allow, Patterns: pats("git push*", "deny")}}}},
		{Global: core.Config{Permissions: core.PermissionRules{"bash": {Patterns: pats("*", "allow")}}},
			GlobalMD: map[string]core.AgentConfig{"plan": {Permissions: core.PermissionRules{"bash": {Patterns: pats("rm *", "deny")}}}}},
		{Global: core.Config{Agents: map[string]core.AgentConfig{"rev": {Permissions: core.PermissionRules{"edit": {Default: core.Deny}, "bash": {Default: core.Allow}}}}}},
	}
	projects := []Layers{
		{Project: core.Config{Permissions: core.PermissionRules{"bash": {Default: core.Allow, Patterns: pats("gi* push --force", "ask", "rm -rf *", "ask", "*", "ask")}}}},
		{Project: core.Config{Permissions: core.PermissionRules{"edit": {Patterns: pats("*", "ask")}, "bash": {Default: core.Deny}}}},
		{Project: core.Config{Agents: map[string]core.AgentConfig{
			"plan":  {Permissions: core.PermissionRules{"bash": {Default: core.Allow, Patterns: pats("rm -rf /", "ask", "gi* push*", "ask")}}},
			"rev":   {Permissions: core.PermissionRules{"edit": {Patterns: pats("x", "ask")}, "bash": {Patterns: pats("*", "deny")}}},
			"build": {Permissions: core.PermissionRules{"write": {Default: core.Allow}}},
		}}},
		{ProjectMD: map[string]core.AgentConfig{
			"rev":  {Permissions: core.PermissionRules{"bash": {Patterns: pats("rm *", "ask", "git *", "allow")}}},
			"plan": {Permissions: core.PermissionRules{"bash": {Patterns: pats("git push --force", "ask")}}},
		}},
	}
	names := []string{"build", "plan", "explore", "general", "rev"}
	tools := []string{"bash", "edit", "write", "read"}
	subjects := []string{"", "x", "ls", "git push --force", "git push", "git status", "rm -rf /", "rm x", "a;b"}

	for gi, g := range globals {
		for pi, p := range projects {
			l := g
			l.Project, l.ProjectMD = p.Project, p.ProjectMD
			restricted, _ := Restrict(l)
			for _, n := range names {
				for _, tool := range tools {
					before := runtimeRule(g, n, tool)
					after := runtimeRule(restricted, n, tool)
					for _, s := range subjects {
						b, a := permission.Evaluate(before, s), permission.Evaluate(after, s)
						if rank(a) < rank(b) {
							t.Errorf("global %d project %d agent %s %s %q: %s -> %s", gi, pi, n, tool, s, b, a)
						}
					}
				}
			}
		}
	}
}
