package frontmatter

import "testing"

type meta struct {
	Title string   `yaml:"title"`
	Tags  []string `yaml:"tags"`
}

func TestParse_WithFrontmatter(t *testing.T) {
	src := "---\ntitle: Hi\ntags: [a, b]\n---\nBody text\nmore\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "Hi" {
		t.Errorf("Title = %q, want %q", m.Title, "Hi")
	}
	if len(m.Tags) != 2 || m.Tags[0] != "a" || m.Tags[1] != "b" {
		t.Errorf("Tags = %v, want [a b]", m.Tags)
	}
	if body != "Body text\nmore\n" {
		t.Errorf("body = %q, want %q", body, "Body text\nmore\n")
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	src := "Just a plain file\nwith no frontmatter\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if body != src {
		t.Errorf("body = %q, want %q", body, src)
	}
	if m.Title != "" || m.Tags != nil {
		t.Errorf("meta = %+v, want untouched zero value", m)
	}
}

func TestParse_CRLF(t *testing.T) {
	src := "---\r\ntitle: Hi\r\n---\r\nBody\r\nmore\r\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "Hi" {
		t.Errorf("Title = %q, want %q", m.Title, "Hi")
	}
	want := "Body\r\nmore\r\n"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestParse_UnterminatedIsError(t *testing.T) {
	src := "---\ntitle: Hi\nno closing delimiter\n"
	var m meta
	if _, err := Parse([]byte(src), &m); err == nil {
		t.Fatal("Parse: want error for unterminated frontmatter, got nil")
	}
}

func TestParse_EmptyFrontmatterBlock(t *testing.T) {
	src := "---\n---\nBody\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "" || m.Tags != nil {
		t.Errorf("meta = %+v, want untouched zero value", m)
	}
	if body != "Body\n" {
		t.Errorf("body = %q, want %q", body, "Body\n")
	}
}

func TestParse_BOMBeforeOpeningDelimiter(t *testing.T) {
	src := "\xef\xbb\xbf---\ntitle: Hi\n---\nBody\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "Hi" {
		t.Errorf("Title = %q, want %q", m.Title, "Hi")
	}
	if body != "Body\n" {
		t.Errorf("body = %q, want %q", body, "Body\n")
	}
}

func TestParse_ClosingAtEOF(t *testing.T) {
	src := "---\ntitle: Hi\n---"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "Hi" {
		t.Errorf("Title = %q, want %q", m.Title, "Hi")
	}
	if body != "" {
		t.Errorf("body = %q, want %q", body, "")
	}
}

func TestParse_DelimiterTrailingWhitespace(t *testing.T) {
	src := "---  \ntitle: Hi\n---\t\nBody\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "Hi" {
		t.Errorf("Title = %q, want %q", m.Title, "Hi")
	}
	if body != "Body\n" {
		t.Errorf("body = %q, want %q", body, "Body\n")
	}
}

func TestParse_NotDelimiterOnFirstLine(t *testing.T) {
	src := "---not-a-delimiter\nrest\n"
	var m meta
	body, err := Parse([]byte(src), &m)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if body != src {
		t.Errorf("body = %q, want %q", body, src)
	}
}
