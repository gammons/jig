package imgrender

import "testing"

func TestDetect_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		env      Env
		terminal string
		want     Protocol
	}{
		{"override kitty", Env{Override: "kitty"}, "", Kitty},
		{"override sixel", Env{Override: "sixel", KittyWindowID: "1"}, "kitty(0.36)", Sixel},
		{"override blocks", Env{Override: "blocks", TermProgram: "ghostty"}, "ghostty 1.1", Blocks},
		{"override off", Env{Override: "off"}, "kitty(0.36)", Off},
		{"override case and space", Env{Override: " KITTY "}, "", Kitty},
		{"override beats tmux", Env{Override: "kitty", TMUX: "/tmp/tmux-1000/default,1,0"}, "", Kitty},
		{"unknown override is auto", Env{Override: "auto", TermProgram: "ghostty"}, "", Kitty},

		{"terminal kitty", Env{}, "kitty(0.36)", Kitty},
		{"terminal ghostty", Env{}, "ghostty 1.1", Kitty},
		{"terminal foot", Env{}, "foot", Sixel},
		{"terminal foot versioned", Env{}, "foot(1.16.2)", Sixel},
		{"terminal mlterm", Env{}, "mlterm(3.9.3)", Sixel},
		{"terminal beats env", Env{TermProgram: "iTerm.app"}, "kitty(0.36)", Kitty},
		{"unknown terminal falls to env", Env{TermProgram: "ghostty"}, "xterm(390)", Kitty},

		{"env kitty window id", Env{KittyWindowID: "1"}, "", Kitty},
		{"env term xterm-kitty", Env{Term: "xterm-kitty"}, "", Kitty},
		{"env term_program ghostty", Env{TermProgram: "ghostty"}, "", Kitty},
		{"env term foot", Env{Term: "foot"}, "", Sixel},
		{"env term foot-extra", Env{Term: "foot-extra"}, "", Sixel},
		{"env term mlterm", Env{Term: "mlterm-256color"}, "", Sixel},
		{"env iterm", Env{TermProgram: "iTerm.app"}, "", Sixel},
		{"env wezterm", Env{TermProgram: "WezTerm"}, "", Sixel},

		{"tmux", Env{TMUX: "/tmp/tmux-1000/default,1,0", Term: "tmux-256color"}, "", Blocks},
		{"tmux with stale kitty id", Env{TMUX: "x", KittyWindowID: "1"}, "", Blocks},
		{"tmux answers xtversion", Env{TMUX: "x", Term: "foot"}, "tmux 3.4", Blocks},

		{"nothing", Env{}, "", Blocks},
		{"plain xterm", Env{Term: "xterm-256color"}, "xterm(390)", Blocks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Detect(tt.env, tt.terminal); got != tt.want {
				t.Errorf("Detect(%+v, %q) = %v, want %v", tt.env, tt.terminal, got, tt.want)
			}
		})
	}
}

func TestProtocol_String(t *testing.T) {
	t.Parallel()
	for p, want := range map[Protocol]string{Off: "off", Blocks: "blocks", Sixel: "sixel", Kitty: "kitty"} {
		if got := p.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(p), got, want)
		}
	}
}
