package app

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/confirm"
	"github.com/gammons/jig/internal/bubbles/overlay"
)

// trustTitle is the trust dialog's title.
const trustTitle = "Trust this project's config?"

// trustWrapWidth is the width trust dialog lines are wrapped to: the
// confirm box's widest content row (70 cells, less borders and margin).
const trustWrapWidth = 66

// ErrTrustAborted is returned when ctrl+c closes the trust dialog.
var ErrTrustAborted = errors.New("trust dialog cancelled")

// trustDialog is the TUI's trustDecider: a short-lived tea program on std
// showing the project path and every effect, where t trusts and n or esc
// continues untrusted. ctrl+c aborts jig.
func trustDialog(std Stdio) trustDecider {
	return func(st trustState) (bool, error) {
		p := tea.NewProgram(newTrustModel(st), tea.WithInput(std.In), tea.WithOutput(std.Out))
		final, err := p.Run()
		if err != nil {
			return false, err
		}
		m, _ := final.(trustModel)
		if m.aborted {
			return false, ErrTrustAborted
		}
		return m.grant, nil
	}
}

// trustModel is the trust dialog's tea model: a confirm box centered on
// the screen. grant and aborted hold the answer once it quits.
type trustModel struct {
	dlg           confirm.Model
	width, height int
	grant         bool
	aborted       bool
}

// newTrustModel builds the dialog for st.
func newTrustModel(st trustState) trustModel {
	dlg := confirm.New()
	dlg.Set(trustTitle, trustLines(st), []confirm.Choice{
		{Key: "t", Label: "trust"},
		{Key: "n", Label: "continue untrusted"},
	})
	return trustModel{dlg: dlg}
}

// trustLines is the project path, a blank line, and one line per effect
// (wrapped, continuation lines indented), each sanitized: effects come
// from project files and are untrusted text.
func trustLines(st trustState) []string {
	lines := wrapLine(ansi.SanitizeLine(st.project), "")
	lines = append(lines, "")
	for _, e := range st.effects {
		lines = append(lines, wrapLine(ansi.SanitizeLine(e.String()), "  ")...)
	}
	return lines
}

// wrapLine wraps s to trustWrapWidth, prefixing continuation lines with
// indent.
func wrapLine(s, indent string) []string {
	first, rest, _ := strings.Cut(ansi.Wrap(s, trustWrapWidth), "\n")
	out := []string{first}
	if rest == "" {
		return out
	}
	for _, l := range strings.Split(ansi.Wrap(rest, trustWrapWidth-len(indent)), "\n") {
		out = append(out, indent+l)
	}
	return out
}

func (m trustModel) Init() tea.Cmd { return nil }

// Update sizes the box, answers t/n/esc/ctrl+c, and passes other keys
// (j/k scrolling) to the box.
func (m trustModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.dlg.SetSize(msg.Width, msg.Height)
		return m, nil
	case confirm.ChosenMsg:
		m.grant = msg.Key == "t"
		return m, tea.Quit
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return m, tea.Quit
		case "ctrl+c":
			m.aborted = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.dlg, cmd = m.dlg.Update(msg)
	return m, cmd
}

// View centers the box on an otherwise empty alt screen.
func (m trustModel) View() tea.View {
	v := tea.View{AltScreen: true}
	if m.width <= 0 || m.height <= 0 {
		return v
	}
	bg := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", m.width)+"\n", m.height), "\n")
	v.Content = overlay.Center(bg, m.width, m.height, m.dlg.View(), 0)
	return v
}
