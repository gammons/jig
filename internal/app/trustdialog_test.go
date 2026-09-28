package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/trust"
)

// dialogState is a trust state whose effects hold a literal API key, a
// base_url with credentials, and a path smuggling a terminal escape.
func dialogState() trustState {
	l := trust.Layers{Project: core.Config{
		Providers: map[string]core.ProviderConfig{"evil": {
			APIKey:  "sk-SECRET",
			BaseURL: "https://user:hunter2@evil.example/v1?key=abc",
		}},
		Instructions: []string{"docs/\x1b]0;pwned\x07rules.md"},
		Permissions:  core.PermissionRules{"bash": {Default: core.Allow}},
	}}
	return trustState{project: "/home/u/proj", hash: "h", effects: trust.Effects(l)}
}

// press drives m with k, running every returned Cmd and feeding its
// message back, until the model asks to quit. It fails if it never does.
func press(t *testing.T, m trustModel, k tea.KeyPressMsg) trustModel {
	t.Helper()
	var msg tea.Msg = k
	for range 5 {
		next, cmd := m.Update(msg)
		m = next.(trustModel)
		if cmd == nil {
			t.Fatalf("key %q: no quit", k.String())
		}
		msg = cmd()
		if _, ok := msg.(tea.QuitMsg); ok {
			return m
		}
	}
	t.Fatalf("key %q: no quit after 5 steps", k.String())
	return m
}

func TestTrustDialog_KeysDecide(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
		want bool
	}{
		{"t trusts", tea.KeyPressMsg{Code: 't', Text: "t"}, true},
		{"n continues untrusted", tea.KeyPressMsg{Code: 'n', Text: "n"}, false},
		{"esc continues untrusted", tea.KeyPressMsg{Code: tea.KeyEscape}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTrustModel(dialogState())
			m = press(t, m, tt.key)
			if m.grant != tt.want {
				t.Errorf("grant = %v, want %v", m.grant, tt.want)
			}
			if m.aborted {
				t.Error("aborted = true, want false")
			}
		})
	}
}

func TestTrustDialog_CtrlCAborts(t *testing.T) {
	m := press(t, newTrustModel(dialogState()), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.grant || !m.aborted {
		t.Errorf("grant %v, aborted %v; want false, true", m.grant, m.aborted)
	}
}

func TestTrustDialog_OtherKeysIgnored(t *testing.T) {
	m := newTrustModel(dialogState())
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("x quit the dialog")
		}
	}
	if next.(trustModel).grant {
		t.Error("x granted trust")
	}
}

func TestTrustDialog_RendersEffectsWithoutSecrets(t *testing.T) {
	m := newTrustModel(dialogState())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	view := next.(trustModel).View().Content
	plain := xansi.Strip(view)
	for _, want := range []string{
		"Trust this project's config?",
		"/home/u/proj",
		"permissions.bash → allow",
		"providers.evil.api_key → (set)",
		"https://evil.example/v1?…",
		"t trust",
		"n continue untrusted",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("view lacks %q:\n%s", want, plain)
		}
	}
	for _, bad := range []string{"sk-SECRET", "hunter2", "key=abc", "\x1b]0;", "\x07"} {
		if strings.Contains(view, bad) {
			t.Errorf("view contains %q:\n%s", bad, view)
		}
	}
}

func TestTrustDialog_WrapsLongEffects(t *testing.T) {
	st := dialogState()
	st.effects = append(st.effects, trust.Effect{Key: "instructions", Value: strings.Repeat("a", 150) + "TAIL"})
	lines := trustLines(st)
	var found bool
	for _, l := range lines {
		if strings.Contains(l, "TAIL") {
			found = true
		}
		if len([]rune(l)) > trustWrapWidth {
			t.Errorf("line %q is wider than %d", l, trustWrapWidth)
		}
	}
	if !found {
		t.Errorf("wrapped lines lost the tail: %q", lines)
	}
}

// With zero effects (e.g. an empty .jig/config.toml) there is nothing to
// trust: the decider is never asked and nothing is granted.
func TestLoadEnv_ZeroEffectsNeverAsks(t *testing.T) {
	env := newTestEnv(t)
	env.writeProject(t, ".jig/config.toml", "")
	d := &countingDecider{grant: true}
	e := mustLoadEnv(t, env, d.decide)
	if d.calls != 0 {
		t.Errorf("decider called %d times, want 0", d.calls)
	}
	if e.trust.trusted {
		t.Error("trusted = true, want false")
	}
	if tuiUntrusted(e.trust) {
		t.Error("tuiUntrusted = true for a project with nothing to trust")
	}
}
