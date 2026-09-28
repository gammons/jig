package agents

import (
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestBuiltin(t *testing.T) {
	plan, ok := Builtin("plan")
	if !ok || plan.Permissions["write"].Default != core.Ask {
		t.Fatalf(`Builtin("plan") = %v, %v`, plan.Permissions, ok)
	}
	// Each call returns a fresh copy.
	plan.Permissions["write"] = core.Rule{Default: core.Allow}
	if again, _ := Builtin("plan"); again.Permissions["write"].Default != core.Ask {
		t.Fatal("Builtin returned a shared Permissions map")
	}
	if _, ok := Builtin("nope"); ok {
		t.Error(`Builtin("nope") = true`)
	}
}

func TestBuiltinNames(t *testing.T) {
	want := []string{"build", "compaction", "explore", "general", "plan", "title"}
	if got := BuiltinNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("BuiltinNames = %v, want %v", got, want)
	}
}
