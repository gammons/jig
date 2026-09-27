package session

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
)

func (f *fixture) withPlaceholder(title string) core.Session {
	f.t.Helper()
	sess := f.create("build")
	sess.Title = title
	if err := f.svc.Update(context.Background(), sess); err != nil {
		f.t.Fatal(err)
	}
	return sess
}

func (f *fixture) title(id core.SessionID) string {
	f.t.Helper()
	got, err := f.svc.Get(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return got.Title
}

func TestTitle_TrimsAndCaps(t *testing.T) {
	long := strings.Repeat("x", 60)
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{"double quotes and blank lines", "\n  \n  \"Fix the parser\"  \nextra line", "Fix the parser"},
		{"single quotes", "'Refactor store'", "Refactor store"},
		{"capped at 50 runes", long, strings.Repeat("x", 49) + "…"},
		{"multibyte cap", strings.Repeat("é", 51), strings.Repeat("é", 49) + "…"},
		{"exactly 50 runes", strings.Repeat("y", 50), strings.Repeat("y", 50)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, defaultCfg())
			small := llmtest.New(llmtest.Text(tc.reply))
			f.llms.clients[smallModel] = small
			sess := f.withPlaceholder("placeholder")

			prompt := strings.Repeat("p", 3000)
			if err := f.svc.GenerateTitle(context.Background(), sess.ID, prompt); err != nil {
				t.Fatal(err)
			}
			got := f.title(sess.ID)
			if got != tc.want {
				t.Errorf("title = %q, want %q", got, tc.want)
			}
			if utf8.RuneCountInString(got) > 50 {
				t.Errorf("title has %d runes, want <= 50", utf8.RuneCountInString(got))
			}

			reqs := small.Requests()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(reqs))
			}
			titleAgent, _ := f.svc.d.Agents.Get("title")
			if reqs[0].System[0] != titleAgent.Prompt {
				t.Errorf("System = %q, want the title prompt", reqs[0].System)
			}
			if n := utf8.RuneCountInString(reqs[0].Messages[0].Parts[0].Text); n != 2000 {
				t.Errorf("user prompt has %d runes, want 2000", n)
			}
		})
	}
}

func TestTitle_ErrorKeepsPlaceholder(t *testing.T) {
	cases := []struct {
		name   string
		client *llmtest.Client
	}{
		{"llm error", llmtest.New()},
		{"empty reply", llmtest.New(llmtest.Text("  \n \"\" \n"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, defaultCfg())
			f.llms.clients[smallModel] = tc.client
			sess := f.withPlaceholder("placeholder")

			if err := f.svc.GenerateTitle(context.Background(), sess.ID, "hello"); err == nil {
				t.Error("GenerateTitle: want error")
			}
			if got := f.title(sess.ID); got != "placeholder" {
				t.Errorf("title = %q, want placeholder", got)
			}
		})
	}
}

func TestTitle_ResolveErrorKeepsPlaceholder(t *testing.T) {
	f := newFixture(t, core.Config{})
	sess := f.withPlaceholder("placeholder")
	if err := f.svc.GenerateTitle(context.Background(), sess.ID, "hello"); err == nil {
		t.Error("GenerateTitle: want error")
	}
	if got := f.title(sess.ID); got != "placeholder" {
		t.Errorf("title = %q, want placeholder", got)
	}
}

func TestPlaceholderTitle(t *testing.T) {
	cases := map[string]string{
		"fix the bug\nplease":         "fix the bug",
		"\n\n  indented first line  ": "indented first line",
		strings.Repeat("z", 80):       strings.Repeat("z", 49) + "…",
		"":                            "",
	}
	for in, want := range cases {
		if got := PlaceholderTitle(in); got != want {
			t.Errorf("PlaceholderTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
