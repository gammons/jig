package app

import (
	"fmt"
	"io"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
)

// modelsCmd implements `jig models [provider]`: every catalog provider
// (or just the named one) with its credential status and models.
func modelsCmd(args []string, std Stdio, getenv func(string) string) int {
	only, err := parseModels(args, std.Err)
	if err != nil {
		return exitConfig
	}
	e, err := loadEnv("", getenv, staticTrust(false))
	if err != nil {
		printLine(std.Err, err.Error())
		return exitConfig
	}
	cat := newCatalog(e, clock.Real())
	providers := cat.Providers()
	if only != "" {
		p, ok := cat.Provider(only)
		if !ok {
			fmt.Fprintf(std.Err, "unknown provider %q\n", only)
			return exitConfig
		}
		providers = []core.ProviderInfo{p}
	}
	for _, p := range providers {
		printProvider(std.Out, p, credentialStatus(p, e))
	}
	return exitOK
}

// credentialStatus is "(configured)" when the provider has an api_key in
// config, its API key env var is set, or it needs no key at all.
func credentialStatus(p core.ProviderInfo, e env) string {
	if e.cfg().Providers[p.ID].APIKey != "" || p.APIKeyEnv == "" || e.getenv(p.APIKeyEnv) != "" {
		return "(configured)"
	}
	return "(no credentials)"
}

// printProvider prints p's catalog entry; catalog text comes from the
// network, so each line is sanitized.
func printProvider(w io.Writer, p core.ProviderInfo, status string) {
	printLine(w, p.ID+" "+status)
	for _, m := range p.Models {
		line := fmt.Sprintf("  %s  ctx %dk  $%.2f/$%.2f per 1M",
			m.Ref.String(), m.ContextWindow/1000, m.CostIn, m.CostOut)
		if len(m.Efforts) > 0 {
			line += "  effort " + effortRange(m)
		}
		printLine(w, line)
	}
}

// effortRange is m's effort levels as "low…max (high)": lowest to
// highest as listed, then the catalog default when it has one.
func effortRange(m core.ModelInfo) string {
	s := string(m.Efforts[0])
	if len(m.Efforts) > 1 {
		s += "…" + string(m.Efforts[len(m.Efforts)-1])
	}
	if m.DefaultEffort != "" {
		s += " (" + string(m.DefaultEffort) + ")"
	}
	return s
}
