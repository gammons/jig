package archtest

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// parseSources parses each source (name → text) as one file of package
// internal/bubbles/x.
func parseSources(t *testing.T, srcs map[string]string) []File {
	t.Helper()
	var files []File
	for name, src := range srcs {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, File{Path: "internal/bubbles/x/" + name, Pkg: "internal/bubbles/x", AST: f, Fset: fset})
	}
	return files
}

const teaImport = "package x\nimport tea \"charm.land/bubbletea/v2\"\n"

func TestMsgFuncFields_Caught(t *testing.T) {
	cases := map[string]map[string]string{
		"plain": {"a.go": teaImport + "type M struct{ f func(tea.Msg) }\n"},
		"aliased import": {"a.go": "package x\nimport bt \"charm.land/bubbletea/v2\"\n" +
			"type M struct{ f func(bt.Msg) }\n"},
		"dot import": {"a.go": "package x\nimport . \"charm.land/bubbletea/v2\"\n" +
			"type M struct{ f func(Msg) }\n"},
		"pointer to func":   {"a.go": teaImport + "type M struct{ f *func(tea.Msg) }\n"},
		"parenthesized":     {"a.go": teaImport + "type M struct{ f (func(tea.Msg)) }\n"},
		"named type":        {"a.go": teaImport + "type send func(tea.Msg)\ntype M struct{ f send }\n"},
		"alias type":        {"a.go": teaImport + "type send = func(tea.Msg)\ntype M struct{ f send }\n"},
		"pointer to named":  {"a.go": teaImport + "type send func(tea.Msg)\ntype M struct{ f *send }\n"},
		"named chain":       {"a.go": teaImport + "type send func(tea.Msg)\ntype send2 send\ntype M struct{ f send2 }\n"},
		"named, other file": {"a.go": teaImport + "type send func(tea.Msg)\n", "b.go": "package x\ntype M struct{ f send }\n"},
		"named param":       {"a.go": teaImport + "type M struct{ f func(m tea.Msg) }\n"},
	}
	for name, srcs := range cases {
		if got := msgFuncFieldViolations(parseSources(t, srcs)); len(got) != 1 || !strings.Contains(got[0], "bubbles-msg-field") {
			t.Errorf("%s: violations = %v, want exactly one", name, got)
		}
	}
}

func TestMsgFuncFields_Allowed(t *testing.T) {
	cases := map[string]map[string]string{
		"returns a cmd":  {"a.go": teaImport + "type M struct{ f func(tea.Msg) tea.Cmd }\n"},
		"other param":    {"a.go": teaImport + "type M struct{ f func(string) }\n"},
		"no tea import":  {"a.go": "package x\ntype Msg int\ntype M struct{ f func(Msg) }\n"},
		"named, unused":  {"a.go": teaImport + "type send func(tea.Msg)\ntype M struct{ f int }\n"},
		"other package":  {"a.go": "package x\nimport tea \"example.com/tea\"\ntype M struct{ f func(tea.Msg) }\n"},
		"msg, not func":  {"a.go": teaImport + "type M struct{ f tea.Msg }\n"},
		"func of named":  {"a.go": teaImport + "type send func(tea.Msg)\ntype M struct{ f func(send) }\n"},
		"local func var": {"a.go": teaImport + "func g() { var f func(tea.Msg); _ = f }\n"},
	}
	for name, srcs := range cases {
		if got := msgFuncFieldViolations(parseSources(t, srcs)); len(got) != 0 {
			t.Errorf("%s: violations = %v, want none", name, got)
		}
	}
}
