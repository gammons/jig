//go:build jigtest

package e2e

import (
	"fmt"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// effortConfig is jigtestConfig with effort levels on the jigtest models.
func effortConfig(script string) string {
	return fmt.Sprintf(`default_model = "jigtest/m1"
small_model = "jigtest/title"

[providers.jigtest]
type = "jigtest"
models = ["m1", "title"]
efforts = ["low", "medium", "high"]
options = { script = %q }
`, script)
}

func TestE2E_EffortReachesProvider(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{{"low", "low"}, {"Max", "high"}} {
		t.Run(tc.flag, func(t *testing.T) {
			env := newEnv(t)
			want := tc.want
			script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
				"m1": {{Text: "ok", ExpectEffort: &want}},
			}})
			writeConfig(t, env, effortConfig(script))
			stdout, stderr, code := runPrompt(t, env, "--effort", tc.flag, "hi")
			wantCode(t, code, 0, stdout, stderr)
		})
	}
}

func TestE2E_BadEffortExitsConfigError(t *testing.T) {
	env := newEnv(t)
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{"m1": {{Text: "ok"}}}})
	writeConfig(t, env, effortConfig(script))
	stdout, stderr, code := runPrompt(t, env, "--effort", "turbo", "hi")
	wantCode(t, code, 2, stdout, stderr)
}
