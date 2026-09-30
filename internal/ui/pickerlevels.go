package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/actions"
	"github.com/gammons/jig/internal/ui/theme"
)

// The picker's level IDs.
const (
	levelRoot     = "root"
	levelSessions = "sessions"
	levelModels   = "models"
	levelAgents   = "agents"
	levelThemes   = "themes"
	levelRename   = "rename"
	levelFiles    = "files"
	levelKeys     = "keys"
	levelMCP      = "mcp"
	levelMCPSvr   = "mcp.server"
	levelMCPTools = "mcp.tools"
)

// maxSessions is how many of the workdir's sessions the sessions level
// lists.
const maxSessions = 30

// levelAction is the action a drill-down level belongs to, recorded as
// recent when one of its items is chosen.
func levelAction(level string) actions.ID {
	switch level {
	case levelSessions:
		return actions.SessionOpen
	case levelModels:
		return actions.ModelSwitch
	case levelAgents:
		return actions.AgentSwitch
	case levelThemes:
		return actions.ViewTheme
	case levelRename:
		return actions.SessionRename
	case levelFiles:
		return actions.PromptAttach
	case levelMCPSvr:
		return actions.MCPServers
	}
	return ""
}

// rootLevel is the picker's action list.
func rootLevel() picker.Level { return picker.Level{ID: levelRoot, Title: "Actions", Actions: true} }

// filesLevel is the multi-select file list.
func filesLevel() picker.Level { return picker.Level{ID: levelFiles, Title: "Files", Multi: true} }

// drillLevel is the level action id opens, if it drills; title is the
// open session's title (the rename level's initial text).
func drillLevel(id actions.ID, title string) (picker.Level, bool) {
	switch id {
	case actions.SessionOpen:
		return picker.Level{ID: levelSessions, Title: "Sessions"}, true
	case actions.SessionRename:
		return picker.Level{ID: levelRename, Title: "Rename session", Input: true, Initial: ansi.SanitizeLine(title)}, true
	case actions.ModelSwitch:
		return picker.Level{ID: levelModels, Title: "Models"}, true
	case actions.AgentSwitch:
		return picker.Level{ID: levelAgents, Title: "Agents"}, true
	case actions.ViewTheme:
		return picker.Level{ID: levelThemes, Title: "Themes"}, true
	case actions.PromptAttach:
		return filesLevel(), true
	case actions.HelpKeys:
		return picker.Level{ID: levelKeys, Title: "Keybindings"}, true
	case actions.MCPServers:
		return picker.Level{ID: levelMCP, Title: "MCP servers"}, true
	}
	return picker.Level{}, false
}

// levels is the picker's LoadFunc: it builds each level's items, calling
// ports only inside the Cmds it returns.
type levels struct{ a *App }

// load returns the Cmd that yields level's items.
func (l levels) load(level picker.Level) tea.Cmd {
	a := l.a
	switch level.ID {
	case levelRoot:
		return itemsCmd(level.ID, rootItems(a.opts.Actions, a.opts.Keymap, keymapMode(a.view.pick.prev), a.sess.info))
	case levelKeys:
		return itemsCmd(level.ID, keyItems(a.opts.Actions, a.opts.Keymap))
	case levelThemes:
		return itemsCmd(level.ID, themeItems(a.theme.custom, a.theme.shown()))
	case levelModels:
		return modelsCmd(a.ports, a.sess.modelRef())
	case levelAgents:
		return agentItemsCmd(a.ports, a.sess.info.Agent)
	case levelSessions:
		if a.ports.Sessions == nil {
			return itemsCmd(level.ID, nil)
		}
		return sessionsCmd(a.ctx, a.ports, a.opts.WorkDir, a.sess.info.ID, a.opts.Clock.Now())
	case levelFiles:
		return filesCmd(a.ctx, a.ports, touchedFiles(a.opts.WorkDir, a.sess.proj.Blocks(), a.sess.proj.ChangedFiles()))
	case levelMCP:
		return itemsCmd(level.ID, mcpServerItems(a.view.mcp.list, a.ports.MCP != nil))
	case levelMCPSvr:
		return itemsCmd(level.ID, mcpServerActionItems(a.view.mcp.list, level.Arg))
	case levelMCPTools:
		return itemsCmd(level.ID, mcpToolItems(a.view.mcp.list, level.Arg))
	}
	return itemsCmd(level.ID, nil)
}

// keymapMode is m's keymap mode name.
func keymapMode(m mode) string {
	if m == modeNormal {
		return normalMode
	}
	return insertMode
}

// rootItems lists every action but picker.open, with the keys bound to
// it in mode as the detail. Drilling actions carry their level; renaming
// needs a session.
func rootItems(c *actions.Catalogue, km actions.Keymap, mode string, info core.Session) []picker.Item {
	if c == nil {
		return nil
	}
	var out []picker.Item
	for _, act := range c.All() {
		if act.ID == actions.PickerOpen {
			continue
		}
		it := picker.Item{
			ID: string(act.ID), Title: ansi.SanitizeLine(act.Title), Group: act.Group,
			Detail: keysDetail(km.Keys(mode, act.ID)),
		}
		if lvl, ok := drillLevel(act.ID, info.Title); ok {
			it.Drill = &lvl
		}
		if act.ID == actions.SessionRename && info.ID == "" {
			it.Drill, it.Disabled = nil, true
		}
		out = append(out, it)
	}
	return out
}

// keysDetail joins keys (bound key strings, which name a config-supplied
// [keybinds] key and so are untrusted) into one sanitized detail string.
func keysDetail(keys []string) string {
	sanitized := make([]string, len(keys))
	for i, k := range keys {
		sanitized[i] = ansi.SanitizeLine(k)
	}
	return strings.Join(sanitized, " ")
}

// keyItems lists every action with its keys in both modes, read-only.
func keyItems(c *actions.Catalogue, km actions.Keymap) []picker.Item {
	if c == nil {
		return nil
	}
	var out []picker.Item
	for _, act := range c.All() {
		var parts []string
		for _, mode := range []string{insertMode, normalMode} {
			if keys := km.Keys(mode, act.ID); len(keys) > 0 {
				parts = append(parts, mode+": "+keysDetail(keys))
			}
		}
		out = append(out, picker.Item{
			ID: string(act.ID), Title: ansi.SanitizeLine(act.Title), Group: act.Group,
			Detail: strings.Join(parts, " · "), Disabled: true,
		})
	}
	return out
}

// themeItems lists the custom palettes, then every built-in one a custom
// palette doesn't shadow; current is the palette in use.
func themeItems(custom []theme.Palette, current string) []picker.Item {
	seen := map[string]bool{}
	var out []picker.Item
	for _, p := range append(append([]theme.Palette(nil), custom...), theme.Builtin()...) {
		key := strings.ToLower(p.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, picker.Item{ID: p.Name, Title: ansi.SanitizeLine(p.Name), Current: strings.EqualFold(p.Name, current)})
	}
	return out
}

// modelItems lists every catalog model grouped by provider; an
// unconfigured provider's models are disabled.
func modelItems(providers []core.ProviderStatus, current string) []picker.Item {
	var out []picker.Item
	for _, p := range providers {
		group := p.Info.Name
		if group == "" {
			group = p.Info.ID
		}
		for _, m := range p.Info.Models {
			title := m.Name
			if title == "" {
				title = m.Ref.Model
			}
			ref := m.Ref.String()
			out = append(out, picker.Item{
				ID: ref, Title: ansi.SanitizeLine(title), Group: ansi.SanitizeLine(group),
				Detail: modelDetail(m), Disabled: !p.Configured, Current: ref == current,
			})
		}
	}
	return out
}

// modelDetail is "200k ctx · $3/$15": the context window, then the input
// and output prices per million tokens.
func modelDetail(m core.ModelInfo) string {
	price := "$" + fmtNum(m.CostIn) + "/$" + fmtNum(m.CostOut)
	if m.ContextWindow <= 0 {
		return price
	}
	return fmtTokens(m.ContextWindow) + " ctx · " + price
}

// fmtTokens is n as "200k" or "1M".
func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmtNum(float64(n)/1e6) + "M"
	case n >= 1000:
		return fmtNum(float64(n)/1e3) + "k"
	}
	return strconv.FormatInt(n, 10)
}

// fmtNum is v with as few decimals as it needs.
func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// agentItems lists the primary agents with their descriptions.
func agentItems(agents []core.Agent, current string) []picker.Item {
	out := make([]picker.Item, len(agents))
	for i, ag := range agents {
		out[i] = picker.Item{
			ID: ag.Name, Title: ansi.SanitizeLine(ag.Name), Detail: ansi.SanitizeLine(ag.Description),
			Current: ag.Name == current,
		}
	}
	return out
}

// sessionItems lists sessions with "<age> · $<cost>" details.
func sessionItems(list []core.Session, costs []float64, current core.SessionID, now time.Time) []picker.Item {
	out := make([]picker.Item, len(list))
	for i, s := range list {
		title := ansi.SanitizeLine(s.Title)
		if title == "" {
			title = "untitled"
		}
		at := s.UpdatedAt
		if at.IsZero() {
			at = s.CreatedAt
		}
		out[i] = picker.Item{
			ID: string(s.ID), Title: title, Current: s.ID == current,
			Detail: fmt.Sprintf("%s · $%.2f", relativeAge(now.Sub(at)), costs[i]),
		}
	}
	return out
}

// relativeAge is d as "just now", "5m ago", "3h ago", or "2d ago".
func relativeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// mcpServerItems lists one item per configured server (mcp level); when
// there is no MCP port it returns one disabled placeholder.
func mcpServerItems(list []core.MCPServerStatus, hasPort bool) []picker.Item {
	if !hasPort {
		return []picker.Item{{ID: "", Title: "no MCP servers configured", Disabled: true}}
	}
	out := make([]picker.Item, len(list))
	for i, s := range list {
		name := ansi.SanitizeLine(s.Name)
		lvl := picker.Level{ID: levelMCPSvr, Title: name, Arg: s.Name}
		out[i] = picker.Item{ID: s.Name, Title: name, Detail: ansi.SanitizeLine(mcpDetail(s)), Drill: &lvl}
	}
	return out
}

// mcpServerActionItems lists the actions available for the server named
// arg, in state order (§8.3).
func mcpServerActionItems(list []core.MCPServerStatus, arg string) []picker.Item {
	var s core.MCPServerStatus
	found := false
	for _, st := range list {
		if st.Name == arg {
			s, found = st, true
			break
		}
	}
	if !found {
		return nil
	}
	toolsLvl := picker.Level{ID: levelMCPTools, Title: s.Name + " tools", Arg: s.Name}
	tools := picker.Item{ID: "tools", Title: "Tools…", Drill: &toolsLvl}
	var out []picker.Item
	switch s.State {
	case core.MCPNeedsAuth:
		out = []picker.Item{{ID: "signin", Title: "Sign in"}, {ID: "reconnect", Title: "Reconnect"}, tools}
	case core.MCPAuthenticating:
		out = []picker.Item{{ID: "cancel", Title: "Cancel sign-in"}, {ID: "copy", Title: "Copy sign-in URL"}, tools}
	case core.MCPReady:
		out = []picker.Item{{ID: "reconnect", Title: "Reconnect"}}
		if s.HasToken {
			out = append(out, picker.Item{ID: "signout", Title: "Sign out"})
		}
		out = append(out, tools)
	case core.MCPFailed:
		out = []picker.Item{}
		if s.Transport == core.MCPHTTP || s.Transport == core.MCPSSE {
			out = append(out, picker.Item{ID: "signin", Title: "Sign in"})
		}
		out = append(out, picker.Item{ID: "reconnect", Title: "Reconnect"}, tools)
	case core.MCPConnecting:
		out = []picker.Item{tools}
	}
	return out
}

// mcpToolItems lists the server named arg's tools, read-only.
func mcpToolItems(list []core.MCPServerStatus, arg string) []picker.Item {
	for _, s := range list {
		if s.Name != arg {
			continue
		}
		out := make([]picker.Item, len(s.ToolNames))
		for i, t := range s.ToolNames {
			out[i] = picker.Item{ID: strconv.Itoa(i), Title: ansi.SanitizeLine(t)}
		}
		return out
	}
	return nil
}
