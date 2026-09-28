// Ported from slk (internal/ui/styles/themes.go, MIT, same author).
package theme

func builtinC() []Palette {
	return []Palette{
		{
			Name: "Nocturne",
			BaseColors: BaseColors{
				Primary: "#4F9CD9", Accent: "#4FB477", Warning: "#E0B14F", Error: "#E0556B",
				Background: "#0F1620", Surface: "#18222F", SurfaceDark: "#0A0F16",
				Text: "#C3CCD9", TextMuted: "#66717F", Border: "#1F2C3A",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1A2736", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Nord",
			BaseColors: BaseColors{
				Primary: "#88C0D0", Accent: "#A3BE8C", Warning: "#EBCB8B", Error: "#BF616A",
				Background: "#2E3440", Surface: "#3B4252", SurfaceDark: "#242933",
				Text: "#ECEFF4", TextMuted: "#7B88A1", Border: "#4C566A",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1B2028", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Oceanic Next",
			BaseColors: BaseColors{
				Primary: "#6699CC", Accent: "#99C794", Warning: "#FAC863", Error: "#EC5F67",
				Background: "#1B2B34", Surface: "#343D46", SurfaceDark: "#16232B",
				Text: "#CDD3DE", TextMuted: "#65737E", Border: "#4F5B66",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#0A1620", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Ochin",
			BaseColors: BaseColors{
				Primary: "#4A90D9", Accent: "#4CA64C", Warning: "#ECB22E", Error: "#EB4D5C",
				Background: "#FFFFFF", Surface: "#F6F7F8", SurfaceDark: "#EDEFF1",
				Text: "#1D1C1D", TextMuted: "#616061", Border: "#DCDFE3",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#303E4D", SidebarText: "#DAE3ED", SidebarTextMuted: "#8B97A5",
			},
		},
		{
			Name: "One Dark",
			BaseColors: BaseColors{
				Primary: "#61AFEF", Accent: "#98C379", Warning: "#E5C07B", Error: "#E06C75",
				Background: "#282C34", Surface: "#2C313C", SurfaceDark: "#21252B",
				Text: "#ABB2BF", TextMuted: "#636D83", Border: "#3E4452",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1A1D23", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "PaperColor Light",
			BaseColors: BaseColors{
				Primary: "#0087AF", Accent: "#008700", Warning: "#D75F00", Error: "#AF0000",
				Background: "#EEEEEE", Surface: "#E4E4E4", SurfaceDark: "#D0D0D0",
				Text: "#444444", TextMuted: "#878787", Border: "#D0D0D0",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1C1C1C", SidebarText: "#E4E4E4", SidebarTextMuted: "#878787",
			},
		},
		{
			Name: "Poimandres",
			BaseColors: BaseColors{
				Primary: "#89DDFF", Accent: "#5DE4C7", Warning: "#FFFAC2", Error: "#D0679D",
				Background: "#1B1E28", Surface: "#252B37", SurfaceDark: "#171922",
				Text: "#E4F0FB", TextMuted: "#767C9D", Border: "#303340",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#0F1118", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Rosé Pine",
			BaseColors: BaseColors{
				Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
				Background: "#191724", Surface: "#1F1D2E", SurfaceDark: "#16141F",
				Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#26233A",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#262433", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Rosé Pine Dawn",
			BaseColors: BaseColors{
				Primary: "#56949F", Accent: "#286983", Warning: "#EA9D34", Error: "#B4637A",
				Background: "#FAF4ED", Surface: "#FFFAF3", SurfaceDark: "#F2E9E1",
				Text: "#575279", TextMuted: "#797593", Border: "#DFDAD9",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#575279", SidebarText: "#FAF4ED", SidebarTextMuted: "#9893A5",
			},
		},
		{
			Name: "Rosé Pine Moon",
			BaseColors: BaseColors{
				Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
				Background: "#232136", Surface: "#2A273F", SurfaceDark: "#1A1825",
				Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#393552",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#15131F", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Slack Default",
			BaseColors: BaseColors{
				Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E", Error: "#E01E5A",
				Background: "#FFFFFF", Surface: "#F8F8F8", SurfaceDark: "#F0F0F0",
				Text: "#1D1C1D", TextMuted: "#616061", Border: "#DDDDDD",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#434243", SidebarText: "#D1D2D3", SidebarTextMuted: "#9A9B9E",
			},
		},
		{
			Name: "Solarized Dark",
			BaseColors: BaseColors{
				Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
				Background: "#002B36", Surface: "#073642", SurfaceDark: "#001E26",
				Text: "#839496", TextMuted: "#586E75", Border: "#073642",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#001820", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Solarized Light",
			BaseColors: BaseColors{
				Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
				Background: "#FDF6E3", Surface: "#EEE8D5", SurfaceDark: "#E4DCCA",
				Text: "#657B83", TextMuted: "#93A1A1", Border: "#EEE8D5",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#002B36", SidebarText: "#93A1A1", SidebarTextMuted: "#586E75",
			},
		},
		{
			Name: "Synthwave",
			BaseColors: BaseColors{
				Primary: "#36F9F6", Accent: "#72F1B8", Warning: "#FEDE5D", Error: "#FF6E96",
				Background: "#241B2F", Surface: "#2D2139", SurfaceDark: "#1A1226",
				Text: "#F8F8F2", TextMuted: "#848BBD", Border: "#495495",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#150E20", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Tokyo Night",
			BaseColors: BaseColors{
				Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
				Background: "#1A1B26", Surface: "#24283B", SurfaceDark: "#16161E",
				Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#262838", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Tokyo Night Light",
			BaseColors: BaseColors{
				Primary: "#34548A", Accent: "#485E30", Warning: "#8F5E15", Error: "#8C4351",
				Background: "#D5D6DB", Surface: "#CBCCD1", SurfaceDark: "#C4C8DA",
				Text: "#343B58", TextMuted: "#6172B0", Border: "#9699A8",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1A1B26", SidebarText: "#A9B1D6", SidebarTextMuted: "#565F89",
			},
		},
		{
			Name: "Tokyo Night Storm",
			BaseColors: BaseColors{
				Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
				Background: "#24283B", Surface: "#2F334D", SurfaceDark: "#1F2335",
				Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#181B29", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Vesper",
			BaseColors: BaseColors{
				Primary: "#FFC799", Accent: "#99FFE4", Warning: "#FFC799", Error: "#FF8080",
				Background: "#101010", Surface: "#1C1C1C", SurfaceDark: "#0A0A0A",
				Text: "#FFFFFF", TextMuted: "#8B8B8B", Border: "#2A2A2A",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1F1F1F", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Zenburn",
			BaseColors: BaseColors{
				Primary: "#8CD0D3", Accent: "#7F9F7F", Warning: "#F0DFAF", Error: "#CC9393",
				Background: "#3F3F3F", Surface: "#4F4F4F", SurfaceDark: "#2B2B2B",
				Text: "#DCDCCC", TextMuted: "#989890", Border: "#5F5F5F",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#2B2B2B", SidebarText: "", SidebarTextMuted: "",
			},
		},
	}
}
