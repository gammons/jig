package imgrender

import (
	"fmt"
	"strings"
)

// Protocol is how an image reaches the terminal.
type Protocol int

const (
	Off    Protocol = iota // a one-line "[image WxH]" note
	Blocks                 // truecolor '▀' half-blocks, two pixels per cell
	Sixel                  // DEC sixel graphics
	Kitty                  // kitty graphics with unicode placeholders
)

func (p Protocol) String() string {
	switch p {
	case Off:
		return "off"
	case Blocks:
		return "blocks"
	case Sixel:
		return "sixel"
	case Kitty:
		return "kitty"
	}
	return fmt.Sprintf("Protocol(%d)", int(p))
}

// Env is the terminal-related environment, captured by the caller (this
// package never reads the process environment).
type Env struct {
	Term          string // $TERM
	TermProgram   string // $TERM_PROGRAM
	KittyWindowID string // $KITTY_WINDOW_ID
	TMUX          string // $TMUX
	Override      string // $JIG_IMAGES: kitty, sixel, blocks, or off
}

// Detect picks the protocol (R24): the Override if it names a protocol;
// else the terminal's XTVERSION name (terminalName, from
// tea.TerminalVersionMsg; "" if unknown): kitty/ghostty → Kitty,
// foot/mlterm → Sixel; else the env: under tmux → Blocks (tmux strips the
// outer terminal's identity and needs passthrough for graphics), then
// slk's rules; otherwise Blocks.
func Detect(env Env, terminalName string) Protocol {
	if p, ok := parseOverride(env.Override); ok {
		return p
	}
	if p, ok := fromTerminalName(terminalName); ok {
		return p
	}
	return fromEnv(env)
}

func parseOverride(s string) (Protocol, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "kitty":
		return Kitty, true
	case "sixel":
		return Sixel, true
	case "blocks":
		return Blocks, true
	case "off":
		return Off, true
	}
	return Off, false
}

// fromTerminalName matches the XTVERSION reply by prefix, since terminals
// append their version as "kitty(0.36.4)" or "ghostty 1.1.3".
func fromTerminalName(name string) (Protocol, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "":
		return Off, false
	case strings.HasPrefix(n, "kitty"), strings.HasPrefix(n, "ghostty"):
		return Kitty, true
	case strings.HasPrefix(n, "foot"), strings.HasPrefix(n, "mlterm"):
		return Sixel, true
	}
	return Off, false
}

// fromEnv is slk's capability.Detect without the tmux shell-out. tmux is
// checked first: a KITTY_WINDOW_ID inherited by a tmux session is often
// stale after a reattach, and kitty graphics need passthrough there.
func fromEnv(env Env) Protocol {
	switch {
	case env.TMUX != "":
		return Blocks
	case env.KittyWindowID != "", env.Term == "xterm-kitty", env.TermProgram == "ghostty":
		return Kitty
	case strings.HasPrefix(env.Term, "foot"), strings.HasPrefix(env.Term, "mlterm"):
		// foot ships "foot" and "foot-extra"; mlterm adds "-256color" variants.
		return Sixel
	case env.TermProgram == "iTerm.app", env.TermProgram == "WezTerm":
		// Their kitty support lacks unicode placeholders; both do sixel well.
		return Sixel
	}
	return Blocks
}
