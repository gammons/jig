package app

import (
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestValidateEfforts(t *testing.T) {
	tests := []struct {
		name    string
		cfg     core.Config
		wantErr string
	}{
		{"empty", core.Config{}, ""},
		{"valid", core.Config{DefaultEffort: "High", Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{"low", "max"}}}}, ""},
		{"bad default", core.Config{DefaultEffort: "turbo"}, "config: default_effort"},
		{"bad provider level", core.Config{Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{"low", "turbo"}}}}, `config: providers.p.efforts: unknown effort "turbo"`},
		{"empty provider level", core.Config{Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{""}}}}, "config: providers.p.efforts"},
	}
	for _, tt := range tests {
		err := validateEfforts(tt.cfg)
		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want nil", tt.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want it to contain %q", tt.name, err, tt.wantErr)
		}
	}
}
