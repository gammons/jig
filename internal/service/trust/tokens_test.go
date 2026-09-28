package trust

import (
	"reflect"
	"strings"
	"testing"
)

func tokenLayers() Layers {
	return Layers{ProjectTokens: []TokenAction{
		{Tool: "bash", Token: "{file:mode}"},
		{Tool: "bash", Pattern: "git *", Token: "{env:X}"},
		{Agent: "build", Tool: "edit", Token: "{file:mode}"},
	}}
}

func TestEffects_TokenActions(t *testing.T) {
	want := []string{
		"agents.build permissions.edit → {file:mode}",
		"permissions.bash → {file:mode}",
		`permissions.bash "git *" → {env:X}`,
	}
	got := effectStrings(Effects(tokenLayers()))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effects =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRestrict_DropsTokenActions(t *testing.T) {
	l := tokenLayers()
	out, dropped := Restrict(l)
	if out.ProjectTokens != nil {
		t.Errorf("kept ProjectTokens = %v, want nil", out.ProjectTokens)
	}
	if got, want := effectStrings(dropped), effectStrings(Effects(l)); !reflect.DeepEqual(got, want) {
		t.Errorf("dropped = %v, want %v", got, want)
	}
	if len(l.ProjectTokens) != 3 {
		t.Error("Restrict mutated l.ProjectTokens")
	}
}
