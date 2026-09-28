package ui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/theme"
)

// loadItems runs the App's loader for level and returns the items.
func loadItems(ta *testApp, level picker.Level) []picker.Item {
	ta.t.Helper()
	cmd := levels{ta.app}.load(level)
	if cmd == nil {
		ta.t.Fatalf("load(%s) returned no Cmd", level.ID)
	}
	msg, ok := cmd().(picker.ItemsMsg)
	if !ok || msg.Err != nil || msg.Level != level.ID {
		ta.t.Fatalf("load(%s) = %#v, want its ItemsMsg", level.ID, msg)
	}
	return msg.Items
}

// itemByID finds id among items.
func itemByID(t *testing.T, items []picker.Item, id string) picker.Item {
	t.Helper()
	i := slices.IndexFunc(items, func(it picker.Item) bool { return it.ID == id })
	if i < 0 {
		t.Fatalf("no item %q among %d items", id, len(items))
	}
	return items[i]
}

// twoModelCatalog has a configured anthropic (sonnet, opus) and an
// unconfigured openai (gpt-6).
func twoModelCatalog() fakeCatalog {
	return fakeCatalog{
		{Configured: true, Info: core.ProviderInfo{ID: "anthropic", Name: "Anthropic", Models: []core.ModelInfo{
			{Ref: core.ModelRef{Provider: "anthropic", Model: "claude-sonnet-5"}, Name: "Claude Sonnet 5", ContextWindow: 200000, CostIn: 3, CostOut: 15},
			{Ref: core.ModelRef{Provider: "anthropic", Model: "claude-opus-5-5"}, Name: "Claude Opus 5.5", ContextWindow: 1000000, CostIn: 5, CostOut: 25},
		}}},
		{Configured: false, Info: core.ProviderInfo{ID: "openai", Name: "OpenAI", Models: []core.ModelInfo{
			{Ref: core.ModelRef{Provider: "openai", Model: "gpt-6"}, Name: "GPT-6", ContextWindow: 400000, CostIn: 1.25, CostOut: 10},
		}}},
	}
}

// resumed is a stored session on anthropic/claude-sonnet-5.
func resumed() core.Session {
	return core.Session{ID: "ses_r", Title: "Old title", Agent: "build", Model: "anthropic/claude-sonnet-5", Cwd: testWorkDir}
}

func TestPicker_RootListsActionsWithKeysAndRecent(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withPrefs(core.Prefs{Recent: []string{"model.switch", "gone.action"}}))
	ta.key("ctrl+p")
	if !ta.app.w.picker.IsOpen() || ta.app.mode != modePicker {
		t.Fatalf("ctrl+p: open=%v mode=%v; want the picker", ta.app.w.picker.IsOpen(), ta.app.mode)
	}
	items := loadItems(ta, rootLevel())
	side := itemByID(t, items, "view.sidebar")
	if side.Title != "Toggle sidebar" || !strings.Contains(side.Detail, "ctrl+b") || side.Group != "View" {
		t.Errorf("view.sidebar item = %+v, want its title, group, and ctrl+b", side)
	}
	if it := itemByID(t, items, "model.switch"); it.Drill == nil || it.Drill.ID != levelModels {
		t.Errorf("model.switch drill = %+v, want the models level", it.Drill)
	}
	if it := itemByID(t, items, "prompt.attach"); it.Drill == nil || it.Drill.ID != levelFiles || !it.Drill.Multi {
		t.Errorf("prompt.attach drill = %+v, want the multi files level", it.Drill)
	}
	if it := itemByID(t, items, "session.rename"); it.Drill != nil && !it.Disabled {
		t.Errorf("session.rename with no session = %+v, want disabled", it)
	}
	view := xansi.Strip(ta.view())
	recent := strings.Index(view, "Recent")
	session := strings.Index(view, "Open session…")
	if recent < 0 || session < 0 || recent > session {
		t.Errorf("want a Recent group before the Session group:\n%s", view)
	}
	if !strings.Contains(view, "Switch model…") {
		t.Errorf("the recent action is not listed:\n%s", view)
	}
	ta.key("esc")
	if ta.app.w.picker.IsOpen() || ta.app.mode != modeInsert {
		t.Errorf("esc: open=%v mode=%v; want INSERT again", ta.app.w.picker.IsOpen(), ta.app.mode)
	}

	// ctrl+t opens it too, from NORMAL, and closing returns to NORMAL.
	ta.key("esc")
	ta.key("ctrl+t")
	if ta.app.mode != modePicker {
		t.Fatalf("ctrl+t in NORMAL: mode = %v", ta.app.mode)
	}
	ta.key("esc")
	if ta.app.mode != modeNormal {
		t.Errorf("closing from NORMAL: mode = %v, want NORMAL", ta.app.mode)
	}
}

func TestKeysDetail_SanitizesKeys(t *testing.T) {
	t.Parallel()
	// actions.Resolve already rejects an unprintable config key (see
	// TestResolve_RejectsUnprintableKey), but keysDetail sanitizes on
	// its own too: a key string always reaches the picker's Detail
	// (rootItems/keyItems) sanitized, regardless of where it came from.
	got := keysDetail([]string{"ctrl+p", "\x1b]52;c;evil\x07", "y"})
	if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\x07') {
		t.Errorf("keysDetail = %q, want no control runes", got)
	}
}

func TestPicker_HelpKeysListsEveryActionReadOnly(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.key("esc")
	ta.key("?")
	if !ta.app.w.picker.IsOpen() {
		t.Fatal("? did not open the keys list")
	}
	items := loadItems(ta, picker.Level{ID: levelKeys})
	if len(items) != len(ta.app.opts.Actions.All()) {
		t.Errorf("keys lists %d items, want every action (%d)", len(items), len(ta.app.opts.Actions.All()))
	}
	for _, it := range items {
		if !it.Disabled {
			t.Errorf("keys item %q is choosable", it.ID)
		}
	}
	if it := itemByID(t, items, "prompt.editor"); !strings.Contains(it.Detail, "ctrl+e") {
		t.Errorf("prompt.editor detail = %q, want ctrl+e", it.Detail)
	}
}

func TestPicker_ModelSwitchConfigures(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(twoModelCatalog()), withResume(resumed(), nil, nil))
	models := loadItems(ta, picker.Level{ID: levelModels})
	sonnet := itemByID(t, models, "anthropic/claude-sonnet-5")
	if sonnet.Group != "Anthropic" || sonnet.Detail != "200k ctx · $3/$15" || !sonnet.Current || sonnet.Disabled {
		t.Errorf("sonnet = %+v", sonnet)
	}
	if opus := itemByID(t, models, "anthropic/claude-opus-5-5"); opus.Detail != "1M ctx · $5/$25" || opus.Current {
		t.Errorf("opus = %+v", opus)
	}
	if gpt := itemByID(t, models, "openai/gpt-6"); !gpt.Disabled || gpt.Group != "OpenAI" || gpt.Detail != "400k ctx · $1.25/$10" {
		t.Errorf("gpt = %+v, want disabled under OpenAI", gpt)
	}

	ta.key("ctrl+p")
	ta.typeText("switch model")
	ta.key("enter")
	ta.typeText("opus")
	ta.key("enter")
	if ta.app.w.picker.IsOpen() || ta.app.mode != modeInsert {
		t.Fatalf("after choosing: open=%v mode=%v", ta.app.w.picker.IsOpen(), ta.app.mode)
	}
	if ta.app.sess.info.Model != "anthropic/claude-opus-5-5" {
		t.Errorf("model = %q, want opus", ta.app.sess.info.Model)
	}
	want := []configureCall{{ID: "ses_r", Model: "anthropic/claude-opus-5-5"}}
	if !slices.Equal(ta.sessions.configure, want) {
		t.Errorf("configure = %+v, want %+v", ta.sessions.configure, want)
	}
}

func TestPicker_ModelSwitchWithoutSessionOnlySetsIt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(twoModelCatalog()))
	ta.key("ctrl+p")
	ta.typeText("switch model")
	ta.key("enter")
	ta.typeText("opus")
	ta.key("enter")
	if ta.app.sess.info.Model != "anthropic/claude-opus-5-5" || len(ta.sessions.configure) != 0 {
		t.Errorf("model = %q configure = %+v; want opus and no Configure", ta.app.sess.info.Model, ta.sessions.configure)
	}
	ta.typeText("hi")
	ta.key("enter")
	if got := ta.chat.sends[0].Model; got != "anthropic/claude-opus-5-5" {
		t.Errorf("new session sent with model %q", got)
	}
}

func TestPicker_DisabledProviderNotChosen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(twoModelCatalog()), withResume(resumed(), nil, nil))
	ta.key("ctrl+p")
	ta.typeText("switch model")
	ta.key("enter")
	ta.typeText("gpt")
	ta.key("enter")
	if !ta.app.w.picker.IsOpen() {
		t.Error("choosing a disabled model closed the picker")
	}
	if ta.app.sess.info.Model != "anthropic/claude-sonnet-5" || len(ta.sessions.configure) != 0 {
		t.Errorf("model = %q configure = %+v; want unchanged", ta.app.sess.info.Model, ta.sessions.configure)
	}
}

func TestPicker_SessionsOpenResumes(t *testing.T) {
	t.Parallel()
	now := testStart()
	others := []core.Session{
		{ID: "ses_a", Title: "Alpha \x1b[31mred", Agent: "build", UpdatedAt: now.Add(-30 * time.Second)},
		{ID: "ses_b", Title: "Beta", Agent: "plan", Model: "anthropic/claude-opus-5-5", UpdatedAt: now.Add(-5 * time.Minute)},
		{ID: "ses_c", Title: "Gamma", UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "ses_d", Title: "Delta", UpdatedAt: now.Add(-50 * time.Hour)},
	}
	msgs := map[core.SessionID][]core.Message{
		"ses_a": nil,
		"ses_b": {
			{ID: "u1", SessionID: "ses_b", Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "beta question"}}},
			{ID: "a1", SessionID: "ses_b", Role: core.RoleAssistant, CostUSD: 0.40, Parts: []core.Part{{Kind: core.PartText, Text: "beta answer"}}},
			{ID: "a2", SessionID: "ses_b", Role: core.RoleAssistant, CostUSD: 0.02, Parts: []core.Part{{Kind: core.PartText, Text: "more"}}},
		},
		"ses_c": nil, "ses_d": nil,
	}
	ta := newTestApp(t, withSessions(others, msgs))
	items := loadItems(ta, picker.Level{ID: levelSessions})
	if ta.sessions.listCwd != testWorkDir {
		t.Errorf("ListForCwd(%q), want the workdir", ta.sessions.listCwd)
	}
	wantDetail := map[string]string{
		"ses_a": "just now · $0.00", "ses_b": "5m ago · $0.42",
		"ses_c": "3h ago · $0.00", "ses_d": "2d ago · $0.00",
	}
	for id, d := range wantDetail {
		if it := itemByID(t, items, id); it.Detail != d {
			t.Errorf("%s detail = %q, want %q", id, it.Detail, d)
		}
	}
	if a := itemByID(t, items, "ses_a"); strings.ContainsRune(a.Title, '\x1b') {
		t.Errorf("title %q not sanitized", a.Title)
	}

	ta.key("ctrl+p")
	ta.typeText("open session")
	ta.key("enter")
	ta.typeText("Beta")
	ta.key("enter")
	if ta.app.sess.info.ID != "ses_b" {
		t.Fatalf("session = %q, want ses_b", ta.app.sess.info.ID)
	}
	if ta.app.sess.info.Agent != "plan" || ta.app.sess.info.Model != "anthropic/claude-opus-5-5" {
		t.Errorf("agent/model = %q/%q, want the session's", ta.app.sess.info.Agent, ta.app.sess.info.Model)
	}
	if got := len(ta.app.sess.proj.Blocks()); got != 3 {
		t.Errorf("%d blocks loaded, want 3", got)
	}
	if cur := itemByID(t, loadItems(ta, picker.Level{ID: levelSessions}), "ses_b"); !cur.Current {
		t.Error("the open session is not marked current")
	}
	ta.typeText("next")
	ta.key("enter")
	if got := ta.chat.sends[0].SessionID; got != "ses_b" {
		t.Errorf("sent to %q, want the opened session", got)
	}
}

func TestPicker_SessionNewClears(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(resumed(), []core.Message{
		{ID: "u1", SessionID: "ses_r", Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "q"}}},
	}, nil))
	if len(ta.app.sess.proj.Blocks()) != 1 {
		t.Fatal("resume did not load")
	}
	ta.key("ctrl+p")
	ta.typeText("new session")
	ta.key("enter")
	if ta.app.sess.info.ID != "" || len(ta.app.sess.proj.Blocks()) != 0 {
		t.Errorf("session = %q blocks = %d; want a cleared new session", ta.app.sess.info.ID, len(ta.app.sess.proj.Blocks()))
	}
	ta.typeText("fresh")
	ta.key("enter")
	if s := ta.chat.sends[0]; s.SessionID != "" || s.Agent != "build" {
		t.Errorf("send = %+v, want a new session on build", s)
	}
}

func TestPicker_AgentSwitch(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(resumed(), nil, nil))
	items := loadItems(ta, picker.Level{ID: levelAgents})
	if !itemByID(t, items, "build").Current || itemByID(t, items, "plan").Current {
		t.Errorf("agents = %+v, want build current", items)
	}
	ta.key("ctrl+p")
	ta.typeText("switch agent")
	ta.key("enter")
	ta.typeText("plan")
	ta.key("enter")
	if ta.app.sess.info.Agent != "plan" {
		t.Errorf("agent = %q, want plan", ta.app.sess.info.Agent)
	}
	want := []configureCall{{ID: "ses_r", Agent: "plan"}}
	if !slices.Equal(ta.sessions.configure, want) {
		t.Errorf("configure = %+v, want %+v", ta.sessions.configure, want)
	}
}

func TestApp_ThemePreviewEscRestores(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	orig := ta.app.theme.current
	ta.key("ctrl+p")
	ta.typeText("switch theme")
	ta.key("enter")
	ta.typeText("nord")
	if got := ta.app.theme.current.Name; got != "Nord" {
		t.Fatalf("previewed theme = %q, want Nord", got)
	}
	previewVersion := ta.app.theme.version
	ta.key("esc")
	if ta.app.theme.current.Name != orig.Name {
		t.Errorf("after esc: theme = %q, want %q", ta.app.theme.current.Name, orig.Name)
	}
	if ta.app.theme.version <= previewVersion {
		t.Errorf("version = %d, want bumped past the preview's %d", ta.app.theme.version, previewVersion)
	}
	if want := theme.Build(orig, ta.app.theme.version); !reflect.DeepEqual(ta.app.theme.set, want) {
		t.Error("restored styles differ from the original palette's")
	}
	if p := ta.prefs.Get().Theme; p != "" {
		t.Errorf("prefs.Theme = %q after esc, want untouched", p)
	}
}

// lineContaining returns the first line of s containing substr.
func lineContaining(s, substr string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

func TestPicker_ThemesStartOnCurrentNoPreviewFlash(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	origVersion := ta.app.theme.version
	ta.key("ctrl+p")
	ta.typeText("switch theme")
	ta.key("enter")
	// Opening the level must not itself preview the first (alphabetical)
	// palette: only moving the highlight away from the current one does.
	if ta.app.theme.version != origVersion || ta.app.theme.current.Name != "Dark" {
		t.Fatalf("theme = %q version = %d, want Dark unchanged at version %d",
			ta.app.theme.current.Name, ta.app.theme.version, origVersion)
	}
	line := lineContaining(xansi.Strip(ta.view()), "Dark")
	if !strings.Contains(line, "❯") {
		t.Errorf("current theme's row = %q, want the cursor already on it", line)
	}

	// Moving away previews; moving back restores exactly.
	ta.key("down")
	if ta.app.theme.current.Name == "Dark" {
		t.Fatal("moving the highlight did not preview a different theme")
	}
	ta.key("up")
	if got := ta.app.theme.current.Name; got != "Dark" {
		t.Errorf("theme after moving back = %q, want Dark", got)
	}
}

func TestApp_ThemeChoosePersists(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.key("ctrl+p")
	ta.typeText("switch theme")
	ta.key("enter")
	ta.typeText("nord")
	ta.key("enter")
	if ta.app.theme.current.Name != "Nord" {
		t.Errorf("theme = %q, want Nord kept", ta.app.theme.current.Name)
	}
	if p := ta.prefs.Get().Theme; p != "Nord" {
		t.Errorf("prefs.Theme = %q, want Nord", p)
	}
}

func TestPicker_RenameCallsPort(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(resumed(), nil, nil))
	it := itemByID(t, loadItems(ta, rootLevel()), "session.rename")
	if it.Drill == nil || !it.Drill.Input || it.Drill.Initial != "Old title" || it.Drill.ID != levelRename {
		t.Fatalf("rename drill = %+v, want an input level prefilled with the title", it.Drill)
	}
	ta.key("ctrl+p")
	ta.typeText("rename")
	ta.key("enter")
	for range len("title") {
		ta.key("backspace")
	}
	ta.typeText("name")
	ta.key("enter")
	want := []renameCall{{ID: "ses_r", Title: "Old name"}}
	if !slices.Equal(ta.sessions.renames, want) {
		t.Errorf("renames = %+v, want %+v", ta.sessions.renames, want)
	}
}

func TestPicker_KeyRunActionsAreRecorded(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.key("ctrl+b") // remappable (view.sidebar), bound in both modes
	want := []string{"view.sidebar"}
	if got := ta.prefs.Get().Recent; !slices.Equal(got, want) {
		t.Fatalf("Recent after ctrl+b = %v, want %v", got, want)
	}

	ta.key("esc")
	ta.key("?") // remappable (help.keys), opens the keys level
	want = []string{"help.keys", "view.sidebar"}
	if got := ta.prefs.Get().Recent; !slices.Equal(got, want) {
		t.Fatalf("Recent after ? = %v, want %v", got, want)
	}
	ta.key("esc")

	// Opening the picker itself (ctrl+p/ctrl+t) is not an action a user
	// chose from it — it never appears in the root list — so it must not
	// occupy a recent slot.
	ta.key("ctrl+p")
	ta.key("esc")
	if got := ta.prefs.Get().Recent; !slices.Equal(got, want) {
		t.Errorf("Recent after ctrl+p = %v, want unchanged %v", got, want)
	}
}

func TestPicker_RecentSaved(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withPrefs(core.Prefs{Recent: []string{"a", "b", "c", "d", "view.sidebar"}}))
	ta.key("ctrl+p")
	ta.typeText("toggle sidebar")
	ta.key("enter")
	want := []string{"view.sidebar", "a", "b", "c", "d"}
	if got := ta.prefs.Get().Recent; !slices.Equal(got, want) {
		t.Errorf("Recent = %v, want %v", got, want)
	}
	ta.key("ctrl+p")
	ta.typeText("switch theme")
	ta.key("enter")
	ta.typeText("nord")
	ta.key("enter")
	want = []string{"view.theme", "view.sidebar", "a", "b", "c"}
	if got := ta.prefs.Get().Recent; !slices.Equal(got, want) {
		t.Errorf("Recent = %v, want %v", got, want)
	}
	if !slices.Equal(ta.app.view.pick.recent, want) {
		t.Errorf("picker recent = %v, want %v", ta.app.view.pick.recent, want)
	}
}

func TestPicker_CompactBusyHint(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		hint string
	}{
		{core.ErrBusy, "session is busy"},
		{errors.New("session: nothing to compact"), "nothing to compact"},
		{errors.New("chat: service closed"), "compact: chat: service closed"},
	} {
		ta := newTestApp(t, withResume(resumed(), nil, nil))
		ta.chat.compactErr = tc.err
		ta.key("ctrl+p")
		ta.typeText("compact")
		ta.key("enter")
		if !slices.Equal(ta.chat.compacts, []core.SessionID{"ses_r"}) {
			t.Errorf("compacts = %v, want ses_r", ta.chat.compacts)
		}
		if got := ta.app.statusState().Hint; got != tc.hint {
			t.Errorf("%v: hint = %q, want %q", tc.err, got, tc.hint)
		}
	}
}

func TestPicker_CompactReloadsOnSuccess(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(resumed(), nil, nil))
	ta.sessions.msgs = []core.Message{{ID: "a1", SessionID: "ses_r", Role: core.RoleAssistant,
		Parts: []core.Part{{Kind: core.PartCompaction, Text: "the summary"}}}}
	ta.key("ctrl+p")
	ta.typeText("compact")
	ta.key("enter")
	if got := len(ta.app.sess.proj.Blocks()); got != 1 {
		t.Errorf("%d blocks after compaction, want the summary notice", got)
	}
}

func TestPicker_GoldenRoot(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withPrefs(core.Prefs{Recent: []string{"model.switch"}}))
	ta.key("ctrl+p")
	golden.Assert(t, "picker_root", ta.view())
}

func TestPicker_GoldenModels(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withCatalog(twoModelCatalog()))
	ta.key("ctrl+p")
	ta.typeText("switch model")
	ta.key("enter")
	golden.Assert(t, "picker_models", ta.view())
}
