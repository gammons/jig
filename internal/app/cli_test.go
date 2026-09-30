package app

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestCLI_ParseRunFlags(t *testing.T) {
	var errw bytes.Buffer
	got, err := parseRun([]string{
		"--agent", "plan", "--model", "anthropic/claude", "--yes",
		"--session", "ses_1", "--cwd", "/tmp/proj", "fix", "the bug",
	}, &errw)
	if err != nil {
		t.Fatalf("parseRun: %v (stderr %q)", err, errw.String())
	}
	want := runOpts{
		agent: "plan", model: "anthropic/claude", yes: true,
		session: "ses_1", cwd: "/tmp/proj", prompt: "fix the bug",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseRun = %+v, want %+v", got, want)
	}
}

func TestCLI_ParseRunDefaults(t *testing.T) {
	got, err := parseRun([]string{"hello"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseRun: %v", err)
	}
	if !reflect.DeepEqual(got, runOpts{prompt: "hello"}) {
		t.Errorf("parseRun = %+v, want only prompt set", got)
	}
}

func TestCLI_ParseRunAttachRepeatable(t *testing.T) {
	got, err := parseRun([]string{"--attach", "a.png", "--attach", "b.txt", "look"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseRun: %v", err)
	}
	want := runOpts{attach: []string{"a.png", "b.txt"}, prompt: "look"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseRun = %+v, want %+v", got, want)
	}
}

func TestCLI_ParseRunInterspersedFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want runOpts
	}{
		{"flags after prompt", []string{"fix tests", "--yes", "--cwd", "/x"},
			runOpts{yes: true, cwd: "/x", prompt: "fix tests"}},
		{"flags between words", []string{"fix", "--model", "a/b", "tests"},
			runOpts{model: "a/b", prompt: "fix tests"}},
		{"double dash ends flags", []string{"--", "--yes"},
			runOpts{prompt: "--yes"}},
		{"double dash mid prompt", []string{"--agent", "plan", "say", "--", "--cwd", "x"},
			runOpts{agent: "plan", prompt: "say --cwd x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRun(tc.args, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("parseRun: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseRun(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestCLI_EmptyPromptUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"--yes"}, {"  ", ""}} {
		if _, err := parseRun(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parseRun(%q) succeeded, want usage error", args)
		}
	}

	env := newTestEnv(t)
	code, _, stderr := env.run(t, "run", "--yes")
	if code != exitConfig {
		t.Errorf("exit = %d, want %d", code, exitConfig)
	}
	if !strings.Contains(stderr, "usage: jig run") {
		t.Errorf("stderr = %q, want usage", stderr)
	}
}

func TestCLI_UnknownFlagExits2(t *testing.T) {
	env := newTestEnv(t)
	code, _, _ := env.run(t, "run", "--bogus", "hi")
	if code != exitConfig {
		t.Errorf("exit = %d, want %d", code, exitConfig)
	}
}

func TestParseRun_Effort(t *testing.T) {
	o, err := parseRun([]string{"--effort", "high", "fix", "it"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.effort != "high" || o.prompt != "fix it" {
		t.Errorf("opts = %+v, want effort high and prompt %q", o, "fix it")
	}
}
