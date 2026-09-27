package archtest

// allowlist maps "rule:path" (e.g. "filesize:internal/foo/bar.go") to a
// justification for why the file is exempt from that rule. This is the one
// sanctioned package-level mutable var in the repo (TestHygiene_NoPackageMutableVars
// allows it explicitly); every other exception needs its own entry here
// with a non-empty justification, checked by TestAllowlist_EntriesHaveJustification.
var allowlist = map[string]string{}
