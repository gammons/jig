package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
)

// teaRef is how one file refers to bubbletea: through a package alias,
// or (dot import) with bare identifiers.
type teaRef struct {
	alias string // "" when dot-imported
	dot   bool
}

// bubbleteaRef returns how f refers to "charm.land/bubbletea/v2", and
// whether it imports it usably at all (a blank import does not). An
// unaliased import resolves to "tea", that package's declared name.
func bubbleteaRef(f *ast.File) (teaRef, bool) {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "charm.land/bubbletea/v2" {
			continue
		}
		switch {
		case imp.Name == nil:
			return teaRef{alias: "tea"}, true
		case imp.Name.Name == "_":
			return teaRef{}, false
		case imp.Name.Name == ".":
			return teaRef{dot: true}, true
		default:
			return teaRef{alias: imp.Name.Name}, true
		}
	}
	return teaRef{}, false
}

// isTeaMsg reports whether expr names bubbletea's Msg under ref.
func (ref teaRef) isTeaMsg(expr ast.Expr) bool {
	switch e := unparen(expr).(type) {
	case *ast.Ident:
		return ref.dot && e.Name == "Msg"
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		return ok && !ref.dot && x.Name == ref.alias && e.Sel.Name == "Msg"
	}
	return false
}

// isMsgFunc reports whether expr is exactly func(tea.Msg), with no
// results, under ref.
func (ref teaRef) isMsgFunc(expr ast.Expr) bool {
	ft, ok := unparen(expr).(*ast.FuncType)
	if !ok || ft.Params == nil || numFields(ft.Params) != 1 {
		return false
	}
	if ft.Results != nil && numFields(ft.Results) != 0 {
		return false
	}
	return ref.isTeaMsg(ft.Params.List[0].Type)
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = p.X
	}
}

// fieldBase strips pointers and parentheses from a field type.
func fieldBase(expr ast.Expr) ast.Expr {
	for {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		default:
			return expr
		}
	}
}

// typeSpecs returns every type declaration in f.
func typeSpecs(f *ast.File) []*ast.TypeSpec {
	var out []*ast.TypeSpec
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok {
				out = append(out, ts)
			}
		}
	}
	return out
}

// msgFuncNames returns the package-level type names (defined or alias)
// in files whose type is func(tea.Msg), directly or through another such
// name, resolved to a fixpoint.
func msgFuncNames(files []File) map[string]bool {
	names := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, f := range files {
			ref, hasTea := bubbleteaRef(f.AST)
			for _, ts := range typeSpecs(f.AST) {
				if names[ts.Name.Name] {
					continue
				}
				id, isIdent := unparen(ts.Type).(*ast.Ident)
				if (hasTea && ref.isMsgFunc(ts.Type)) || (isIdent && names[id.Name]) {
					names[ts.Name.Name], changed = true, true
				}
			}
		}
	}
	return names
}

// msgFuncFieldViolations reports every struct field in files (one
// package's files, or several packages') whose type is func(tea.Msg): a
// literal func type (through any pointers or parentheses, with bubbletea
// imported under any alias or dot-imported), or a same-package named
// type or alias whose underlying type is one.
func msgFuncFieldViolations(files []File) []string {
	byPkg := map[string][]File{}
	for _, f := range files {
		byPkg[f.Pkg] = append(byPkg[f.Pkg], f)
	}
	var out []string
	for _, pkg := range byPkg {
		names := msgFuncNames(pkg)
		for _, f := range pkg {
			ref, hasTea := bubbleteaRef(f.AST)
			for _, ts := range typeSpecs(f.AST) {
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				for _, field := range st.Fields.List {
					base := fieldBase(field.Type)
					id, isIdent := base.(*ast.Ident)
					isMsgFunc := (hasTea && ref.isMsgFunc(base)) || (isIdent && names[id.Name])
					if !isMsgFunc {
						continue
					}
					line := f.Fset.Position(field.Pos()).Line
					out = append(out, fmt.Sprintf("%s:%d: bubbles-msg-field: struct field must not have type func(tea.Msg)", f.Path, line))
				}
			}
		}
	}
	return out
}
