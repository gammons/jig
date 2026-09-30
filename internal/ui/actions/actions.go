// Package actions is jig's action catalogue: the stable IDs the ctrl+p
// picker and the mode handlers use to reach every user-invocable action,
// plus the default keymap and its config-driven remapping (see
// keymap.go). jig has no slash commands; every action is reached through
// the picker or a key.
package actions

import "github.com/gammons/jig/internal/core/ext"

// ID is the stable identifier of one action.
type ID string

const (
	SessionNew        ID = "session.new"
	SessionOpen       ID = "session.open"
	SessionRename     ID = "session.rename"
	SessionCompact    ID = "session.compact"
	AgentSwitch       ID = "agent.switch"
	ModelSwitch       ID = "model.switch"
	EffortSwitch      ID = "effort.switch"
	PromptAttach      ID = "prompt.attach"
	PromptEditor      ID = "prompt.editor"
	TranscriptSearch  ID = "transcript.search"
	TranscriptYank    ID = "transcript.yank"
	TranscriptDetails ID = "transcript.details"
	TranscriptFold    ID = "transcript.fold"
	RunCancel         ID = "run.cancel"
	ViewSidebar       ID = "view.sidebar"
	ViewTheme         ID = "view.theme"
	ViewReasoning     ID = "view.reasoning"
	MCPServers        ID = "mcp.servers"
	HelpKeys          ID = "help.keys"
	AppQuit           ID = "app.quit"
	PickerOpen        ID = "picker.open"
)

// Action is one entry the ctrl+p picker can show: a stable ID, its
// picker title and group, whether selecting it drills into another
// picker level, and, for an extension action, the ext.Command it runs.
type Action struct {
	ID      ID
	Title   string
	Group   string
	Drill   bool
	Command ext.Command // set for ext.<name> actions; nil for builtins
}

// builtinActions returns jig's built-in actions, in spec order, grouped
// Session / Agent & model / Prompt / Transcript / View / App.
func builtinActions() []Action {
	return []Action{
		{ID: SessionNew, Title: "New session", Group: "Session"},
		{ID: SessionOpen, Title: "Open session…", Group: "Session", Drill: true},
		{ID: SessionRename, Title: "Rename session…", Group: "Session", Drill: true},
		{ID: SessionCompact, Title: "Compact session", Group: "Session"},
		{ID: AgentSwitch, Title: "Switch agent…", Group: "Agent & model", Drill: true},
		{ID: ModelSwitch, Title: "Switch model…", Group: "Agent & model", Drill: true},
		{ID: EffortSwitch, Title: "Switch effort…", Group: "Agent & model", Drill: true},
		{ID: PromptAttach, Title: "Attach files…", Group: "Prompt", Drill: true},
		{ID: PromptEditor, Title: "Edit prompt in $EDITOR", Group: "Prompt"},
		{ID: TranscriptSearch, Title: "Search transcript", Group: "Transcript"},
		{ID: TranscriptYank, Title: "Yank block", Group: "Transcript"},
		{ID: TranscriptDetails, Title: "Toggle details", Group: "Transcript"},
		{ID: TranscriptFold, Title: "Toggle group", Group: "Transcript"},
		{ID: RunCancel, Title: "Cancel run", Group: "Transcript"},
		{ID: ViewSidebar, Title: "Toggle sidebar", Group: "View"},
		{ID: ViewTheme, Title: "Switch theme…", Group: "View", Drill: true},
		{ID: ViewReasoning, Title: "Streamed reasoning…", Group: "View", Drill: true},
		{ID: MCPServers, Title: "MCP servers…", Group: "MCP", Drill: true},
		{ID: HelpKeys, Title: "Keybindings", Group: "App", Drill: true},
		{ID: AppQuit, Title: "Quit", Group: "App"},
		{ID: PickerOpen, Title: "Open picker", Group: "App"},
	}
}

// Catalogue is the ordered, indexed set of actions the picker shows:
// jig's built-ins followed by every registered ext.Command, exposed as
// "ext.<name>" actions in the "Extensions" group.
type Catalogue struct {
	actions []Action
	index   map[ID]int
}

// NewCatalogue builds a Catalogue from jig's built-in actions and cmds,
// the extension commands registered through the ext.Registry.
func NewCatalogue(cmds []ext.Command) *Catalogue {
	all := builtinActions()
	for _, c := range cmds {
		all = append(all, Action{
			ID:      ID("ext." + c.Name()),
			Title:   c.Description(),
			Group:   "Extensions",
			Command: c,
		})
	}
	index := make(map[ID]int, len(all))
	for i, a := range all {
		index[a.ID] = i
	}
	return &Catalogue{actions: all, index: index}
}

// All returns every action, in catalogue order.
func (c *Catalogue) All() []Action {
	out := make([]Action, len(c.actions))
	copy(out, c.actions)
	return out
}

// Get returns the action registered under id, if any.
func (c *Catalogue) Get(id ID) (Action, bool) {
	i, ok := c.index[id]
	if !ok {
		return Action{}, false
	}
	return c.actions[i], true
}
