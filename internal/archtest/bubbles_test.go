package archtest

import (
	"go/ast"
	"strings"
	"testing"
)

// bubblesAllowedInternal is the set of internal/bubbles/... packages that
// other internal/bubbles/... files may import (R1).
var bubblesAllowedInternal = map[string]bool{
	"internal/bubbles/ansi":      true,
	"internal/bubbles/overlay":   true,
	"internal/bubbles/scrollbar": true,
	"internal/bubbles/wintree":   true,
}

// bubblesThirdPartyPrefixes is the third-party import allowlist for
// internal/bubbles/... (R1).
var bubblesThirdPartyPrefixes = []string{
	"charm.land/",
	"github.com/charmbracelet/x/ansi",
	"github.com/alecthomas/chroma/v2",
	"golang.org/x/image/",
	"github.com/sahilm/fuzzy",
	"github.com/aymanbagabas/go-udiff",
}

func TestLayers_BubblesImports(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || !underDir(f.Pkg, "internal/bubbles") {
			continue
		}
		for _, imp := range f.Imports() {
			switch {
			case strings.HasPrefix(imp.Path, "internal/"):
				if !bubblesAllowedInternal[imp.Path] || imp.Path == f.Pkg {
					t.Errorf("%s:%d: bubbles-layer: internal/bubbles may only import internal/bubbles/ansi, internal/bubbles/overlay, internal/bubbles/scrollbar, or internal/bubbles/wintree, got %q", f.Path, imp.Line, imp.Path)
				}
			case isStdlib(imp.Path):
				continue
			default:
				if !hasAnyPrefix(imp.Path, bubblesThirdPartyPrefixes) {
					t.Errorf("%s:%d: bubbles-layer: internal/bubbles must not import third-party package %q", f.Path, imp.Line, imp.Path)
				}
			}
		}
	}
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func numFields(fl *ast.FieldList) int {
	n := 0
	for _, field := range fl.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		n += count
	}
	return n
}

func TestBubbles_NoFuncMsgFields(t *testing.T) {
	var files []File
	for _, f := range LoadRepo(t) {
		if !f.IsTest && underDir(f.Pkg, "internal/bubbles") {
			files = append(files, f)
		}
	}
	for _, v := range msgFuncFieldViolations(files) {
		t.Error(v)
	}
}

func TestBubbles_ViewTakesNoParams(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || !underDir(f.Pkg, "internal/bubbles") {
			continue
		}
		for _, decl := range f.AST.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "View" {
				continue
			}
			if fn.Type.Params != nil && numFields(fn.Type.Params) != 0 {
				line := f.Fset.Position(fn.Pos()).Line
				t.Errorf("%s:%d: bubbles-view-params: method View must take zero parameters", f.Path, line)
			}
		}
	}
}

func TestLayers_TranscriptImportsOnlyCore(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || !underDir(f.Pkg, "internal/ui/transcript") {
			continue
		}
		for _, imp := range f.Imports() {
			if isStdlib(imp.Path) || underDir(imp.Path, "internal/core") {
				continue
			}
			t.Errorf("%s:%d: transcript-layer: internal/ui/transcript may only import stdlib and internal/core, got %q", f.Path, imp.Line, imp.Path)
		}
	}
}
