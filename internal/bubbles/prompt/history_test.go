package prompt

import "testing"

func TestHistory_WalkAndRestoreDraft(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetHistory([]string{"first", "second", "third"}) // oldest first
	m = typeText(m, "draft")

	steps := []struct {
		key  string
		want string
	}{
		{"up", "third"},
		{"up", "second"},
		{"up", "first"},
		{"up", "first"}, // no older entry: unchanged
		{"down", "second"},
		{"down", "third"},
		{"down", "draft"}, // past the newest entry: draft restored
	}
	for _, s := range steps {
		m, _ = m.Update(keyMsg(s.key))
		if got := m.Value(); got != s.want {
			t.Errorf("after %q: Value() = %q, want %q", s.key, got, s.want)
		}
	}
}
