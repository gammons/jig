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
		{"a mid-word dot is fine", "Bump Go to v1.26", "Bump Go to v1.26"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, defaultCfg())
			small := llmtest.New(llmtest.Text(tc.reply))
			f.llms.clients[smallModel] = small
			prompt := strings.Repeat("p", 3000)
			sess := f.withPlaceholder(PlaceholderTitle(prompt))

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
			want := "<message>\n" + strings.Repeat("p", 2000) + "\n</message>"
			if got := reqs[0].Messages[0].Parts[0].Text; got != want {
				t.Errorf("user prompt = %q (%d runes), want the first 2000 runes inside <message> tags", got, utf8.RuneCountInString(got))
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
		// The model answered the prompt instead of titling it.
		{"a reply, not a title", llmtest.New(llmtest.Text("I appreciate you reaching out, but I should clarify that I can't read files."))},
		{"a short sentence", llmtest.New(llmtest.Text("I can't read files."))},
		{"a question", llmtest.New(llmtest.Text("Which file should I read?"))},
		{"too many words", llmtest.New(llmtest.Text("Sure here is a title for this session about reviewing the open PRs"))},
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

func TestTitle_PromptSaysNotToAnswer(t *testing.T) {
	f := newFixture(t, defaultCfg())
	a, _ := f.svc.d.Agents.Get("title")
	for _, want := range []string{"<message>", "Do not answer"} {
		if !strings.Contains(a.Prompt, want) {
			t.Errorf("title prompt lacks %q:\n%s", want, a.Prompt)
		}
	}
}
