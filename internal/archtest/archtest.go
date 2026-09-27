// Package archtest loads the repository's Go source as parsed files so
// that the tests in this package can enforce the layer-import rules and
// size limits described in AGENTS.md and the project spec.
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// modulePath is this repo's module import path, stripped from import specs
// so that layer checks can match on repo-relative paths like
// "internal/ui/plain" instead of the fully qualified import path.
const modulePath = "github.com/gammons/jig/"

// File is one parsed .go source file from the repo.
type File struct {
	Path      string // relative to the module root, forward-slash separated
	Pkg       string // directory containing the file, relative to the module root
	AST       *ast.File
	Fset      *token.FileSet // matches the token.Pos values in AST
	Lines     int
	IsTest    bool
	Generated bool
}

// Import is one import spec from a File, with its path normalized to be
// repo-relative (module prefix stripped) and its source line recorded for
// error reporting.
type Import struct {
	Path string
	Line int
}

// Imports returns f's import specs, normalized for layer-rule matching:
// this repo's own module prefix is stripped, so
// "github.com/gammons/jig/internal/ui" becomes "internal/ui".
func (f File) Imports() []Import {
	imports := make([]Import, 0, len(f.AST.Imports))
	for _, spec := range f.AST.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		imports = append(imports, Import{
			Path: strings.TrimPrefix(path, modulePath),
			Line: f.Fset.Position(spec.Pos()).Line,
		})
	}
	return imports
}

// LoadRepo finds the module root by walking upward from the working
// directory for a go.mod file, then parses every .go file under it except
// those in a "testdata" directory.
func LoadRepo(t testing.TB) []File {
	t.Helper()

	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}

	files, err := loadFiles(root)
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}
	return files
}

func loadFiles(root string) ([]File, error) {
	var files []File

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipDir(path, root, d.Name())
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, err := loadFile(root, path)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

func skipDir(path, root, name string) error {
	if path != root && strings.HasPrefix(name, ".") {
		return filepath.SkipDir
	}
	if name == "testdata" {
		return filepath.SkipDir
	}
	return nil
}

func loadFile(root, path string) (File, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return File{}, err
	}
	rel = filepath.ToSlash(rel)

	tf := fset.File(astFile.Pos())

	return File{
		Path:      rel,
		Pkg:       filepath.ToSlash(filepath.Dir(rel)),
		AST:       astFile,
		Fset:      fset,
		Lines:     tf.LineCount(),
		IsTest:    strings.HasSuffix(rel, "_test.go"),
		Generated: isGenerated(astFile),
	}, nil
}

// isGenerated reports whether f carries the standard "Code generated ...
// DO NOT EDIT." marker comment.
func isGenerated(f *ast.File) bool {
	re := regexp.MustCompile(`^// ?Code generated .* DO NOT EDIT\.$`)
	for _, group := range f.Comments {
		for _, c := range group.List {
			if re.MatchString(c.Text) {
				return true
			}
		}
	}
	return false
}

// findModuleRoot walks upward from the current working directory until it
// finds a go.mod file.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
