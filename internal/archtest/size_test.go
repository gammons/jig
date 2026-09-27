package archtest

import (
	"go/ast"
	"go/token"
	"testing"
)

func TestSize_MainGo(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.Path != "cmd/jig/main.go" {
			continue
		}
		if f.Lines > 60 {
			t.Errorf("%s: %d lines, want <= 60", f.Path, f.Lines)
		}
		return
	}
	// cmd/jig/main.go does not exist yet; nothing to check.
}

func TestSize_SourceFiles(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || f.Generated {
			continue
		}
		if f.Lines <= 500 {
			continue
		}
		if _, ok := allowlist["filesize:"+f.Path]; ok {
			continue
		}
		t.Errorf("%s: %d lines, want <= 500 (or add to allowlist.go with a justification)", f.Path, f.Lines)
	}
}

func TestSize_AppFunctions(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || f.Generated || !underDir(f.Pkg, "internal/app") {
			continue
		}
		for _, decl := range f.AST.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			start := f.Fset.Position(fn.Pos()).Line
			end := f.Fset.Position(fn.End()).Line
			lines := end - start + 1
			if lines > 40 {
				t.Errorf("%s:%d: app-func-size: func %s is %d lines, want <= 40", f.Path, start, fn.Name.Name, lines)
			}
		}
	}
}

func TestSize_Structs(t *testing.T) {
	files := LoadRepo(t)

	type structInfo struct {
		pkg, name, path string
		line, fields    int
	}
	methodCounts := map[string]int{} // "pkg.Receiver" -> method count
	var structs []structInfo

	for _, f := range files {
		if f.IsTest || f.Generated {
			continue
		}
		for _, decl := range f.AST.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					fields := 0
					for _, field := range st.Fields.List {
						n := len(field.Names)
						if n == 0 {
							n = 1 // embedded field counts as one
						}
						fields += n
					}
					structs = append(structs, structInfo{
						pkg:    f.Pkg,
						name:   ts.Name.Name,
						path:   f.Path,
						line:   f.Fset.Position(ts.Pos()).Line,
						fields: fields,
					})
				}
			case *ast.FuncDecl:
				recv := receiverTypeName(d)
				if recv != "" {
					methodCounts[f.Pkg+"."+recv]++
				}
			}
		}
	}

	for _, s := range structs {
		if s.fields > 15 {
			t.Errorf("%s:%d: struct-size: %s has %d fields, want <= 15", s.path, s.line, s.name, s.fields)
		}
		if count := methodCounts[s.pkg+"."+s.name]; count > 20 {
			t.Errorf("%s:%d: struct-size: %s has %d methods across the package, want <= 20", s.path, s.line, s.name, count)
		}
	}
}

// receiverTypeName returns the name of fn's receiver type, or "" if fn has
// no receiver.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}
