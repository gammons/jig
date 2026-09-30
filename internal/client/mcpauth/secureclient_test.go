package mcpauth

import (
	"net/http/httptest"
	"testing"
)

func TestSecureClient_RejectsHTTP(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	client := SecureClient()

	if _, err := client.Get("http://example.com"); err == nil {
		t.Error("GET http://example.com succeeded, want refused")
	}

	// Loopback plain http is allowed (this is what httptest.NewServer gives
	// tests everywhere in this package).
	if _, err := client.Get(ts.URL); err != nil {
		t.Errorf("GET %s (loopback) failed: %v", ts.URL, err)
	}
}
