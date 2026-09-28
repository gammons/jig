package transcript

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// toolUse is one finished root tool call.
type toolUse struct {
	name, input string
	isErr       bool
}

func use(name, input string) toolUse         { return toolUse{name: name, input: input} }
func failed(name, input string) toolUse      { return toolUse{name: name, input: input, isErr: true} }
func pathInput(path string) string           { return fmt.Sprintf(`{"path":%q,"content":"x"}`, path) }
func bashInput(cmd string) string            { return fmt.Sprintf(`{"command":%q}`, cmd) }
func edited(path string) toolUse             { return use("edit", pathInput(path)) }
func wrote(path string) toolUse              { return use("write", pathInput(path)) }
func readFile(path string) toolUse           { return use("read", pathInput(path)) }
func browsed(cmd string) toolUse             { return use("bash", bashInput(cmd)) }
func added(path string) FileChange           { return FileChange{Path: path, Kind: ChangeAdded} }
func modified(path string) FileChange        { return FileChange{Path: path, Kind: ChangeModified} }
func callOf(i int, u toolUse) *core.ToolCall { return mkCall("c"+strconv.Itoa(i), u.name, u.input) }

// feeds are the two ways a projection learns of tool calls.
func feeds() []struct {
	name string
	feed func(p *Projection, uses []toolUse)
} {
	return []struct {
		name string
		feed func(p *Projection, uses []toolUse)
	}{
		{"Load", func(p *Projection, uses []toolUse) {
			var parts []core.Part
			for i, u := range uses {
				c := callOf(i, u)
				parts = append(parts, callPart(c), resultPart(mkResult(c.ID, u.name, "out", u.isErr)))
			}
			p.Load([]core.Message{asstMsg("m1", core.StatusComplete, parts...)})
		}},
		{"Apply", func(p *Projection, uses []toolUse) {
			for i, u := range uses {
				c := callOf(i, u)
				p.Apply(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: *c})
				p.Apply(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: *mkResult(c.ID, u.name, "out", u.isErr)})
			}
		}},
	}
}

func TestChangedFiles(t *testing.T) {
	tests := []struct {
		name string
		uses []toolUse
		want []FileChange
	}{
		{"write to a new path is added", []toolUse{wrote("a.go")}, []FileChange{added("a.go")}},
		{"read then write is modified", []toolUse{readFile("b.go"), wrote("b.go")}, []FileChange{modified("b.go")}},
		{"edit is modified", []toolUse{edited("c.go")}, []FileChange{modified("c.go")}},
		{"failed write is ignored", []toolUse{failed("write", pathInput("a.go"))}, nil},
		{"failed edit is ignored", []toolUse{failed("edit", pathInput("a.go"))}, nil},
		{"failed read does not count as seen", []toolUse{failed("read", pathInput("a.go")), wrote("a.go")}, []FileChange{added("a.go")}},
		{"read alone changes nothing", []toolUse{readFile("a.go")}, nil},
		{
			"first-seen order, each path once",
			[]toolUse{wrote("a.go"), edited("b.go"), wrote("a.go"), edited("a.go"), wrote("b.go"), wrote("c.go")},
			[]FileChange{added("a.go"), modified("b.go"), added("c.go")},
		},
		{"paths are kept as given", []toolUse{wrote("./x/y.go"), edited("/w/x/y.go")}, []FileChange{added("./x/y.go"), modified("/w/x/y.go")}},
		{"other tools and bad input are ignored", []toolUse{use("bash", bashInput("touch z")), use("write", `"nope"`), use("edit", `{}`)}, nil},
	}
	for _, f := range feeds() {
		for _, tt := range tests {
			t.Run(f.name+"/"+tt.name, func(t *testing.T) {
				p := New(root)
				f.feed(p, tt.uses)
				if got := p.ChangedFiles(); !reflect.DeepEqual(got, tt.want) {
					t.Errorf("ChangedFiles = %+v, want %+v", got, tt.want)
				}
			})
		}
	}
}

func TestChangedFiles_ReturnsCopyAndLoadRecomputes(t *testing.T) {
	p := New(root)
	feeds()[1].feed(p, []toolUse{wrote("a.go")})
	cf := p.ChangedFiles()
	if len(cf) != 1 {
		t.Fatalf("ChangedFiles = %+v, want one entry", cf)
	}
	cf[0].Path = "mutated"
	if got := p.ChangedFiles(); !reflect.DeepEqual(got, []FileChange{added("a.go")}) {
		t.Fatalf("ChangedFiles = %+v, want [a.go added]", got)
	}
	// Load replaces live observations with those of the stored history.
	feeds()[0].feed(p, []toolUse{edited("b.go")})
	if got := p.ChangedFiles(); !reflect.DeepEqual(got, []FileChange{modified("b.go")}) {
		t.Errorf("after Load: ChangedFiles = %+v, want [b.go modified]", got)
	}
}

func TestChangedFiles_IgnoresDescendants(t *testing.T) {
	p := New(root)
	run(t, p, []step{
		{taskStarted(), []BlockID{"t/c1"}},
		{spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}},
		{event.ToolCallStarted{Base: sessBase("k1"), MessageID: "km", Call: *mkCall("w1", "write", pathInput("a.go"))}, []BlockID{"t/c1"}},
		{event.ToolCallFinished{Base: sessBase("k1"), MessageID: "km", Result: *mkResult("w1", "write", "ok", false)}, []BlockID{"t/c1"}},
	})
	if got := p.ChangedFiles(); got != nil {
		t.Errorf("ChangedFiles = %+v, want nil", got)
	}
}

func TestLastBrowserURL(t *testing.T) {
	tests := []struct {
		name string
		uses []toolUse
		want string
	}{
		{
			"open, click, goto",
			[]toolUse{browsed("agent-browser open localhost:3000"), browsed("agent-browser click @e2"), browsed("agent-browser goto https://x.dev")},
			"https://x.dev",
		},
		{"open", []toolUse{browsed("agent-browser open localhost:3000")}, "localhost:3000"},
		{"navigate", []toolUse{browsed("agent-browser navigate http://a.test/p")}, "http://a.test/p"},
		{"non-navigation keeps the last URL", []toolUse{browsed("agent-browser open a.test"), browsed("agent-browser snapshot -i")}, "a.test"},
		{"flags are skipped", []toolUse{browsed("agent-browser --headed open b.test")}, "b.test"},
		{"value flag's argument is not mistaken for the subcommand", []toolUse{browsed("agent-browser --session s1 open x")}, "x"},
		{"quoted URL", []toolUse{browsed(`agent-browser open "https://q.test/?a=1"`)}, "https://q.test/?a=1"},
		{"chained commands, last navigation wins", []toolUse{browsed("cd x && agent-browser open a.test && agent-browser goto b.test; agent-browser snapshot")}, "b.test"},
		{"path to the binary", []toolUse{browsed("npx /usr/bin/agent-browser open c.test")}, "c.test"},
		{"no URL after the subcommand", []toolUse{browsed("agent-browser open && ls")}, ""},
		{"other commands", []toolUse{browsed("echo agent-browser"), browsed("curl localhost:3000")}, ""},
		{"failed navigation", []toolUse{failed("bash", bashInput("agent-browser open a.test"))}, ""},
		{"a non-bash tool", []toolUse{use("read", `{"command":"agent-browser open a.test"}`)}, ""},
	}
	for _, f := range feeds() {
		for _, tt := range tests {
			t.Run(f.name+"/"+tt.name, func(t *testing.T) {
				p := New(root)
				f.feed(p, tt.uses)
				if got := p.LastBrowserURL(); got != tt.want {
					t.Errorf("LastBrowserURL = %q, want %q", got, tt.want)
				}
			})
		}
	}
}

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		wantSub  string
		wantArgs []string
		wantOK   bool
	}{
		{"no flags", "agent-browser open localhost:3000", "open", []string{"localhost:3000"}, true},
		{"boolean flag consumes nothing", "agent-browser --headed open b.test", "open", []string{"b.test"}, true},
		{"value flag consumes its argument", "agent-browser --session s1 open x", "open", []string{"x"}, true},
		{
			"value flag joined with a quoted shell variable, then a boolean flag",
			`agent-browser --session "$S" --restore open twitter.com`,
			"open", []string{"twitter.com"}, true,
		},
		{"unknown flag is treated as boolean", "agent-browser --json snapshot -i", "snapshot", []string{"-i"}, true},
		{"flag=value form consumes no extra token", "agent-browser --session=s1 open x", "open", []string{"x"}, true},
		{"multiple flags before the subcommand", "agent-browser --profile p1 --headed click @e2", "click", []string{"@e2"}, true},
		{"no subcommand after the flags", "agent-browser --headed", "", nil, false},
		{"no subcommand at all", "agent-browser", "", nil, false},
		{"a path to the binary", "/usr/bin/agent-browser open c.test", "open", []string{"c.test"}, true},
		{"quoted argument is unquoted", `agent-browser open "https://q.test/?a=1"`, "open", []string{"https://q.test/?a=1"}, true},
		{"not agent-browser", "curl localhost:3000", "", nil, false},
		{"empty", "", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, args, ok := BrowserCommand(tt.cmd)
			if sub != tt.wantSub || !reflect.DeepEqual(args, tt.wantArgs) || ok != tt.wantOK {
				t.Errorf("BrowserCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.cmd, sub, args, ok, tt.wantSub, tt.wantArgs, tt.wantOK)
			}
		})
	}
}

func TestLastBrowserURL_IgnoresDescendants(t *testing.T) {
	p := New(root)
	feeds()[1].feed(p, []toolUse{browsed("agent-browser open root.test")})
	run(t, p, []step{
		{taskStarted(), []BlockID{"t/c1"}},
		{spawned(root, "k1", "explore", "c1"), []BlockID{"t/c1"}},
		{event.ToolCallStarted{Base: sessBase("k1"), MessageID: "km", Call: *mkCall("b1", "bash", bashInput("agent-browser open kid.test"))}, []BlockID{"t/c1"}},
		{event.ToolCallFinished{Base: sessBase("k1"), MessageID: "km", Result: *mkResult("b1", "bash", "ok", false)}, []BlockID{"t/c1"}},
	})
	if got := p.LastBrowserURL(); got != "root.test" {
		t.Errorf("LastBrowserURL = %q, want root.test", got)
	}
}

func TestBrowserSession(t *testing.T) {
	tests := []struct {
		name, cmd, want string
		wantOK          bool
	}{
		{"no session flag", "agent-browser open a.test", "default", true},
		{"separate value", "agent-browser --session s1 open a.test", "s1", true},
		{"joined value", "agent-browser --headed --session=s2 click @e1", "s2", true},
		{"quoted value", `agent-browser --session "s3" open a.test`, "s3", true},
		{"last invocation wins", "agent-browser --session a open x && agent-browser --session b snapshot", "b", true},
		{"last invocation without a flag is default", "agent-browser --session a open x; agent-browser snapshot", "default", true},
		{"a flag after the subcommand is an argument", "agent-browser open x --session s9", "default", true},
		{"a path to the binary", "cd /tmp && /usr/bin/agent-browser --session p open x", "p", true},
		{"not agent-browser", "echo --session s1", "", false},
		{"no subcommand", "agent-browser --session s1", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BrowserSession(tt.cmd)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("BrowserSession(%q) = (%q, %v), want (%q, %v)", tt.cmd, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
