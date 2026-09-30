package mcpauth

import "testing"

func TestOpenCmd(t *testing.T) {
	tests := []struct {
		goos string
		want string
		ok   bool
	}{
		{"linux", "xdg-open", true},
		{"darwin", "open", true},
		{"windows", "", false},
	}
	for _, tt := range tests {
		got, ok := openCmd(tt.goos)
		if got != tt.want || ok != tt.ok {
			t.Errorf("openCmd(%q) = %q, %v, want %q, %v", tt.goos, got, ok, tt.want, tt.ok)
		}
	}
}
