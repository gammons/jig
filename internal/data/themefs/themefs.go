// Package themefs reads custom theme files from a directory: one Theme
// per top-level "*.toml" file, ported from slk's
// styles.LoadCustomThemes (internal/ui/styles/themes.go, MIT, same
// author). This package returns plain data (no ui import: data may not
// depend on ui); callers convert a Theme's Colors map into a
// theme.Palette with theme.Custom.
package themefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Theme is one custom theme file's contents.
type Theme struct {
	Name   string
	Colors map[string]string
}

// themeFile is the TOML shape of a custom theme file.
type themeFile struct {
	Name   string            `toml:"name"`
	Colors map[string]string `toml:"colors"`
}

// Load reads every "*.toml" file directly under dir (a subdirectory's
// files are ignored) into a Theme: Name is the file's "name" key, or the
// file's stem (base name without ".toml") when that key is absent; Colors
// is the file's [colors] table.
//
// A missing dir returns (nil, nil, nil). A file that can't be read or
// parsed produces a warning naming the file and is skipped, rather than
// failing the whole load.
func Load(dir string) ([]Theme, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("themefs: reading %s: %w", dir, err)
	}

	var themes []Theme
	var warnings []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		th, warn, ok := loadOne(path)
		if !ok {
			warnings = append(warnings, warn)
			continue
		}
		themes = append(themes, th)
	}
	return themes, warnings, nil
}

// loadOne reads and parses one theme file. ok is false (with warn set) if
// the file could not be read or parsed.
func loadOne(path string) (Theme, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, fmt.Sprintf("themefs: reading %s: %v", path, err), false
	}

	var tf themeFile
	if _, err := toml.Decode(string(data), &tf); err != nil {
		return Theme{}, fmt.Sprintf("themefs: parsing %s: %v", path, err), false
	}

	name := tf.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".toml")
	}
	return Theme{Name: name, Colors: tf.Colors}, "", true
}
