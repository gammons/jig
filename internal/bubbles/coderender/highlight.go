// Package coderender renders source code as syntax-highlighted terminal
// lines and unified diffs, for widgets that display file contents or
// edits (tool-call details, diff previews). It tokenizes with chroma and
// styles each token with a caller-supplied Styles, so callers control
// every color; coderender does no theme lookups of its own.
package coderender

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Styles holds the colors and attributes coderender renders tokens and
// diff lines with. Plain covers punctuation-less, uncategorized text;
// Name covers plain identifiers; Type and Func cover identifiers chroma
// tags as a builtin/class name or a function name, distinct from Name so
// a theme can make declarations stand out. Added, Removed, and Hunk style
// Diff's "+"/"-" lines and "@@" headers; Gutter is left for a caller that
// renders its own line-number column alongside these lines.
type Styles struct {
	Plain, Keyword, Type, Name, Func, String, Number, Comment, Operator, Added, Removed, Hunk, Gutter lipgloss.Style
}

// DefaultStyles returns coderender's built-in colors: fixed hex values,
// independent of any theme or terminal, for callers that don't supply
// their own Styles.
func DefaultStyles() Styles {
	return Styles{
		Plain:    lipgloss.NewStyle().Foreground(lipgloss.Color("#d0d0d0")),
		Keyword:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff5f87")),
		Type:     lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")),
		Name:     lipgloss.NewStyle().Foreground(lipgloss.Color("#d0d0d0")),
		Func:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd75f")),
		String:   lipgloss.NewStyle().Foreground(lipgloss.Color("#87d787")),
		Number:   lipgloss.NewStyle().Foreground(lipgloss.Color("#d787ff")),
		Comment:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#808080")),
		Operator: lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Added:    lipgloss.NewStyle().Background(lipgloss.Color("#1a3a1a")),
		Removed:  lipgloss.NewStyle().Background(lipgloss.Color("#3a1a1a")),
		Hunk:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fd7ff")),
		Gutter:   lipgloss.NewStyle().Foreground(lipgloss.Color("#585858")),
	}
}

// tabWidth is how many spaces Highlight expands each tab into.
const tabWidth = 4

// Highlight tokenizes code with the lexer chroma matches to path, falling
// back to content analysis (lexers.Analyse) and then a plaintext lexer
// when neither finds one. It expands tabs to tabWidth spaces, styles each
// token from st by its chroma token type, and returns one rendered string
// per line of code, split on "\n" (a trailing newline yields a final
// empty-string line, matching strings.Split's own behavior).
func Highlight(path, code string, st Styles) []string {
	return highlightLines(path, code, st, nil)
}

// highlightLines is Highlight's implementation, plus an optional extra
// transform applied to every token's style before it renders (Diff uses
// this to add a line-wide background without breaking the per-token
// foreground colors, which a naive outer wrap would do: an inner token's
// own SGR reset would also clear an outer background).
func highlightLines(path, code string, st Styles, extra func(lipgloss.Style) lipgloss.Style) []string {
	code = strings.ReplaceAll(code, "\t", strings.Repeat(" ", tabWidth))
	lines := strings.Split(code, "\n")

	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	tokens, err := chroma.Tokenise(lexer, nil, code)
	if err != nil {
		return lines
	}

	out := make([]strings.Builder, len(lines))
	row := 0
	for _, tok := range tokens {
		style := styleFor(tok.Type, st)
		if extra != nil {
			style = extra(style)
		}
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				row++
			}
			if part == "" || row >= len(out) {
				continue
			}
			out[row].WriteString(style.Render(part))
		}
	}

	result := make([]string, len(lines))
	for i := range out {
		result[i] = out[i].String()
	}
	return result
}

// styleFor maps a chroma token type to the Styles field for its category,
// per types.go's category/sub-category grouping (Category groups in
// ranges of 1000, SubCategory in ranges of 100).
func styleFor(tt chroma.TokenType, st Styles) lipgloss.Style {
	switch tt {
	case chroma.KeywordType, chroma.NameClass, chroma.NameBuiltin, chroma.NameBuiltinPseudo, chroma.NameVariableClass:
		return st.Type
	case chroma.NameFunction, chroma.NameFunctionMagic, chroma.NameDecorator:
		return st.Func
	}

	switch tt.Category() {
	case chroma.Keyword.Category():
		return st.Keyword
	case chroma.Name.Category():
		return st.Name
	case chroma.Operator.Category(), chroma.Punctuation.Category():
		return st.Operator
	case chroma.Comment.Category():
		return st.Comment
	case chroma.Literal.Category():
		switch {
		case tt.InSubCategory(chroma.LiteralString):
			return st.String
		case tt.InSubCategory(chroma.LiteralNumber):
			return st.Number
		}
	}
	return st.Plain
}
