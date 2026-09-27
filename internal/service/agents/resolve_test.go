package agents

import (
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestResolve_Chain(t *testing.T) {
	agentModel := core.ModelRef{Provider: "anthropic", Model: "claude-opus-4"}
	parentModel := core.ModelRef{Provider: "anthropic", Model: "claude-sonnet-4"}
	sessionModel := core.ModelRef{Provider: "openai", Model: "gpt-5"}
	defaultModel := core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4"}

	tests := []struct {
		name        string
		agent       core.ModelRef
		parent      core.ModelRef
		session     core.ModelRef
		defaultCfg  string
		want        core.ModelRef
		wantErr     bool
		wantErrText string
	}{
		{
			name:       "agent model wins over everything",
			agent:      agentModel,
			parent:     parentModel,
			session:    sessionModel,
			defaultCfg: defaultModel.String(),
			want:       agentModel,
		},
		{
			name:       "parent wins when agent is zero",
			parent:     parentModel,
			session:    sessionModel,
			defaultCfg: defaultModel.String(),
			want:       parentModel,
		},
		{
			name:       "session wins when agent and parent are zero",
			session:    sessionModel,
			defaultCfg: defaultModel.String(),
			want:       sessionModel,
		},
		{
			name:       "default model used when agent, parent, session are zero",
			defaultCfg: defaultModel.String(),
			want:       defaultModel,
		},
		{
			name:        "all zero and no default configured is an error",
			wantErr:     true,
			wantErrText: "no model configured: set default_model in config.toml or pass --model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.Config{DefaultModel: tt.defaultCfg}
			svc, err := New(cfg, Sources{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			agent := core.Agent{Model: tt.agent}
			got, err := svc.ResolveModel(agent, tt.parent, tt.session)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolveModel: want error, got nil")
				}
				if err.Error() != tt.wantErrText {
					t.Errorf("ResolveModel: err = %q, want %q", err.Error(), tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveModel: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveModel = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSmallModel(t *testing.T) {
	fallback := core.ModelRef{Provider: "anthropic", Model: "claude-sonnet-4"}

	t.Run("uses cfg.SmallModel when set", func(t *testing.T) {
		cfg := core.Config{SmallModel: "anthropic/claude-haiku-4"}
		svc, err := New(cfg, Sources{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		want := core.ModelRef{Provider: "anthropic", Model: "claude-haiku-4"}
		if got := svc.SmallModel(fallback); got != want {
			t.Errorf("SmallModel = %+v, want %+v", got, want)
		}
	})

	t.Run("falls back when cfg.SmallModel is unset", func(t *testing.T) {
		svc, err := New(core.Config{}, Sources{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := svc.SmallModel(fallback); got != fallback {
			t.Errorf("SmallModel = %+v, want fallback %+v", got, fallback)
		}
	})
}

func TestResolveRef(t *testing.T) {
	cfg := core.Config{ModelAliases: map[string]string{"fast": "jigtest/m2", "broken": "no-slash"}}
	svc, err := New(cfg, Sources{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, err := svc.ResolveRef("anthropic/opus"); err != nil || got != (core.ModelRef{Provider: "anthropic", Model: "opus"}) {
		t.Errorf("ResolveRef(provider/model) = %+v, %v", got, err)
	}
	if got, err := svc.ResolveRef("fast"); err != nil || got != (core.ModelRef{Provider: "jigtest", Model: "m2"}) {
		t.Errorf("ResolveRef(fast) = %+v, %v", got, err)
	}
	if _, err := svc.ResolveRef("nope"); err == nil || err.Error() != `model alias "nope" is not defined in [model_aliases]` {
		t.Errorf("ResolveRef(nope) err = %v", err)
	}
	if _, err := svc.ResolveRef("broken"); err == nil {
		t.Error("ResolveRef(broken) = nil error, want a parse error")
	}
}

func TestResolve_AliasesInDefaultAndSmallModel(t *testing.T) {
	cfg := core.Config{
		DefaultModel: "big",
		SmallModel:   "fast",
		ModelAliases: map[string]string{"big": "anthropic/opus", "fast": "anthropic/haiku"},
	}
	svc, err := New(cfg, Sources{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := svc.ResolveModel(core.Agent{}, core.ModelRef{}, core.ModelRef{})
	if err != nil || got != (core.ModelRef{Provider: "anthropic", Model: "opus"}) {
		t.Errorf("ResolveModel = %+v, %v; want anthropic/opus", got, err)
	}
	if got := svc.SmallModel(core.ModelRef{Provider: "x", Model: "y"}); got != (core.ModelRef{Provider: "anthropic", Model: "haiku"}) {
		t.Errorf("SmallModel = %+v, want anthropic/haiku", got)
	}
}
