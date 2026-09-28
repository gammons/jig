package mdrender

import (
	"fmt"
	"image/color"

	gansi "charm.land/glamour/v2/ansi"
)

// styleConfig builds a glamour ansi.StyleConfig from st. Every Styles color
// becomes a "#rrggbb" hex string, glamour's *string color field. The block
// structure (heading prefixes, list markers, blockquote indent) follows
// glamour's own "notty" style, which does not depend on a detected
// terminal, so rendering stays deterministic across environments.
func styleConfig(st Styles) gansi.StyleConfig {
	text := hexColor(st.Text)
	muted := hexColor(st.Muted)
	heading := hexColor(st.Heading)
	link := hexColor(st.Link)
	code := hexColor(st.Code)
	codeBg := hexColor(st.CodeBg)
	quote := hexColor(st.Quote)
	rule := hexColor(st.Rule)

	yes := true

	return gansi.StyleConfig{
		Document: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{Color: text},
		},
		BlockQuote: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{Color: quote, Italic: &yes},
			Indent:         uintPtr(1),
			IndentToken:    strPtr("\u2502 "),
		},
		List: gansi.StyleList{
			StyleBlock:  gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: text}},
			LevelIndent: 2,
		},
		Heading: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{Color: heading, Bold: &yes},
		},
		H1: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "# "}},
		H2: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "## "}},
		H3: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "### "}},
		H4: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "#### "}},
		H5: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "##### "}},
		H6: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "###### "}},

		Text:   gansi.StylePrimitive{Color: text},
		Emph:   gansi.StylePrimitive{Italic: &yes},
		Strong: gansi.StylePrimitive{Bold: &yes},
		HorizontalRule: gansi.StylePrimitive{
			Color:  rule,
			Format: "\n\u2500\u2500\u2500\n",
		},

		Item:        gansi.StylePrimitive{BlockPrefix: "\u2022 ", Color: muted},
		Enumeration: gansi.StylePrimitive{BlockPrefix: ". ", Color: muted},

		Link:      gansi.StylePrimitive{Color: link, Underline: &yes},
		LinkText:  gansi.StylePrimitive{Color: link, Bold: &yes},
		ImageText: gansi.StylePrimitive{Color: muted},

		Code: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           code,
				BackgroundColor: codeBg,
			},
		},
		CodeBlock: gansi.StyleCodeBlock{
			StyleBlock: gansi.StyleBlock{
				StylePrimitive: gansi.StylePrimitive{
					Color:           code,
					BackgroundColor: codeBg,
				},
			},
			Chroma: &gansi.Chroma{
				Text: gansi.StylePrimitive{Color: code},
			},
		},
	}
}

// hexColor converts c into a "#rrggbb" hex string pointer, or nil for a nil
// color (leaving the corresponding glamour style field unset).
func hexColor(c color.Color) *string {
	if c == nil {
		return nil
	}
	r, g, b, _ := c.RGBA()
	return strPtr(fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8)))
}

func strPtr(s string) *string { return &s }
func uintPtr(v uint) *uint    { return &v }
