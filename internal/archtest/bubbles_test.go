package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
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

// bubbleteaImportAlias returns the local identifier f uses for
// "charm.land/bubbletea/v2", and whether it imports it at all. An unaliased
// import resolves to "tea", that package's declared name (dot- or
// blank-imported forms are reported as absent, mirroring timeImportAlias).
func bubbleteaImportAlias(f *ast.File) (string, bool) {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "charm.land/bubbletea/v2" {
			continue
		}
		if imp.Name == nil {
			return "tea", true
		}
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return "", false
		}
		return imp.Name.Name, true
	}
	return "", false
}

// isTeaMsgFuncField reports whether ft is exactly func(<alias>.Msg) with no
// return values.
func isTeaMsgFuncField(ft *ast.FuncType, alias string) bool {
	if ft.Params == nil || numFields(ft.Params) != 1 {
		return false
	}
	if ft.Results != nil && numFields(ft.Results) != 0 {
		return false
	}
	sel, ok := ft.Params.List[0].Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Msg" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == alias
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
	for _, f := range LoadRepo(t) {
		if f.IsTest || !underDir(f.Pkg, "internal/bubbles") {
			continue
		}
		alias, ok := bubbleteaImportAlias(f.AST)
		if !ok {
			continue
		}
		for _, decl := range f.AST.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				for _, field := range st.Fields.List {
					ft, ok := field.Type.(*ast.FuncType)
					if !ok || !isTeaMsgFuncField(ft, alias) {
						continue
					}
					line := f.Fset.Position(field.Pos()).Line
					t.Errorf("%s:%d: bubbles-msg-field: struct field must not have type func(%s.Msg)", f.Path, line, alias)
				}
			}
		}
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
