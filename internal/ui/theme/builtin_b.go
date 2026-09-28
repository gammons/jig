// Ported from slk (internal/ui/styles/themes.go, MIT, same author).
package theme

func builtinB() []Palette {
	return []Palette{
		{
			Name: "GitHub Dark",
			BaseColors: BaseColors{
				Primary: "#58A6FF", Accent: "#3FB950", Warning: "#D29922", Error: "#F85149",
				Background: "#0D1117", Surface: "#161B22", SurfaceDark: "#010409",
				Text: "#C9D1D9", TextMuted: "#8B949E", Border: "#30363D",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1C2128", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "GitHub Light",
			BaseColors: BaseColors{
				Primary: "#0969DA", Accent: "#1A7F37", Warning: "#9A6700", Error: "#CF222E",
				Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
				Text: "#1F2328", TextMuted: "#656D76", Border: "#D0D7DE",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#24292F", SidebarText: "#F6F8FA", SidebarTextMuted: "#8C959F",
			},
		},
		{
			Name: "Gruvbox Dark",
			BaseColors: BaseColors{
				Primary: "#83A598", Accent: "#B8BB26", Warning: "#FABD2F", Error: "#FB4934",
				Background: "#282828", Surface: "#3C3836", SurfaceDark: "#1D2021",
				Text: "#EBDBB2", TextMuted: "#928374", Border: "#504945",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#181818", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Gruvbox Light",
			BaseColors: BaseColors{
				Primary: "#076678", Accent: "#79740E", Warning: "#B57614", Error: "#9D0006",
				Background: "#FBF1C7", Surface: "#EBDBB2", SurfaceDark: "#D5C4A1",
				Text: "#3C3836", TextMuted: "#928374", Border: "#BDAE93",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#282828", SidebarText: "#EBDBB2", SidebarTextMuted: "#A89984",
			},
		},
		{
			Name: "Gruvbox Material Dark",
			BaseColors: BaseColors{
				Primary: "#7DAEA3", Accent: "#A9B665", Warning: "#D8A657", Error: "#EA6962",
				Background: "#282828", Surface: "#32302F", SurfaceDark: "#1D2021",
				Text: "#D4BE98", TextMuted: "#928374", Border: "#45403D",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1A1A1A", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Hot Dog Stand",
			BaseColors: BaseColors{
				Primary: "#FF0000", Accent: "#000000", Warning: "#FF8000", Error: "#800000",
				Background: "#FFFF00", Surface: "#FFFF80", SurfaceDark: "#FFCC00",
				Text: "#000000", TextMuted: "#7F6F00", Border: "#FF0000",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#FF0000", SidebarText: "#FFFF00", SidebarTextMuted: "#FFCC00",
			},
		},
		{
			Name: "Iceberg",
			BaseColors: BaseColors{
				Primary: "#84A0C6", Accent: "#B4BE82", Warning: "#E2A478", Error: "#E27878",
				Background: "#161821", Surface: "#1E2132", SurfaceDark: "#0F1117",
				Text: "#C6C8D1", TextMuted: "#6B7089", Border: "#2E313F",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#242736", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Kanagawa",
			BaseColors: BaseColors{
				Primary: "#7FB4CA", Accent: "#98BB6C", Warning: "#E6C384", Error: "#E46876",
				Background: "#1F1F28", Surface: "#2A2A37", SurfaceDark: "#16161D",
				Text: "#DCD7BA", TextMuted: "#727169", Border: "#363646",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#0C0C10", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Kanagawa Dragon",
			BaseColors: BaseColors{
				Primary: "#8BA4B0", Accent: "#8A9A7B", Warning: "#C4B28A", Error: "#C4746E",
				Background: "#181616", Surface: "#282423", SurfaceDark: "#0D0C0C",
				Text: "#C5C9C5", TextMuted: "#737C73", Border: "#2D2C29",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#282423", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Kanagawa Lotus",
			BaseColors: BaseColors{
				Primary: "#4D699B", Accent: "#6F894E", Warning: "#C4781E", Error: "#C84053",
				Background: "#F2ECBC", Surface: "#E7DBA0", SurfaceDark: "#DCD5AC",
				Text: "#545464", TextMuted: "#8A8980", Border: "#DCD5AC",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1F1F28", SidebarText: "#DCD7BA", SidebarTextMuted: "#8A8980",
			},
		},
		{
			Name: "Light",
			BaseColors: BaseColors{
				Primary: "#0366D6", Accent: "#28A745", Warning: "#D9840D", Error: "#CB2431",
				Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
				Text: "#24292E", TextMuted: "#6A737D", Border: "#D1D5DA",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#24292E", SidebarText: "#F6F8FA", SidebarTextMuted: "#8C959F",
			},
		},
		{
			Name: "Material Ocean",
			BaseColors: BaseColors{
				Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
				Background: "#0F111A", Surface: "#1A1C25", SurfaceDark: "#090B10",
				Text: "#A6ACCD", TextMuted: "#4B526D", Border: "#1F2233",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#2A2D38", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Material Palenight",
			BaseColors: BaseColors{
				Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
				Background: "#292D3E", Surface: "#34324A", SurfaceDark: "#1F1F2E",
				Text: "#A6ACCD", TextMuted: "#676E95", Border: "#3A3F58",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1B1B28", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Melange Dark",
			BaseColors: BaseColors{
				Primary: "#A3A9CE", Accent: "#85B695", Warning: "#EBC06D", Error: "#D47766",
				Background: "#292522", Surface: "#34302C", SurfaceDark: "#1F1B18",
				Text: "#ECE1D7", TextMuted: "#867462", Border: "#403A36",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#16130F", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Mocha",
			BaseColors: BaseColors{
				Primary: "#A0522D", Accent: "#C58A5E", Warning: "#D89A4E", Error: "#B5453B",
				Background: "#F7F3F0", Surface: "#EDE7E2", SurfaceDark: "#E2DAD3",
				Text: "#2A2220", TextMuted: "#6B5E58", Border: "#DAD0C8",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#2E2422", SidebarText: "#E6DCD6", SidebarTextMuted: "#A38F86",
			},
		},
		{
			Name: "Modus Operandi",
			BaseColors: BaseColors{
				Primary: "#0031A9", Accent: "#006800", Warning: "#6F5500", Error: "#A60000",
				Background: "#FFFFFF", Surface: "#F2F2F2", SurfaceDark: "#E5E5E5",
				Text: "#000000", TextMuted: "#595959", Border: "#D0D0D0",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1E1E1E", SidebarText: "#FFFFFF", SidebarTextMuted: "#989898",
			},
		},
		{
			Name: "Modus Vivendi",
			BaseColors: BaseColors{
				Primary: "#2FAFFF", Accent: "#44BC44", Warning: "#FEC43F", Error: "#FF5F59",
				Background: "#000000", Surface: "#1E1E1E", SurfaceDark: "#0A0A0A",
				Text: "#FFFFFF", TextMuted: "#989898", Border: "#303030",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#1A1A1A", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Monokai",
			BaseColors: BaseColors{
				Primary: "#66D9EF", Accent: "#A6E22E", Warning: "#E6DB74", Error: "#F92672",
				Background: "#272822", Surface: "#3E3D32", SurfaceDark: "#1E1F1C",
				Text: "#F8F8F2", TextMuted: "#75715E", Border: "#49483E",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#181811", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Night Owl",
			BaseColors: BaseColors{
				Primary: "#82AAFF", Accent: "#ADDB67", Warning: "#ECC48D", Error: "#EF5350",
				Background: "#011627", Surface: "#0E293F", SurfaceDark: "#010E1A",
				Text: "#D6DEEB", TextMuted: "#5F7E97", Border: "#1D3B53",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#0E293F", SidebarText: "", SidebarTextMuted: "",
			},
		},
		{
			Name: "Nightfox",
			BaseColors: BaseColors{
				Primary: "#719CD6", Accent: "#81B29A", Warning: "#DBC074", Error: "#C94F6D",
				Background: "#192330", Surface: "#212E3F", SurfaceDark: "#131A24",
				Text: "#CDCECF", TextMuted: "#738091", Border: "#2B3B51",
			},
			SidebarColors: SidebarColors{
				SidebarBackground: "#0E141C", SidebarText: "", SidebarTextMuted: "",
			},
		},
	}
}
