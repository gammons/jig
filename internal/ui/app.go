package ui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/bubbles/overlay"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/bubbles/statusbar"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/actions"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// Timing (spec §5.2): streaming re-renders at most every 80 ms, and never
// sooner than 3× the last streaming render pass took (capped at 1 s), so
// a huge growing block can't keep the loop busy; a width change
// re-renders every transcript block, so bursts of resizes are coalesced
// into one list resize 50 ms after the last.
const (
	streamInterval    = 80 * time.Millisecond
	maxStreamInterval = time.Second
	resizeDebounce    = 50 * time.Millisecond
	overlayDim        = 0.4
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
	DefaultModel        string            // resolved default_model ("provider/model"), for the status bar
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

// viewState is the App's presentation state: the sidebar preference, whether the details split is open, the
// status hint, the project's prompt history, the streamTick's state
// (stream), and the transcript list's applied size (listW/listH) and a
// pending debounced width (pendingW, keyed by resizeGen). keyPrefix holds
// a pending NORMAL g-prefix ("g", awaiting its second key); searching is
// whether the one-line search input owns the status bar's slot;
// detailsFor is the block ID the open details split shows, so an async
// detailsMsg for a block the selection has since left can be ignored.
// pick is the picker's state (pickerView). resumeHeld is a session whose
// resume result arrived mid-send; it is re-read once idle.
type viewState struct {
	pick         pickerView
	sidebarPref  *bool
	detailsOpen  bool
	hint         string
	history      []string
	stream       streamState
	resizeGen    int
	pendingW     int
	listW, listH int
	keyPrefix    string
	searching    bool
	detailsFor   transcript.BlockID
	resumeHeld   core.SessionID
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
	img           *imageState
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
		sess:  newSessionState(o.Session, o.Clock, o.DefaultModel),
		theme: newThemeState(o.Theme, o.Themes),
		img:   newImageState(imgrender.Detect(o.Images, ""), o.Tmux),
		after: tick,
	}
	a.w = newWidgets(&a.theme.set, func(text string) tea.Cmd { return editorCmd(a.ports, text) }, levels{a}.load, replyFunc(a))
	a.w.list.SetHighlight(false) // the App starts in INSERT
	if p.Subscribe != nil {
		a.sub = p.Subscribe()
	}
	return a
}

// tick is tea.Tick yielding msg after d.
func tick(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
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
	prev := a.lay
	layout := tea.Batch(a.relayout(), a.sync())
	if a.lay != prev {
		// The details pane moved or resized: draw a shown sixel again.
		layout = tea.Batch(layout, placeSixel(a))
	}
	return a, tea.Batch(cmd, layout)
}

// View renders the frame: the regions per the layout, and the picker
// composited over them while it is open. The theme's Background and Text
// become the terminal's default colors (restored on exit), so the whole
// screen follows the theme, not just the cells a widget styles.
func (a *App) View() tea.View {
	v := tea.View{
		AltScreen:       true,
		WindowTitle:     a.windowTitle(),
		BackgroundColor: a.theme.set.Background,
		ForegroundColor: a.theme.set.Foreground,
	}
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
	status := a.w.status.View()
	if a.view.searching {
		status = a.w.search.View()
	}
	v.Content = compose(a.lay, a.w.list.View(), side, a.w.prompt.View(), status)
	if a.w.picker.IsOpen() {
		v.Content = overlay.Center(v.Content, a.width, a.height, a.w.picker.View(), overlayDim)
	}
	return v
}

// onEvent reconciles one bus event and re-arms the bridge.
func (a *App) onEvent(ev event.Event) tea.Cmd {
	res := a.sess.apply(ev)
	if res.reload {
		a.w.setItems(a.sess.allItems())
	}
	a.flush(res.upsert)
	cmds := []tea.Cmd{waitEvent(a.sub)}
	if res.settled {
		cmds = append(cmds, a.sender().afterRun())
	}
	if a.sess.info.ID == "" || ev.Root() != a.sess.info.ID {
		return tea.Batch(cmds...)
	}
	if e, ok := ev.(event.PermissionRequested); ok {
		cmds = append(cmds, permCtl{a}.sync(), permCtl{a}.requested(e))
	}
	if ev.Session() != a.sess.info.ID {
		detailsCtl{a}.childEvent()
	}
	return tea.Batch(cmds...)
}

// onTick renders the dirty streaming blocks and advances the spinners in
// one Upsert, and reschedules itself while the run lasts, after a delay
// stretched by how long that render pass took (nextInterval).
func (a *App) onTick() tea.Cmd {
	ids := a.sess.tick()
	start := a.opts.Clock.Now()
	a.flush(ids)
	took := a.opts.Clock.Now().Sub(start)
	refresh := detailsCtl{a}.refresh()
	if a.sess.run.running {
		return tea.Batch(refresh, a.after(nextInterval(took), streamTickMsg{}))
	}
	a.view.stream.ticking = false
	return refresh
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
		return pickerCtl{a}.open(filesLevel(), true)
	case picker.ItemsMsg, picker.ChosenMsg, picker.InputMsg, picker.ClosedMsg, themePreviewMsg, themeApplyMsg:
		return pickerCtl{a}.handle(msg)
	case compactedMsg:
		return pickerCtl{a}.compacted(msg)
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
	case detailsMsg:
		return detailsCtl{a}.result(msg)
	case cardArmMsg:
		permCtl{a}.armed(msg)
	case tea.TerminalVersionMsg:
		if p := imgrender.Detect(a.opts.Images, msg.Name); p != a.img.r.Protocol() {
			a.img = newImageState(p, a.opts.Tmux)
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
		a.view.pick.recent = slices.Clone(msg.prefs.Recent[:min(len(msg.prefs.Recent), maxRecent)])
		a.w.picker.SetRecent(a.view.pick.recent)
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
		if a.sess.run.busy() {
			// A send started first (a Load now would drop its blocks):
			// re-read the session once idle (sender.idle).
			a.view.resumeHeld = msg.info.ID
			return
		}
		if msg.info.ID != a.sess.info.ID {
			// A session opened from the picker replaces this one.
			a.sess = freshSession(a.sess, msg.info.ID)
			a.view.detailsOpen = false
		}
		a.sess.load(msg.info, msg.msgs, msg.todos)
		a.w.setItems(a.sess.allItems())
		a.w.prompt.SetAgent(ansi.SanitizeLine(a.sess.info.Agent))
	case errMsg:
		a.view.hint = msg.hint()
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
	a.w.search.SetWidth(max(a.lay.Status.W-2, 0))
	a.w.side.SetSize(a.lay.Side.W, a.lay.Side.H)
	a.w.details.SetSize(a.lay.Side.W, a.lay.Side.H)
	a.w.picker.SetSize(a.width, a.height)
	a.w.confirm.SetSize(a.width, a.height)

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

// sync points the permission card at its request (returning its arming
// tick, if any) and rebuilds the status bar and the sidebar from the state.
func (a *App) sync() tea.Cmd {
	cmd := permCtl{a}.sync()
	a.w.status.Set(a.statusState())
	if a.lay.SideVisible {
		a.w.side.SetSections(sidebarSections(a))
	}
	return cmd
}

// statusState is the full status bar state.
func (a *App) statusState() statusbar.State {
	st := a.sess.status(a.opts.Aliases)
	st.Mode = a.mode.String()
	st.Untrusted = a.opts.Untrusted
	st.Hint = a.view.hint
	st.Pending = len(a.sess.proj.Pending())
	if st.Hint == "" && st.Pending > 0 && (a.mode != modeNormal || !permCtl{a}.onCard()) {
		st.Hint = permissionHint
	}
	return st
}

// setMode switches the input mode; only INSERT focuses the prompt, and
// only NORMAL highlights the selected block (the picker keeps the prior).
func (a *App) setMode(m mode) tea.Cmd {
	a.mode = m
	if m != modePicker {
		a.w.list.SetHighlight(m == modeNormal)
	}
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

// runAction runs a remappable action bound to a key or chosen from the
// picker's root list, then records it as recent (spec §6.4) — except
// picker.open, which never appears in that list and would otherwise fill
// it with nothing but itself every time ctrl+p is pressed.
func (a *App) runAction(id actions.ID) tea.Cmd {
	cmd := dispatchAction(a, id)
	if id == actions.PickerOpen {
		return cmd
	}
	return tea.Batch(cmd, pickerCtl{a}.remember(id))
}

// dispatchAction is runAction's switch, split out so every case funnels
// through runAction's single remember call above, whether reached by a
// key (via Keymap.Lookup) or a picker choice (pickerCtl.chosen).
func dispatchAction(a *App, id actions.ID) tea.Cmd {
	switch id {
	case actions.ViewSidebar:
		show := !a.lay.SideVisible
		a.view.sidebarPref = &show
		return prefsUpdateCmd(a.ports, func(p *core.Prefs) { p.Sidebar = &show })
	case actions.PromptEditor:
		return editorCmd(a.ports, a.w.prompt.Value())
	case actions.RunCancel:
		return a.sender().cancelRun()
	case actions.TranscriptSearch:
		return normalKeys{a}.openSearch()
	case actions.TranscriptYank:
		return normalKeys{a}.yank()
	case actions.TranscriptDetails:
		return normalKeys{a}.toggleDetails()
	case actions.PickerOpen:
		return pickerCtl{a}.open(rootLevel(), false)
	case actions.AppQuit:
		return a.quit()
	}
	return pickerCtl{a}.action(id)
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
