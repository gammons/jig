package catalog

import (
	"strings"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/gammons/jig/internal/core"
)

// convertProviders converts catwalk's provider list into core.ProviderInfo.
func convertProviders(providers []catwalk.Provider) []core.ProviderInfo {
	infos := make([]core.ProviderInfo, 0, len(providers))
	for _, p := range providers {
		infos = append(infos, convertProvider(p))
	}
	return infos
}

func convertProvider(p catwalk.Provider) core.ProviderInfo {
	models := make([]core.ModelInfo, 0, len(p.Models))
	for _, m := range p.Models {
		models = append(models, convertModel(string(p.ID), m))
	}
	return core.ProviderInfo{
		ID:        string(p.ID),
		Name:      p.Name,
		Type:      string(p.Type),
		APIKeyEnv: apiKeyEnv(p.APIKey),
		Endpoint:  p.APIEndpoint,
		Models:    models,
	}
}

// convertModel maps a catwalk model onto core.ModelInfo. The pricing
// mapping matches crush's usage: CostPer1MIn/Out map straight across, but
// the cached fields cross over — CostPer1MInCached (the cost of writing to
// cache) becomes CostCacheWrite, and CostPer1MOutCached (the cost of a
// cache hit) becomes CostCacheRead.
func convertModel(providerID string, m catwalk.Model) core.ModelInfo {
	return core.ModelInfo{
		Ref:              core.ModelRef{Provider: providerID, Model: m.ID},
		Name:             m.Name,
		ContextWindow:    m.ContextWindow,
		DefaultMaxTokens: m.DefaultMaxTokens,
		CostIn:           m.CostPer1MIn,
		CostOut:          m.CostPer1MOut,
		CostCacheRead:    m.CostPer1MOutCached,
		CostCacheWrite:   m.CostPer1MInCached,
		CanReason:        m.CanReason,
	}
}

// apiKeyEnv strips catwalk's leading "$" env-var marker off an API key
// template, e.g. "$ANTHROPIC_API_KEY" -> "ANTHROPIC_API_KEY". A value with
// no leading "$" isn't an env var reference, so it maps to "".
func apiKeyEnv(key string) string {
	if !strings.HasPrefix(key, "$") {
		return ""
	}
	return strings.TrimPrefix(key, "$")
}

// mergeCustom overlays config-defined custom providers onto infos. A custom
// provider whose ID already exists in infos overrides that provider's
// Endpoint and appends its models; otherwise it is added as a new provider
// named after its ID.
func mergeCustom(infos []core.ProviderInfo, custom map[string]core.ProviderConfig) []core.ProviderInfo {
	if len(custom) == 0 {
		return infos
	}

	index := make(map[string]int, len(infos))
	for i, p := range infos {
		index[p.ID] = i
	}

	for id, cfg := range custom {
		models := customModels(id, cfg.Models)
		if i, ok := index[id]; ok {
			infos[i].Endpoint = cfg.BaseURL
			infos[i].Models = append(infos[i].Models, models...)
			continue
		}
		infos = append(infos, core.ProviderInfo{
			ID:       id,
			Name:     id,
			Type:     cfg.Type,
			Endpoint: cfg.BaseURL,
			Models:   models,
		})
		index[id] = len(infos) - 1
	}
	return infos
}

// customModels builds one zero-cost ModelInfo per model ID, named after the
// model ID itself (custom providers have no pricing data).
func customModels(providerID string, ids []string) []core.ModelInfo {
	models := make([]core.ModelInfo, 0, len(ids))
	for _, id := range ids {
		models = append(models, core.ModelInfo{
			Ref:  core.ModelRef{Provider: providerID, Model: id},
			Name: id,
		})
	}
	return models
}
