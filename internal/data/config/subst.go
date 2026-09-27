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
	resolved := paths.ExpandHome(rawPath, home)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(dir, resolved)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("config: {file:%s}: reading %s: %w", rawPath, resolved, err)
	}
	return trimOneTrailingNewline(string(data)), nil
}

func trimOneTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	return strings.TrimSuffix(s, "\n")
}
