//go:build jigtest

package app

import (
	"github.com/gammons/jig/internal/client/llm/jigtest"
	"github.com/gammons/jig/internal/core/ext"
)

// extraProviders adds the scripted "jigtest" provider used by the e2e
// tests.
func extraProviders() []ext.ProviderFactory {
	return []ext.ProviderFactory{jigtest.Factory()}
}
