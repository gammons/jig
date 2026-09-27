package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestHygiene_NoPackageMutableVars(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || f.Generated {
			continue
		}
		for _, decl := range f.AST.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR || hasEmbedDirective(gd) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if isAllowedPackageVar(f, name.Name, vs, i) {
						continue
					}
					line := f.Fset.Position(name.Pos()).Line
					t.Errorf("%s:%d: package-var: top-level var %q is not allowed (see AGENTS.md)", f.Path, line, name.Name)
				}
			}
		}
	}
}

func hasEmbedDirective(gd *ast.GenDecl) bool {
	if gd.Doc == nil {
		return false
	}
	for _, c := range gd.Doc.List {
		if strings.HasPrefix(c.Text, "//go:embed") {
			return true
		}
	}
	return false
}

func isAllowedPackageVar(f File, name string, vs *ast.ValueSpec, idx int) bool {
	if name == "_" {
		return true
	}
	if f.Pkg == "internal/archtest" && name == "allowlist" {
		return true
	}
	if strings.HasPrefix(name, "Err") && idx < len(vs.Values) && isErrorConstructorCall(vs.Values[idx]) {
		return true
	}
	return false
}

func isErrorConstructorCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (pkgIdent.Name == "errors" && sel.Sel.Name == "New") ||
		(pkgIdent.Name == "fmt" && sel.Sel.Name == "Errorf")
}

// TestHygiene_NoSleepOrNowInTests walks the AST of every _test.go file
// looking for calls to time.Sleep or time.Now, where "time" resolves to the
// imported standard library "time" package (respecting import aliases).
// It does not use string search, so this file's own mentions of "Sleep" and
// "Now" as identifiers/strings don't trip it.
func TestHygiene_NoSleepOrNowInTests(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if !f.IsTest {
			continue
		}
		alias, ok := timeImportAlias(f.AST)
		if !ok {
			continue
		}
		ast.Inspect(f.AST, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != alias {
				return true
			}
			if sel.Sel.Name == "Sleep" || sel.Sel.Name == "Now" {
				line := f.Fset.Position(call.Pos()).Line
				t.Errorf("%s:%d: hygiene: test files must not call time.%s; use internal/clock", f.Path, line, sel.Sel.Name)
			}
			return true
		})
	}
}

// timeImportAlias returns the local identifier the file uses for the
// standard library "time" package, and whether it imports it at all
// (dot-imported or blank-imported "time" is reported as not present, since
// neither form allows a "time.Sleep"/"time.Now" selector).
func timeImportAlias(f *ast.File) (string, bool) {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "time" {
			continue
		}
		if imp.Name == nil {
			return "time", true
		}
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return "", false
		}
		return imp.Name.Name, true
	}
	return "", false
}
