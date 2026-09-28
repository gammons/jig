package archtest

// allowlist maps "rule:path" (e.g. "filesize:internal/foo/bar.go") to a
// justification for why the file is exempt from that rule. This is the one
// sanctioned package-level mutable var in the repo (TestHygiene_NoPackageMutableVars
// allows it explicitly); every other exception needs its own entry here
// with a non-empty justification, checked by TestAllowlist_EntriesHaveJustification.
var allowlist = map[string]string{
	"structsize:internal/ui/theme/palette.go": "theme.Palette is a pure data record (theme color strings), ported verbatim " +
		"from slk's ThemeColors (internal/ui/styles/themes.go); its field set is pinned by the Plan 2b Task 2 brief and " +
		"carries no behavior, unlike the App/widget structs the 15-field cap targets.",
}
