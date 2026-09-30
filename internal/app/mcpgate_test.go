package app

import (
	"context"
	"testing"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/gammons/jig/internal/client/mcpauth"
	mcpsvc "github.com/gammons/jig/internal/service/mcp"
)

// TestGateFor_ClosedModeNeverSharesOpenGate: while a sign-in holds a
// server's gate open, a dial in AuthClosed mode (a stale startup connect,
// say) must get a closed gate, or it would open a browser with a redirect
// URL that no listener answers (port 0).
func TestGateFor_ClosedModeNeverSharesOpenGate(t *testing.T) {
	st := &mcpServerState{gate: &mcpauth.Gate{}}
	st.gate.Open(func(context.Context, string) (*sdkauth.AuthorizationResult, error) {
		return nil, nil
	})

	if g := gateFor(st, mcpsvc.AuthOpen); g != st.gate {
		t.Errorf("AuthOpen: got a different gate, want the server's shared gate")
	}
	if g := gateFor(st, mcpsvc.AuthClosed); g == st.gate {
		t.Errorf("AuthClosed: got the server's shared (open) gate, want a closed one")
	}
}
