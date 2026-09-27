package core

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestRule_UnmarshalTOML(t *testing.T) {
	t.Run("bare action string", func(t *testing.T) {
		var doc struct {
			P PermissionRules `toml:"permissions"`
		}
		src := `
[permissions]
write = "deny"
`
		if _, err := toml.Decode(src, &doc); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		rule, ok := doc.P["write"]
		if !ok {
			t.Fatalf("permissions map missing %q", "write")
		}
		if rule.Default != Deny {
			t.Errorf("Default = %q, want %q", rule.Default, Deny)
		}
		if len(rule.Patterns) != 0 {
			t.Errorf("Patterns = %v, want empty", rule.Patterns)
		}
	})

	t.Run("pattern table", func(t *testing.T) {
		var doc struct {
			P PermissionRules `toml:"permissions"`
		}
		src := `
[permissions.bash]
"git status*" = "allow"
"*" = "ask"
`
		if _, err := toml.Decode(src, &doc); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		rule, ok := doc.P["bash"]
		if !ok {
			t.Fatalf("permissions map missing %q", "bash")
		}
		if rule.Default != "" {
			t.Errorf("Default = %q, want empty", rule.Default)
		}
		if len(rule.Patterns) != 2 {
			t.Fatalf("Patterns = %v, want 2 entries", rule.Patterns)
		}
		if rule.Patterns["git status*"] != Allow {
			t.Errorf("Patterns[git status*] = %q, want %q", rule.Patterns["git status*"], Allow)
		}
		if rule.Patterns["*"] != Ask {
			t.Errorf("Patterns[*] = %q, want %q", rule.Patterns["*"], Ask)
		}
	})

	t.Run("invalid action", func(t *testing.T) {
		var doc struct {
			P PermissionRules `toml:"permissions"`
		}
		src := `
[permissions]
write = "maybe"
`
		_, err := toml.Decode(src, &doc)
		if err == nil {
			t.Fatal("Decode: want error for invalid action, got nil")
		}
		if !strings.Contains(err.Error(), "maybe") {
			t.Errorf("error %q does not mention the invalid value %q", err.Error(), "maybe")
		}
	})
}
