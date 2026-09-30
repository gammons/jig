package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
)

// pendingHint is the status hint shown when a request arrives while the
// user is typing (spec §6.5, verbatim).
const pendingHint = "⚠ permission pending · esc gp"

// startBash delivers a root bash call id running cmd.
func (ta *testApp) startBash(id, cmd string) {
	ta.t.Helper()
	call := core.ToolCall{ID: id, Name: "bash", Input: []byte(`{"command":"` + cmd + `"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
}

// request delivers a root permission request req for bash call id.
func (ta *testApp) request(req, id, subject string) {
	ta.t.Helper()
	ta.event(event.PermissionRequested{
		Base: rootBase(), RequestID: req, Tool: "bash", Subject: subject,
		Call: core.ToolCall{ID: id, Name: "bash"},
	})
}

// selectedID is the transcript list's selected block ID ("" for none).
func (ta *testApp) selectedID() string {
	it, ok := ta.app.sess.main.list.Selected()
	if !ok {
		return ""
	}
	return it.ID
}

func TestApp_PermissionFocusesWhenPromptEmpty(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("clean up")
	ta.startBash("c1", "rm -rf build")
	ta.startBash("c2", "ls")
	ta.request("p1", "c1", "rm -rf build")

	if ta.app.mode != modeNormal {
		t.Fatalf("mode = %v, want NORMAL", ta.app.mode)
	}
	if got := ta.selectedID(); got != "t/c1" {
		t.Fatalf("selected = %q, want the requesting block t/c1", got)
	}
	req := ta.app.w.card.Request()
	if req == nil || req.ID != "p1" || req.Subject != "rm -rf build" {
		t.Fatalf("card request = %+v, want p1 for rm -rf build", req)
	}
	if st := ta.app.statusState(); st.Pending != 1 {
		t.Errorf("status Pending = %d, want 1", st.Pending)
	}
	ta.arm()
	view := xansi.Strip(ta.view())
	if !strings.Contains(view, "bash wants to run:  rm -rf build") {
		t.Errorf("view has no card line:\n%s", view)
	}
	if !strings.Contains(view, "a allow · A always (this exact command) · d deny · D deny with message") {
		t.Errorf("view has no card hint line:\n%s", view)
	}
}

// arm delivers the pending card-arming ticks (only those).
func (ta *testApp) arm() {
	ta.t.Helper()
	var rest, arms []deferredMsg
	for _, d := range ta.deferred {
		if _, ok := d.msg.(cardArmMsg); ok {
			arms = append(arms, d)
			continue
		}
		rest = append(rest, d)
	}
	ta.deferred = rest
	for _, d := range arms {
		ta.send(d.msg)
	}
}

func TestApp_CardKeysWaitForArming(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "make")
	ta.startBash("c2", "ls")
	ta.request("p1", "c1", "make") // empty prompt: focus jumps to the card

	ticks := deferredOf[cardArmMsg](ta)
	if len(ticks) == 0 || ticks[len(ticks)-1].d != cardArmDelay {
		t.Fatalf("arm ticks = %+v, want the last at %v", ticks, cardArmDelay)
	}
	if cardArmDelay != 400*time.Millisecond {
		t.Errorf("cardArmDelay = %v, want 400ms", cardArmDelay)
	}
	view := xansi.Strip(ta.view())
	if strings.Contains(view, "a allow") {
		t.Errorf("the key legend shows before the card is armed:\n%s", view)
	}
	ta.key("a")
	ta.key("d")
	if len(ta.perms.replies) != 0 || ta.app.mode != modeNormal {
		t.Fatalf("keys before arming: replies %+v, mode %v; want none, NORMAL", ta.perms.replies, ta.app.mode)
	}

	ta.arm()
	if !strings.Contains(xansi.Strip(ta.view()), "a allow · A always (this exact command) · d deny · D deny with message") {
		t.Errorf("the armed card lacks its key legend:\n%s", xansi.Strip(ta.view()))
	}
	// Moving off the card and back onto it disarms it again.
	ta.key("j")
	ta.key("k")
	if got := ta.selectedID(); got != "t/c1" {
		t.Fatalf("selected = %q, want t/c1", got)
	}
	ta.key("a")
	if len(ta.perms.replies) != 0 {
		t.Fatalf("a right after reselecting the card replied: %+v", ta.perms.replies)
	}
	ta.arm()
	ta.key("a")
	want := []permReply{{ID: "p1", Reply: core.PermissionReply{Kind: core.ReplyOnce}}}
	if len(ta.perms.replies) != 1 || ta.perms.replies[0] != want[0] {
		t.Errorf("replies = %+v, want %+v", ta.perms.replies, want)
	}
}

// A new request that switches INSERT → NORMAL onto a block already
// showing an older, armed request must disarm the card: the key the user
// was typing can't answer the old request.
func TestApp_RequestFocusDisarmsArmedCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "make")
	ta.startBash("c2", "ls")
	ta.request("p1", "c1", "make")
	ta.arm()
	ta.key("i")
	if ta.app.mode != modeInsert || ta.app.w.prompt.Value() != "" {
		t.Fatalf("setup: mode %v prompt %q, want INSERT and empty", ta.app.mode, ta.app.w.prompt.Value())
	}
	ta.request("p2", "c2", "ls")
	if ta.app.mode != modeNormal || ta.selectedID() != "t/c1" {
		t.Fatalf("mode %v selected %q, want NORMAL on t/c1", ta.app.mode, ta.selectedID())
	}
	ta.key("a")
	if len(ta.perms.replies) != 0 {
		t.Fatalf("a right after the focus switch replied: %+v", ta.perms.replies)
	}
	if ta.app.mode != modeNormal {
		t.Fatalf("mode = %v after a on the disarmed card, want NORMAL", ta.app.mode)
	}
	ta.arm()
	ta.key("a")
	want := permReply{ID: "p1", Reply: core.PermissionReply{Kind: core.ReplyOnce}}
	if len(ta.perms.replies) != 1 || ta.perms.replies[0] != want {
		t.Errorf("replies = %+v, want [%+v]", ta.perms.replies, want)
	}
}

func TestApp_PermissionReplyErrorPrefixedOnce(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "make")
	ta.request("p1", "c1", "make")
	ta.arm()
	ta.perms.err = errors.New(`permission: unknown request "p1"`)
	ta.key("a")
	if got, want := ta.app.view.hint, `permission: unknown request "p1"`; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
}

func TestApp_PermissionDoesNotStealFocusWhileTyping(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("clean up")
	ta.startBash("c1", "rm -rf build")
	ta.typeText("half typed")
	ta.request("p1", "c1", "rm -rf build")

	if ta.app.mode != modeInsert {
		t.Fatalf("mode = %v, want INSERT", ta.app.mode)
	}
	if got := ta.app.w.prompt.Value(); got != "half typed" {
		t.Fatalf("prompt = %q, want it unchanged", got)
	}
	if got := ta.app.view.hint; got != pendingHint {
		t.Fatalf("hint = %q, want %q", got, pendingHint)
	}
	if !strings.Contains(xansi.Strip(ta.view()), pendingHint) {
		t.Error("the status bar does not show the pending hint")
	}
}

func TestApp_CardKeysReply(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "make")
	ta.startBash("c2", "make install")
	ta.startBash("c3", "make clean")
	ta.request("p1", "c1", "make")
	ta.arm()

	ta.key("a")
	if ta.app.mode != modeNormal {
		t.Fatalf("a on a card block switched mode to %v; want it to go to the card", ta.app.mode)
	}
	ta.event(event.PermissionResolved{Base: rootBase(), RequestID: "p1", Reply: core.PermissionReply{Kind: core.ReplyOnce}})
	if ta.app.w.card.Request() != nil {
		t.Fatalf("card = %+v after the last request resolved, want it cleared", ta.app.w.card.Request())
	}
	if strings.Contains(xansi.Strip(ta.view()), "wants to run") {
		t.Error("the cleared card is still rendered")
	}

	ta.request("p2", "c2", "make install")
	ta.arm()
	ta.key("A")
	ta.event(event.PermissionResolved{Base: rootBase(), RequestID: "p2"})

	ta.request("p3", "c3", "make clean")
	ta.arm()
	ta.key("D")
	if !ta.app.w.card.Typing() {
		t.Fatal("D did not open the deny-message input")
	}
	ta.typeText("not now")
	if ta.app.mode != modeNormal {
		t.Fatalf("typing into the deny input changed mode to %v", ta.app.mode)
	}
	ta.key("enter")

	want := []permReply{
		{ID: "p1", Reply: core.PermissionReply{Kind: core.ReplyOnce}},
		{ID: "p2", Reply: core.PermissionReply{Kind: core.ReplyAlways}},
		{ID: "p3", Reply: core.PermissionReply{Kind: core.ReplyDeny, Message: "not now"}},
	}
	if len(ta.perms.replies) != len(want) {
		t.Fatalf("replies = %+v, want %+v", ta.perms.replies, want)
	}
	for i := range want {
		if ta.perms.replies[i] != want[i] {
			t.Errorf("reply %d = %+v, want %+v", i, ta.perms.replies[i], want[i])
		}
	}

	// A reply to a request that already resolved elsewhere (e.g. the run
	// was cancelled) is harmless: the port's error becomes a hint.
	ta.perms.err = errors.New("unknown permission request")
	ta.key("d")
	if !strings.Contains(ta.app.view.hint, "unknown permission request") {
		t.Errorf("hint = %q, want the reply error", ta.app.view.hint)
	}

	// i still enters INSERT on a card block.
	ta.key("i")
	if ta.app.mode != modeInsert {
		t.Errorf("i on a card block: mode = %v, want INSERT", ta.app.mode)
	}
}

func TestApp_CardDenyEscClosesInput(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "make")
	ta.request("p1", "c1", "make")
	ta.arm()
	ta.key("D")
	ta.typeText("jk")
	ta.key("esc")
	if ta.app.w.card.Typing() {
		t.Fatal("esc did not close the deny input")
	}
	if ta.app.mode != modeNormal || ta.selectedID() != "t/c1" {
		t.Fatalf("esc in the deny input left mode %v / selection %q; want NORMAL on t/c1", ta.app.mode, ta.selectedID())
	}
	if len(ta.perms.replies) != 0 {
		t.Fatalf("replies = %+v, want none", ta.perms.replies)
	}
}

// startSubagent delivers a root task call c9 and its child ses_c's spawn.
func (ta *testApp) startSubagent() {
	ta.t.Helper()
	call := core.ToolCall{ID: "c9", Name: "task", Input: []byte(`{"agent":"explore","description":"find the config"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.event(event.SubagentSpawned{Base: rootBase(), Child: "ses_c", Agent: "explore", Description: "find the config", CallID: "c9"})
}

// childBase is the event Base of the subagent session ses_c.
func childBase() event.Base { return event.Base{SessionID: "ses_c", RootID: "ses_1"} }

func TestApp_SubagentPermissionSelectsOwner(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.startBash("c10", "ls")
	ta.event(event.PermissionRequested{
		Base: childBase(), RequestID: "p1", Tool: "bash", Subject: "cat /etc/hosts",
		Call: core.ToolCall{ID: "k1", Name: "bash"},
	})

	if ta.app.mode != modeNormal {
		t.Fatalf("mode = %v, want NORMAL", ta.app.mode)
	}
	if got := ta.selectedID(); got != "t/c9" {
		t.Fatalf("selected = %q, want the owning subagent block t/c9", got)
	}
	req := ta.app.w.card.Request()
	if req == nil || req.Subagent != "explore" {
		t.Fatalf("card request = %+v, want the explore subagent's", req)
	}
	if !strings.Contains(xansi.Strip(ta.view()), "explore (subagent) wants to run bash: cat /etc/hosts") {
		t.Errorf("view has no subagent card:\n%s", xansi.Strip(ta.view()))
	}
	ta.arm()
	golden.Assert(t, "app_subagent_permission", ta.view())
}

func TestApp_GPCyclesPending(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "one")
	ta.startBash("c2", "two")
	ta.startBash("c3", "three")
	ta.request("p1", "c1", "one")
	ta.request("p3", "c3", "three")
	if got := ta.selectedID(); got != "t/c1" {
		t.Fatalf("selected = %q, want t/c1", got)
	}
	if st := ta.app.statusState(); st.Pending != 2 {
		t.Fatalf("Pending = %d, want 2", st.Pending)
	}

	reqOf := map[string]string{"t/c1": "p1", "t/c3": "p3"}
	for _, want := range []string{"t/c3", "t/c1", "t/c3"} {
		ta.key("g")
		ta.key("p")
		if got := ta.selectedID(); got != want {
			t.Fatalf("gp: selected = %q, want %q", got, want)
		}
		if req := ta.app.w.card.Request(); req == nil || req.ID != reqOf[want] {
			t.Fatalf("card = %+v, want request %s", req, reqOf[want])
		}
	}

	// From a block with no request, gp goes to the first pending one.
	ta.key("k")
	if got := ta.selectedID(); got != "t/c2" {
		t.Fatalf("k: selected = %q, want t/c2", got)
	}
	ta.key("g")
	ta.key("p")
	if got := ta.selectedID(); got != "t/c1" {
		t.Fatalf("gp from a plain block: selected = %q, want the first pending t/c1", got)
	}
}

func TestApp_GoldenPermissionCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("clean up the build")
	ta.startBash("c1", "rm -rf build")
	ta.request("p1", "c1", "rm -rf build")
	ta.arm()
	golden.Assert(t, "app_permission_card", ta.view())
}

func TestApp_SubagentDetailsRefreshPerTick(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSessions(nil, map[core.SessionID][]core.Message{"ses_c": {
		{ID: "k1", SessionID: "ses_c", Role: core.RoleAssistant, Parts: []core.Part{{Kind: core.PartText, Text: "child says hi"}}},
	}}))
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.key("esc")
	if ta.selectedID() != "t/c9" {
		t.Fatalf("test setup: selected %q, want t/c9", ta.selectedID())
	}
	ta.key("enter")
	calls := func() int {
		ta.sessions.mu.Lock()
		defer ta.sessions.mu.Unlock()
		return ta.sessions.msgCalls["ses_c"]
	}
	if n := calls(); n != 1 {
		t.Fatalf("Messages(ses_c) calls after opening = %d, want 1", n)
	}
	for range 3 {
		ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "more "})
	}
	if n := calls(); n != 1 {
		t.Fatalf("child events re-read at once (%d calls); want it deferred to the tick", n)
	}
	ta.fire()
	if n := calls(); n != 2 {
		t.Fatalf("Messages(ses_c) calls after one tick = %d, want 2", n)
	}
	ta.fire()
	if n := calls(); n != 2 {
		t.Fatalf("a tick with no child events re-read (%d calls)", n)
	}
	if !strings.Contains(xansi.Strip(ta.view()), "child says hi") {
		t.Error("the details pane does not show the child's messages")
	}
}

func TestApp_PermissionHintStaysWhileTyping(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("clean up")
	ta.startBash("c1", "rm -rf build")
	ta.typeText("half")
	ta.request("p1", "c1", "rm -rf build")
	ta.typeText(" typed more")
	if got := ta.app.statusState().Hint; got != pendingHint {
		t.Fatalf("hint after more typing = %q, want %q", got, pendingHint)
	}
	// On the card itself the hint is not needed; once resolved it is gone.
	ta.key("esc")
	ta.key("g")
	ta.key("p")
	if got := ta.app.statusState().Hint; got == pendingHint {
		t.Error("the hint is shown while the card's block is selected")
	}
	ta.event(event.PermissionResolved{Base: rootBase(), RequestID: "p1"})
	ta.key("k")
	if got := ta.app.statusState().Hint; got == pendingHint {
		t.Error("the hint outlived the last pending request")
	}
}

func TestApp_SubagentDetailsRefreshKeepsScroll(t *testing.T) {
	t.Parallel()
	var long []string
	for i := range 60 {
		long = append(long, fmt.Sprintf("row%02d", i))
	}
	child := []core.Message{{ID: "k1", SessionID: "ses_c", Role: core.RoleAssistant, Parts: []core.Part{{Kind: core.PartText, Text: strings.Join(long, "\n")}}}}
	ta := newTestApp(t, withSessions(nil, map[core.SessionID][]core.Message{"ses_c": child}))
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.key("esc")
	ta.key("enter")
	for range 5 {
		ta.key("ctrl+e")
	}
	firstRow := func() string {
		return strings.TrimSpace(xansi.Strip(strings.Split(ta.app.w.details.View(), "\n")[2]))
	}
	if got := firstRow(); got != "row04" {
		t.Fatalf("after 5× ctrl+e the first body row = %q, want row04 (a blank line precedes the text)", got)
	}
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "more"})
	ta.fire()
	if got := firstRow(); got != "row04" {
		t.Fatalf("after a refresh the first body row = %q, want the scroll kept (row04)", got)
	}
}
