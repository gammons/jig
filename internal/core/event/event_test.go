package event

import (
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestBase_RootFallsBackToSession(t *testing.T) {
	a := Base{SessionID: "a"}
	if got := a.Root(); got != core.SessionID("a") {
		t.Errorf("Root() = %q, want %q", got, "a")
	}

	ar := Base{SessionID: "a", RootID: "r"}
	if got := ar.Root(); got != core.SessionID("r") {
		t.Errorf("Root() = %q, want %q", got, "r")
	}
}
