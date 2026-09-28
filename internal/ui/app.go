package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/confirm"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/bubbles/overlay"
	"github.com/gammons/jig/internal/bubbles/permcard"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/bubbles/statusbar"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/actions"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// Timing (spec §5.2): streaming re-renders at most every 80 ms; a width
// change re-renders every transcript block, so bursts of resizes are
// coalesced into one list resize 50 ms after the last.
const (
	streamInterval = 80 * time.Millisecond
	resizeDebounce = 50 * time.Millisecond
	overlayDim     = 0.4
)

// Options configure an App.
type Options struct {
	Session             core.SessionID // resume; "" = new
	WorkDir, ProjectKey string
	Untrusted           bool
	Actions             *actions.Catalogue
	Keymap              actions.Keymap
	Themes              []theme.Palette // custom palettes
	Theme               string
	Images              imgrender.Env
	Tmux                bool
	Aliases             map[string]string // alias → "provider/model", for the status bar
	Clock               clock.Clock
}

// mode is the App's input mode.
type mode int

const (
	modeInsert mode = iota
	modeNormal
	modePicker
)

// String is the status bar's mode badge.
func (m mode) String() string {
	switch m {
	case modeNormal:
		return "NORMAL"
	case modePicker:
		return "PICKER"
	}
	return "INSERT"
}

// Messages the App schedules for itself.
type (
	streamTickMsg struct{}
	resizeMsg     struct{ gen int }
)

// widgets holds every widget the App owns. upserts counts list Upsert
// calls (streaming coalescing is asserted on it).
type widgets struct {
	list    blocklist.Model
	prompt  prompt.Model
	picker  picker.Model
	details details.Model
	card    permcard.Model
	status  statusbar.Model
	side    sidebar.Model
	confirm confirm.Model
	render  *renderer
	upserts int
}

// upsert re-renders items in the transcript list; nothing for none.
func (w *widgets) upsert(items []blocklist.Item) {
	if len(items) == 0 {
		return
	}
	w.upserts++
	w.list.Upsert(items...)
}

// viewState is the App's presentation state: the sidebar preference, whether the details split is open, the
// status hint, the project's prompt history, whether streamTick is
// running, and the transcript list's applied size (listW/listH) and a
// pending debounced width (pendingW, keyed by resizeGen).
type viewState struct {
	sidebarPref  *bool
	detailsOpen  bool
	hint         string
	history      []string
	ticking      bool
	resizeGen    int
	pendingW     int
	listW, listH int
}

// App is jig's TUI: a bubbletea model that bridges bus events into
// messages, owns every widget's state, and reconciles them. Widgets talk
// back only through the Cmds they return.
type App struct {
	ports         Ports
	opts          Options
	w             widgets
	sess          *sessionState
	lay           rects
	mode          mode
	theme         *themeState
	img           *imgrender.Renderer
	sub           *event.Subscription
	view          viewState
	width, height int
	ctx           context.Context
	cancel        context.CancelFunc
	after         func(time.Duration, tea.Msg) tea.Cmd
}

// New builds an App over p. It subscribes to the bus at once, so no event
// published after New is missed.
func New(p Ports, o Options) *App {
	if o.Clock == nil {
		o.Clock = clock.Real()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		ports: p, opts: o, ctx: ctx, cancel: cancel,
		sess:  newSessionState(o.Session, o.Clock),
		theme: newThemeState(o.Theme, o.Themes),
		img:   imgrender.New(imgrender.Detect(o.Images, ""), imgrender.WithTmux(o.Tmux)),
		after: tick,
	}
	a.w = newWidgets(&a.theme.set, func(text string) tea.Cmd { return editorCmd(a.ports, text) })
	if p.Subscribe != nil {
		a.sub = p.Subscribe()
	}
	return a
}

// tick is tea.Tick yielding msg after d.
func tick(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

// newWidgets builds every widget styled from set.
func newWidgets(set *theme.Set, edit prompt.EditFunc) widgets {
	r := newRenderer(set)
	return widgets{
		list:    blocklist.New(r.render, blocklist.WithStyles(set.Blocklist)),
		prompt:  prompt.New(edit, prompt.WithStyles(set.Prompt)),
		picker:  picker.New(func(picker.Level) tea.Cmd { return nil }, picker.WithStyles(set.Picker)),
		details: details.New(details.WithStyles(set.Details)),
		card:    permcard.New(func(string, permcard.Reply) tea.Cmd { return nil }, permcard.WithStyles(set.Card)),
		status:  statusbar.New(statusbar.WithStyles(set.Status)),
		side:    sidebar.New(sidebar.WithStyles(set.Sidebar)),
		confirm: confirm.New(confirm.WithStyles(set.Confirm)),
		render:  r,
	}
}

// Init focuses the prompt, asks the terminal for its name (image protocol
// detection), starts the bus bridge, loads prefs, agents, and the
// catalog, and, when resuming, the session.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.w.prompt.Focus(), tea.RequestTerminalVersion, waitEvent(a.sub),
		prefsCmd(a.ports), agentsCmd(a.ports), catalogCmd(a.ports),
	}
	if a.opts.Session != "" {
		cmds = append(cmds, resumeCmd(a.ctx, a.ports, a.opts.Session))
	}
	return tea.Batch(cmds...)
}

// Update handles one message, then re-lays out and rebuilds the status
// bar and sidebar from the reconciled state.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = max(msg.Width, 0), max(msg.Height, 0)
	case tea.KeyPressMsg:
		a.view.hint = ""
		cmd = a.onKey(msg)
	case tea.PasteMsg:
		if a.mode == modeInsert {
			a.w.prompt, cmd = a.w.prompt.Update(msg)
		}
	case eventMsg:
		cmd = a.onEvent(msg.ev)
	case streamTickMsg:
		cmd = a.onTick()
	case resizeMsg:
		if msg.gen == a.view.resizeGen {
			a.applyListWidth()
		}
	default:
		cmd = a.onResult(msg)
	}
	layout := a.relayout()
	a.sync()
	return a, tea.Batch(cmd, layout)
}

// View renders the frame: the regions per the layout, and the picker
// composited over them while it is open.
func (a *App) View() tea.View {
	v := tea.View{AltScreen: true}
	if a.width <= 0 || a.height <= 0 {
		return v
	}
	side := ""
	switch {
	case a.lay.DetailsOpen:
		side = a.w.details.View()
	case a.lay.SideVisible:
		side = a.w.side.View()
	}
	v.Content = compose(a.lay, a.w.list.View(), side, a.w.prompt.View(), a.w.status.View())
	if a.w.picker.IsOpen() {
		v.Content = overlay.Center(v.Content, a.width, a.height, a.w.picker.View(), overlayDim)
	}
	return v
}

// onKey routes a key press to the current mode's handler.
func (a *App) onKey(k tea.KeyPressMsg) tea.Cmd {
	switch a.mode {
	case modeNormal:
		return normalKeys{a}.handle(k)
	case modePicker:
		var cmd tea.Cmd
		a.w.picker, cmd = a.w.picker.Update(k)
		return cmd
	}
	return insertKeys{a}.handle(k)
}

// onEvent reconciles one bus event and re-arms the bridge.
func (a *App) onEvent(ev event.Event) tea.Cmd {
	res := a.sess.apply(ev)
	if res.reload {
		a.w.list.SetItems(a.sess.allItems())
	}
	a.flush(res.upsert)
	var cmd tea.Cmd
	if res.settled {
		cmd = a.sender().afterRun()
	}
	return tea.Batch(waitEvent(a.sub), cmd)
}

// onTick renders the dirty streaming blocks and advances the spinners in
// one Upsert, and reschedules itself while the run lasts.
func (a *App) onTick() tea.Cmd {
	a.flush(a.sess.tick())
	if a.sess.run.running {
		return a.after(streamInterval, streamTickMsg{})
	}
	a.view.ticking = false
	return nil
}

// flush re-renders the blocks ids in the transcript list.
func (a *App) flush(ids []transcript.BlockID) {
	a.w.upsert(a.sess.items(ids))
}

// onResult handles port results and widget messages.
func (a *App) onResult(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case prompt.SubmitMsg:
		return a.sender().submit(msg.Text)
	case prompt.MentionMsg:
		a.w.prompt.Insert("@")
	case prompt.EditedMsg:
		if msg.Err != nil {
			a.view.hint = "editor: " + ansi.SanitizeLine(msg.Err.Error())
		}
		var cmd tea.Cmd
		a.w.prompt, cmd = a.w.prompt.Update(msg)
		return cmd
	case editorExecMsg:
		return tea.Exec(msg.cmd, func(err error) tea.Msg { return editorExitedMsg{err: err, result: msg.result} })
	case editorExitedMsg:
		return editorResultCmd(msg)
	case sendDoneMsg:
		return a.sender().done(msg)
	case tea.TerminalVersionMsg:
		if p := imgrender.Detect(a.opts.Images, msg.Name); p != a.img.Protocol() {
			a.img = imgrender.New(p, imgrender.WithTmux(a.opts.Tmux))
		}
	default:
		a.onPortResult(msg)
	}
	return nil
}

// onPortResult folds a startup port result into the state.
func (a *App) onPortResult(msg tea.Msg) {
	switch msg := msg.(type) {
	case prefsMsg:
		a.view.sidebarPref = msg.prefs.Sidebar
		a.view.history = append([]string(nil), msg.prefs.History[a.opts.ProjectKey]...)
		a.w.prompt.SetHistory(a.view.history)
	case agentsMsg:
		a.sess.cat.agents = msg.agents
		if a.sess.info.Agent == "" && len(msg.agents) > 0 {
			a.sess.info.Agent = msg.agents[0].Name
		}
		a.w.prompt.SetAgent(ansi.SanitizeLine(a.sess.info.Agent))
	case catalogMsg:
		a.sess.cat.providers = msg.providers
	case resumeMsg:
		if msg.err != nil {
			a.view.hint = "session: " + ansi.SanitizeLine(msg.err.Error())
			return
		}
		if !a.sess.run.busy() {
			a.sess.load(msg.info, msg.msgs, msg.todos)
			a.w.list.SetItems(a.sess.allItems())
			a.w.prompt.SetAgent(ansi.SanitizeLine(a.sess.info.Agent))
		}
	case errMsg:
		a.view.hint = msg.what + ": " + ansi.SanitizeLine(msg.err.Error())
	}
}

// relayout recomputes the layout for the current size and prompt height
// and sizes every widget to it. The transcript list's height applies at
// once; a width change is debounced (it re-renders every block).
func (a *App) relayout() tea.Cmd {
	if a.lay.Prompt.W != a.width {
		a.w.prompt.SetWidth(a.width)
	}
	a.lay = computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, a.view.detailsOpen)
	a.w.status.SetWidth(a.lay.Status.W)
	a.w.side.SetSize(a.lay.Side.W, a.lay.Side.H)
	a.w.details.SetSize(a.lay.Side.W, a.lay.Side.H)
	a.w.picker.SetSize(a.width, a.height)
	a.w.confirm.SetSize(a.width, a.height)
	a.w.card.SetWidth(a.lay.Transcript.W)

	tw, th := a.lay.Transcript.W, a.lay.Transcript.H
	if a.view.listW == 0 || tw == a.view.listW {
		a.view.pendingW = tw
		a.w.list.SetSize(tw, th)
		a.view.listW, a.view.listH = tw, th
		return nil
	}
	a.w.list.SetSize(a.view.listW, th)
	a.view.listH = th
	if tw == a.view.pendingW {
		return nil
	}
	a.view.resizeGen++
	a.view.pendingW = tw
	return a.after(resizeDebounce, resizeMsg{gen: a.view.resizeGen})
}

// applyListWidth gives the transcript list the layout's current size.
func (a *App) applyListWidth() {
	a.w.list.SetSize(a.lay.Transcript.W, a.lay.Transcript.H)
	a.view.listW, a.view.listH = a.lay.Transcript.W, a.lay.Transcript.H
	a.view.pendingW = a.view.listW
}

// sync rebuilds the status bar and the sidebar from the state.
func (a *App) sync() {
	a.w.status.Set(a.statusState())
	if a.lay.SideVisible {
		a.w.side.SetSections(a.sess.sections(a.opts.WorkDir, a.opts.Aliases))
	}
}

// statusState is the full status bar state.
func (a *App) statusState() statusbar.State {
	st := a.sess.status(a.opts.Aliases)
	st.Mode = a.mode.String()
	st.Untrusted = a.opts.Untrusted
	st.Hint = a.view.hint
	return st
}

// setMode switches the input mode; only INSERT focuses the prompt.
func (a *App) setMode(m mode) tea.Cmd {
	a.mode = m
	if m == modeInsert {
		return a.w.prompt.Focus()
	}
	a.w.prompt.Blur()
	return nil
}

// cycleAgent moves to the next (1) or previous (-1) primary agent and, on
// an existing session, configures it there.
func (a *App) cycleAgent(delta int) tea.Cmd {
	if !a.sess.cycleAgent(delta) {
		return nil
	}
	a.w.prompt.SetAgent(ansi.SanitizeLine(a.sess.info.Agent))
	if a.sess.info.ID == "" {
		return nil
	}
	return configureCmd(a.ctx, a.ports, a.sess.info.ID, a.sess.info.Agent, "")
}

// runAction runs a remappable action bound to a key.
func (a *App) runAction(id actions.ID) tea.Cmd {
	switch id {
	case actions.ViewSidebar:
		show := !a.lay.SideVisible
		a.view.sidebarPref = &show
		return prefsUpdateCmd(a.ports, func(p *core.Prefs) { p.Sidebar = &show })
	case actions.PromptEditor:
		return editorCmd(a.ports, a.w.prompt.Value())
	case actions.RunCancel:
		return a.sender().cancelRun()
	case actions.AppQuit:
		return a.quit()
	}
	return nil
}

// quit cancels the base context (and with it any run and port call in
// flight), closes the bus subscription, and ends the program.
func (a *App) quit() tea.Cmd {
	a.cancel()
	if a.sub != nil {
		a.sub.Close()
	}
	return tea.Quit
}

// sender returns the send/queue/cancel handler.
func (a *App) sender() sender { return sender{a} }
