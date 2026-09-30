package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestPrintProvider_ShowsEffortLevels(t *testing.T) {
	var buf bytes.Buffer
	printProvider(&buf, core.ProviderInfo{ID: "anthropic", Models: []core.ModelInfo{
		{Ref: core.ModelRef{Provider: "anthropic", Model: "opus"}, ContextWindow: 200000,
			Efforts: []core.Effort{core.EffortLow, core.EffortHigh, core.EffortMax}, DefaultEffort: core.EffortHigh},
		{Ref: core.ModelRef{Provider: "anthropic", Model: "haiku"}, ContextWindow: 200000},
	}}, "(configured)")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[1], "  effort low…max (high)") || strings.Contains(lines[2], "effort") {
		t.Errorf("output =\n%s\nwant opus with 'effort low…max (high)' and haiku without effort", buf.String())
	}
}
