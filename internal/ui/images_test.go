package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/imgrender"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/bubbles/statusbar"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// testPNG is a 4×4 PNG: red, green, blue, and white quadrants.
func testPNG(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.RGBA{R: 255, A: 255}
			switch {
			case x >= 2 && y < 2:
				c = color.RGBA{G: 255, A: 255}
			case x < 2 && y >= 2:
				c = color.RGBA{B: 255, A: 255}
			case x >= 2 && y >= 2:
				c = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imageSession is a stored session whose last block is a read of an image
// (blob "sha-img").
func imageSession() (core.Session, []core.Message) {
	msgs := []core.Message{
		{ID: "u1", SessionID: "ses_1", Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "look at this"}}},
		{ID: "a1", SessionID: "ses_1", Role: core.RoleAssistant, Parts: []core.Part{
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "read", Input: []byte(`{"path":"pic.png"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{
				CallID: "c1", Name: "read", Output: "image 4x4",
				Media: []core.Media{{Ref: "sha-img", MIME: "image/png"}},
			}},
		}},
	}
	return core.Session{ID: "ses_1", Agent: "build"}, msgs
}

// newImageApp resumes imageSession with the image protocol override.
func newImageApp(t *testing.T, override string, opts ...testOpt) *testApp {
	t.Helper()
	info, msgs := imageSession()
	base := []testOpt{withResume(info, msgs, nil), withBlobs(map[string][]byte{"sha-img": testPNG(t)}), withImages(override)}
	return newTestApp(t, append(base, opts...)...)
}

// countRaw counts the raw payloads containing sub.
func (ta *testApp) countRaw(sub string) int {
	n := 0
	for _, r := range ta.raws {
		if strings.Contains(r, sub) {
			n++
		}
	}
	return n
}

func TestImages_KittyUploadSentOnce(t *testing.T) {
	t.Parallel()
	ta := newImageApp(t, "kitty")
	ta.key("esc")
	ta.key("enter") // open the details on the image read
	if n := ta.countRaw("\x1b_G"); n != 1 {
		t.Fatalf("kitty uploads after opening = %d, want 1", n)
	}
	if !strings.ContainsRune(ta.view(), '\U0010EEEE') {
		t.Error("the details pane shows no kitty placeholder cells")
	}
	ta.key("enter") // close
	ta.key("enter") // reopen: cached, already uploaded
	if n := ta.countRaw("\x1b_G"); n != 1 {
		t.Fatalf("kitty uploads after reopening = %d, want still 1", n)
	}
	if ta.blobs.opens != 1 {
		t.Errorf("blob opens = %d, want 1 (the rendered Result is cached)", ta.blobs.opens)
	}
	for _, r := range ta.raws {
		if strings.Contains(ta.view(), r) {
			t.Fatal("a raw payload leaked into View")
		}
	}
}

// isRaw reports whether cmd yields a tea.RawMsg.
func isRaw(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.RawMsg)
	return ok
}

// showKitty shows k the way the App does: from the cache when usable,
// else rendered (as the details Cmd would) and stored. It reports whether
// the show sent an upload.
func showKitty(s *imageState, k imgKey, img image.Image) bool {
	if _, ok := s.cached(k); !ok {
		s.store(k, s.renderFor(k)(img))
	}
	return isRaw(s.show(k))
}

func TestImages_KittyReuploadAfterReplacement(t *testing.T) {
	t.Parallel()
	// A 64×64 px image is 8×4 cells at its natural size; the smaller box
	// shrinks it, so S and T fit at different sizes.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	s := newImageState(imgrender.Kitty, false)
	sKey, tKey := imgKey{ref: "r", cols: 8, rows: 4}, imgKey{ref: "r", cols: 2, rows: 1}

	if !showKitty(s, sKey, img) {
		t.Fatal("S: first show sent no upload")
	}
	if showKitty(s, sKey, img) {
		t.Fatal("S again: re-sent an upload the terminal holds")
	}
	if !showKitty(s, tKey, img) {
		t.Fatal("T: a new size sent no upload")
	}
	if !showKitty(s, sKey, img) {
		t.Fatal("S after T: the cached upload was not re-sent")
	}
	// Evict every entry (S and T included) with other images.
	for i := range maxCachedImages {
		k := imgKey{ref: fmt.Sprintf("other%d", i), cols: 2, rows: 1}
		s.store(k, s.renderFor(k)(img))
	}
	if _, ok := s.cached(tKey); ok {
		t.Fatal("test setup: T still cached")
	}
	if !showKitty(s, tKey, img) {
		t.Fatal("T after eviction: no upload, though the terminal holds S")
	}
}

func TestImages_CacheIsLRU(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	s := newImageState(imgrender.Blocks, false)
	keep := imgKey{ref: "keep", cols: 1, rows: 1}
	s.store(keep, s.renderFor(keep)(img))
	for i := range maxCachedImages {
		if _, ok := s.cached(keep); !ok {
			t.Fatalf("the recently used entry was evicted after %d stores", i)
		}
		k := imgKey{ref: fmt.Sprintf("o%d", i), cols: 1, rows: 1}
		s.store(k, s.renderFor(k)(img))
	}
	if _, ok := s.cached(imgKey{ref: "o0", cols: 1, rows: 1}); ok {
		t.Error("the least recently used entry was not evicted")
	}
}

func TestImages_SixelReplacedAfterResize(t *testing.T) {
	t.Parallel()
	ta := newImageApp(t, "sixel")
	ta.key("esc")
	ta.key("enter")
	side := ta.app.lay.Side
	want := fmt.Sprintf("\x1b7\x1b[%d;%dH", side.Y+2+1, side.X+1)
	if n := ta.countRaw(want); n != 1 {
		t.Fatalf("sixel placements at %q = %d, want 1 (raws %q)", want, n, ta.raws)
	}
	ta.send(tea.WindowSizeMsg{Width: 160, Height: 40})
	side = ta.app.lay.Side
	want2 := fmt.Sprintf("\x1b7\x1b[%d;%dH", side.Y+2+1, side.X+1)
	if want2 == want {
		t.Fatal("test setup: the details origin did not move")
	}
	if n := ta.countRaw(want2); n != 1 {
		t.Fatalf("sixel re-placements at %q = %d, want 1 (raws %q)", want2, n, ta.raws)
	}
	total := len(ta.raws)
	ta.key("j") // no relayout change: no re-placement
	if len(ta.raws) != total {
		t.Errorf("a key with no layout change re-placed the sixel")
	}
	ta.key("enter") // close: nothing is placed any more
	ta.send(tea.WindowSizeMsg{Width: 150, Height: 40})
	if len(ta.raws) != total {
		t.Errorf("a closed pane's sixel was re-placed")
	}
}

func TestImages_SixelNotPlacedWithoutBodyRow(t *testing.T) {
	t.Parallel()
	ta := newImageApp(t, "sixel")
	ta.key("esc")
	ta.key("enter")
	n := len(ta.raws)
	// The side slot runs beside the prompt too, so squeeze the whole
	// frame down to leave the pane no body row.
	ta.send(tea.WindowSizeMsg{Width: 120, Height: 2})
	if _, _, ok := columnTop(ta.app).body.BodyOrigin(); ok {
		t.Fatalf("test setup: the details pane (%+v) still has a body row", ta.app.lay.Side)
	}
	if len(ta.raws) != n {
		t.Fatalf("placed a sixel with no body row: %q", ta.raws[n:])
	}
}

func TestImages_TerminalVersionRedetects(t *testing.T) {
	t.Parallel()
	info, msgs := imageSession()
	ta := newTestApp(t, withResume(info, msgs, nil), withBlobs(map[string][]byte{"sha-img": testPNG(t)}), withImages(""))
	ta.send(tea.TerminalVersionMsg{Name: "kitty(0.36)"})
	if p := ta.app.img.r.Protocol(); p != imgrender.Kitty {
		t.Fatalf("protocol = %v after a kitty XTVERSION, want kitty", p)
	}
}

func TestApp_GoldenImageDetails(t *testing.T) {
	t.Parallel()
	ta := newImageApp(t, "blocks", withSize(150, 40))
	ta.key("esc")
	ta.key("enter")
	ta.fire() // the resize debounce: record the settled list width
	if len(ta.raws) != 0 {
		t.Fatalf("blocks protocol sent raw payloads: %q", ta.raws)
	}
	golden.Assert(t, "app_image_details", ta.view())
}

func TestTheme_PushBumpsAllVersions(t *testing.T) {
	t.Parallel()
	info, msgs, todos := sidebarSession()
	ta := newTestApp(t, withSize(150, 40), withResume(info, msgs, todos))
	ta.typeText("more")
	ta.key("enter")
	ta.event(event.MessageStarted{Base: rootBase(), MessageID: "m1"})
	ta.startBash("c9", "make")
	ta.request("p1", "c9", "make")
	cardVer := ta.app.w.card.Version()
	before := map[string]int{}
	for id, v := range ta.app.sess.main.track.versions {
		before[string(id)] = v
	}
	if len(before) == 0 {
		t.Fatal("test setup: no items")
	}

	if !ta.app.theme.preview("nord") {
		t.Fatal("test setup: nord preview did not apply")
	}
	pushTheme(ta.app)
	ta.send(nil)

	for id, v := range before {
		if got := ta.app.sess.main.track.versions[transcript.BlockID(id)]; got <= v {
			t.Errorf("item %s version = %d, want > %d", id, got, v)
		}
	}
	if ta.app.w.card.Version() <= cardVer {
		t.Error("the card was not restyled")
	}
	set := ta.app.theme.set

	st := statusbar.New(statusbar.WithStyles(set.Status))
	st.SetWidth(ta.app.lay.Status.W)
	st.Set(ta.app.statusState())
	if got := ta.app.w.status.View(); got != st.View() {
		t.Errorf("status bar not restyled:\n got %q\nwant %q", got, st.View())
	}
	sb := sidebar.New(sidebar.WithStyles(set.Sidebar))
	sb.SetSize(ta.app.lay.Side.W, ta.app.lay.Side.H)
	sb.SetSections(sidebarSections(ta.app))
	if got := ta.app.w.side.View(); got != sb.View() {
		t.Errorf("sidebar not restyled")
	}
	ta.key("esc")
	ta.key("enter")
	bw, bh := columnBodySize(ta.app)
	dt := details.New(details.WithoutHeader(), details.WithStyles(set.Details))
	dt.SetSize(bw, bh)
	dt2 := columnTop(ta.app).body
	dt2.SetContent(details.Content{Header: "x"})
	dt.SetContent(details.Content{Header: "x"})
	if dt2.View() != dt.View() {
		t.Errorf("details not restyled")
	}
	if want := theme.Build(ta.app.theme.current, ta.app.theme.version); want.Version != set.Version {
		t.Errorf("set version = %d, want %d", set.Version, want.Version)
	}
}
