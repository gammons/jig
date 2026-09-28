package actions

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core/ext"
)

// DefaultBindings is jig's default keymap, registered through the same
// ext.Keybind path any extension's keys would use (R21).
func DefaultBindings() []ext.Keybind {
	return []ext.Keybind{
		{Mode: "insert", Key: "ctrl+p", Command: string(PickerOpen)},
		{Mode: "insert", Key: "ctrl+t", Command: string(PickerOpen)},
		{Mode: "normal", Key: "ctrl+p", Command: string(PickerOpen)},
		{Mode: "normal", Key: "ctrl+t", Command: string(PickerOpen)},
		{Mode: "insert", Key: "ctrl+b", Command: string(ViewSidebar)},
		{Mode: "normal", Key: "ctrl+b", Command: string(ViewSidebar)},
		{Mode: "insert", Key: "ctrl+e", Command: string(PromptEditor)},
		{Mode: "normal", Key: "enter", Command: string(TranscriptDetails)},
		{Mode: "normal", Key: "/", Command: string(TranscriptSearch)},
		{Mode: "normal", Key: "y", Command: string(TranscriptYank)},
		{Mode: "normal", Key: "?", Command: string(HelpKeys)},
		{Mode: "normal", Key: "ctrl+c", Command: string(RunCancel)},
	}
}

// isMode reports whether mode is one of the two UI modes a keybind can
// target.
func isMode(mode string) bool {
	return mode == "insert" || mode == "normal"
}

// Keymap resolves a mode and key to the action ID bound there.
type Keymap struct {
	binds map[string]map[string]ID // mode -> key -> ID
}

func newKeymap() Keymap {
	return Keymap{binds: map[string]map[string]ID{}}
}

func (k Keymap) set(mode, key string, id ID) {
	m, ok := k.binds[mode]
	if !ok {
		m = map[string]ID{}
		k.binds[mode] = m
	}
	m[key] = id
}

// Lookup returns the action ID bound to key in mode, if any.
func (k Keymap) Lookup(mode, key string) (ID, bool) {
	id, ok := k.binds[mode][key]
	return id, ok
}

// Keys returns every key bound to id in mode, sorted, for the help list.
func (k Keymap) Keys(mode string, id ID) []string {
	var keys []string
	for key, bound := range k.binds[mode] {
		if bound == id {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// Resolve builds a Keymap from binds (registration order; later wins for
// the same mode+key), then overlays config entries of the form
// "<mode>.<key>" = "<action id>". A config entry naming an unknown mode
// or unknown action is skipped and reported as a warning; config entries
// are applied in sorted key order so warnings are deterministic.
func Resolve(binds []ext.Keybind, config map[string]string, c *Catalogue) (Keymap, []string) {
	km := newKeymap()
	for _, b := range binds {
		km.set(b.Mode, b.Key, ID(b.Command))
	}

	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var warnings []string
	for _, k := range keys {
		v := config[k]
		mode, key, found := strings.Cut(k, ".")
		if !found || !isMode(mode) {
			warnings = append(warnings, fmt.Sprintf("warning: keybinds.%q: unknown mode %q", k, mode))
			continue
		}
		if _, ok := c.Get(ID(v)); !ok {
			warnings = append(warnings, fmt.Sprintf("warning: keybinds.%q: unknown action %q", k, v))
			continue
		}
		km.set(mode, key, ID(v))
	}
	return km, warnings
}
