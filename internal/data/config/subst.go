package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gammons/jig/internal/data/paths"
)

// substitute walks v -- as produced by toml.Decode into a map[string]any --
// replacing "{env:VAR}" and "{file:path}" tokens in every string,
// recursively through maps and slices. dir is the directory of the config
// file that declared v, used to resolve "{file:...}" paths; home is used to
// expand a leading "~" in those paths.
func substitute(v any, dir, home string, getenv func(string) string) (any, error) {
	switch val := v.(type) {
	case string:
		return substituteString(val, dir, home, getenv)
	case map[string]any:
		return substituteMap(val, dir, home, getenv)
	case []any:
		return substituteSlice(val, dir, home, getenv)
	default:
		return v, nil
	}
}

func substituteMap(m map[string]any, dir, home string, getenv func(string) string) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, elem := range m {
		sub, err := substitute(elem, dir, home, getenv)
		if err != nil {
			return nil, err
		}
		out[k] = sub
	}
	return out, nil
}

func substituteSlice(s []any, dir, home string, getenv func(string) string) ([]any, error) {
	out := make([]any, len(s))
	for i, elem := range s {
		sub, err := substitute(elem, dir, home, getenv)
		if err != nil {
			return nil, err
		}
		out[i] = sub
	}
	return out, nil
}

// tokenPattern matches "{env:VAR}" and "{file:path}" substitution tokens.
// It is compiled per call, not held in a package var, per the repo's
// no-package-mutable-vars rule.
func tokenPattern() *regexp.Regexp {
	return regexp.MustCompile(`\{(env|file):([^}]*)\}`)
}

// substituteString replaces every "{env:VAR}" and "{file:path}" token in s,
// leaving surrounding text untouched. A string may contain multiple tokens.
func substituteString(s, dir, home string, getenv func(string) string) (string, error) {
	matches := tokenPattern().FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s, nil
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(s[last:m[0]])
		kind, arg := s[m[2]:m[3]], s[m[4]:m[5]]
		replacement, err := resolveToken(kind, arg, dir, home, getenv)
		if err != nil {
			return "", err
		}
		b.WriteString(replacement)
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String(), nil
}

func resolveToken(kind, arg, dir, home string, getenv func(string) string) (string, error) {
	switch kind {
	case "env":
		return getenv(arg), nil
	case "file":
		return readFileToken(arg, dir, home)
	default:
		return "", nil
	}
}

// readFileToken resolves rawPath -- relative to dir, with a leading "~"
// expanded to home -- reads it, and returns its content with one trailing
// newline trimmed. A missing file is an error naming both rawPath and the
// resolved absolute path.
func readFileToken(rawPath, dir, home string) (string, error) {
	resolved := resolveFilePath(rawPath, dir, home)
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("config: {file:%s}: reading %s: %w", rawPath, resolved, err)
	}
	return trimOneTrailingNewline(string(data)), nil
}

// resolveFilePath resolves a "{file:rawPath}" token's path: a leading "~"
// expands to home, and a relative path is joined to dir.
func resolveFilePath(rawPath, dir, home string) string {
	resolved := paths.ExpandHome(rawPath, home)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(dir, resolved)
	}
	return resolved
}

// fileRefs appends to out the resolved path of every "{file:...}" token
// in v (walked like substitute), without reading any of them.
func fileRefs(out []string, v any, dir, home string) []string {
	switch val := v.(type) {
	case string:
		for _, m := range tokenPattern().FindAllStringSubmatch(val, -1) {
			if m[1] == "file" {
				out = append(out, resolveFilePath(m[2], dir, home))
			}
		}
	case map[string]any:
		for _, elem := range val {
			out = fileRefs(out, elem, dir, home)
		}
	case []any:
		for _, elem := range val {
			out = fileRefs(out, elem, dir, home)
		}
	}
	return out
}

// dropTokenActions removes, from a file's generic map, every permission
// action (top-level [permissions] and [agents.<name>.permissions], bare or
// per pattern) that holds a "{env:}"/"{file:}" token. It is used when
// tokens are left literal: a literal token can't be a valid action, so
// the entry is treated as unset. It mutates m.
func dropTokenActions(m map[string]any) {
	dropTokenRules(m["permissions"])
	agents, _ := m["agents"].(map[string]any)
	for _, a := range agents {
		if am, ok := a.(map[string]any); ok {
			dropTokenRules(am["permissions"])
		}
	}
}

func dropTokenRules(v any) {
	rules, _ := v.(map[string]any)
	for tool, r := range rules {
		switch rv := r.(type) {
		case string:
			if tokenPattern().MatchString(rv) {
				delete(rules, tool)
			}
		case map[string]any:
			for pattern, a := range rv {
				if s, ok := a.(string); ok && tokenPattern().MatchString(s) {
					delete(rv, pattern)
				}
			}
		}
	}
}

func trimOneTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	return strings.TrimSuffix(s, "\n")
}
