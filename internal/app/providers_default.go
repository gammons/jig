//go:build !jigtest

package app

import "github.com/gammons/jig/internal/core/ext"

// extraProviders returns provider factories beyond llm.Factories. It is
// empty in normal builds; the jigtest build tag adds the scripted provider.
func extraProviders() []ext.ProviderFactory { return nil }
