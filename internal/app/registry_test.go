package app

import (
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/ui/actions"
)

func TestRegistry_RegistersDefaultKeybinds(t *testing.T) {
	r := ext.NewRegistry()
	if err := addKeybinds(r, registryDeps{}); err != nil {
		t.Fatalf("addKeybinds: %v", err)
	}
	got := r.Freeze().Keybinds()
	want := actions.DefaultBindings()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Keybinds() = %+v, want %+v", got, want)
	}
}

func TestRegistry_AddCommandsStartsEmpty(t *testing.T) {
	r := ext.NewRegistry()
	if err := addCommands(r, registryDeps{}); err != nil {
		t.Fatalf("addCommands: %v", err)
	}
	if got := r.Freeze().Commands(); len(got) != 0 {
		t.Errorf("Commands() = %v, want empty", got)
	}
}
