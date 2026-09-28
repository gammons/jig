package archtest

import (
	"strings"
	"testing"
)

// underDir reports whether pkg is dir itself or a subpackage of dir.
func underDir(pkg, dir string) bool {
	return pkg == dir || strings.HasPrefix(pkg, dir+"/")
}

// isStdlib reports whether an import path looks like a standard library
// package: its first path segment has no dot in it. Third-party import
// paths always contain a dot (a domain), e.g. "charm.land/fantasy".
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func TestLayers_UIImportsOnlyCoreAndUI(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || !underDir(f.Pkg, "internal/ui") {
			continue
		}
		for _, imp := range f.Imports() {
			switch {
			case strings.HasPrefix(imp.Path, "internal/"):
				if !underDir(imp.Path, "internal/core") &&
					!underDir(imp.Path, "internal/ui") &&
					!underDir(imp.Path, "internal/clock") &&
					!underDir(imp.Path, "internal/bubbles") {
					t.Errorf("%s:%d: ui-layer: internal/ui may only import internal/core, internal/ui, internal/clock, and internal/bubbles, got %q", f.Path, imp.Line, imp.Path)
				}
			case isStdlib(imp.Path):
				switch imp.Path {
				case "os/exec", "net/http", "database/sql":
					t.Errorf("%s:%d: ui-layer: internal/ui must not import %q", f.Path, imp.Line, imp.Path)
				}
			default:
				if !strings.HasPrefix(imp.Path, "charm.land/") {
					t.Errorf("%s:%d: ui-layer: internal/ui must not import third-party package %q", f.Path, imp.Line, imp.Path)
				}
			}
		}
	}
}

func TestLayers_ServiceDoesNotImportConcreteClientOrData(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest || !underDir(f.Pkg, "internal/service") {
			continue
		}
		for _, imp := range f.Imports() {
			switch {
			case underDir(imp.Path, "internal/client"),
				underDir(imp.Path, "internal/data"),
				underDir(imp.Path, "internal/ui"),
				imp.Path == "charm.land/fantasy",
				imp.Path == "database/sql",
				imp.Path == "modernc.org/sqlite":
				t.Errorf("%s:%d: service-layer: internal/service must not import %q", f.Path, imp.Line, imp.Path)
			}
		}
	}
}

func TestLayers_ClientAndDataDoNotImportUpward(t *testing.T) {
	for _, f := range LoadRepo(t) {
		inClientOrData := underDir(f.Pkg, "internal/client") || underDir(f.Pkg, "internal/data")
		if f.IsTest || !inClientOrData {
			continue
		}
		for _, imp := range f.Imports() {
			switch {
			case underDir(imp.Path, "internal/service"),
				underDir(imp.Path, "internal/ui"),
				underDir(imp.Path, "internal/app"):
				t.Errorf("%s:%d: client-data-layer: %s must not import %q", f.Path, imp.Line, f.Pkg, imp.Path)
			}
		}
	}
}

func TestLayers_OnlyAppImportsEverything(t *testing.T) {
	for _, f := range LoadRepo(t) {
		if f.IsTest {
			continue
		}
		if underDir(f.Pkg, "internal/app") || underDir(f.Pkg, "cmd/jig") || underDir(f.Pkg, "e2e") {
			continue
		}

		var importsClient, importsService bool
		for _, imp := range f.Imports() {
			if underDir(imp.Path, "internal/client") {
				importsClient = true
			}
			if underDir(imp.Path, "internal/service") {
				importsService = true
			}
		}
		if importsClient && importsService {
			t.Errorf("%s: app-layer: only internal/app, cmd/jig, and e2e may import both client and service packages, but %s does", f.Path, f.Pkg)
		}
	}
}
