package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/openrouter"
	"cpa-usage-keeper/internal/pricingmetadata"
)

// SetOpenRouterClient wires the fork-only OpenRouter pricing source (optional OPENROUTER_API_KEY).
// Without it the source still works against the public models endpoint.
func SetOpenRouterClient(provider PricingProvider, client *openrouter.Client) {
	if s, ok := provider.(*pricingService); ok {
		s.openRouter = client
	}
}

// fetchOpenRouterCatalog adapts the OpenRouter model list to the shared sync catalog so the
// "Auto-fill from OpenRouter" flow reuses upstream matching, review and apply.
func (s *pricingService) fetchOpenRouterCatalog(ctx context.Context) (pricingmetadata.Catalog, error) {
	source, err := pricingmetadata.SourceByID(pricingmetadata.OpenRouterSourceID)
	if err != nil {
		return pricingmetadata.Catalog{}, err
	}
	client := s.openRouter
	if client == nil {
		client = openrouter.NewClient("", 12*time.Second)
	}
	models, err := client.FetchModels(ctx)
	if err != nil {
		return pricingmetadata.Catalog{}, fmt.Errorf("fetch %s pricing catalog: %w", source.Name, err)
	}
	return pricingmetadata.Catalog{Source: source, Entries: openRouterCatalogEntries(models)}, nil
}

func openRouterCatalogEntries(models []openrouter.ModelInfo) []pricingmetadata.Entry {
	entries := make([]pricingmetadata.Entry, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		input, inputOK := openRouterPerMillion(model.Pricing.Prompt)
		output, outputOK := openRouterPerMillion(model.Pricing.Completion)
		// Negative prices mark variable-price routers (e.g. openrouter/auto); they have no fixed base price.
		if id == "" || !inputOK || !outputOK {
			continue
		}
		cost := pricingmetadata.Cost{Input: input, Output: output}
		if cacheRead, ok := openRouterPerMillion(model.Pricing.CacheRead); ok {
			cost.CacheRead = cacheRead
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = id
		}
		entries = append(entries, pricingmetadata.Entry{
			ProviderID:   pricingmetadata.OpenRouterSourceID,
			ProviderName: "OpenRouter",
			Model:        pricingmetadata.Model{ID: id, Name: name, Cost: cost},
		})
	}
	return entries
}

// openRouterPerMillion converts OpenRouter per-token decimal strings to per-1M prices.
func openRouterPerMillion(raw string) (*float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, false
	}
	perMillion := value * 1_000_000
	return &perMillion, true
}
