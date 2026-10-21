package service

import (
	"testing"

	"cpa-usage-keeper/internal/openrouter"
	"cpa-usage-keeper/internal/pricingmetadata"
)

func TestOpenRouterCatalogEntriesConvertPerTokenPricesAndSkipVariablePricing(t *testing.T) {
	entries := openRouterCatalogEntries([]openrouter.ModelInfo{
		{ID: "openai/gpt-5.6", Name: "GPT-5.6", Pricing: openrouter.ModelPricing{Prompt: "0.0000025", Completion: "0.00001", CacheRead: "0.00000025"}},
		{ID: "openrouter/auto", Pricing: openrouter.ModelPricing{Prompt: "-1", Completion: "-1"}},
		{ID: "vendor/no-cache", Pricing: openrouter.ModelPricing{Prompt: "0", Completion: "0.000002"}},
	})
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	first := entries[0]
	if first.ProviderID != pricingmetadata.OpenRouterSourceID || first.Model.ID != "openai/gpt-5.6" || first.Model.Name != "GPT-5.6" {
		t.Fatalf("first entry = %+v", first)
	}
	assertOpenRouterPrice(t, first.Model.Cost.Input, 2.5)
	assertOpenRouterPrice(t, first.Model.Cost.Output, 10)
	assertOpenRouterPrice(t, first.Model.Cost.CacheRead, 0.25)
	if first.Model.Cost.CacheWrite != nil {
		t.Fatalf("cache write must stay missing: %+v", first.Model.Cost)
	}
	second := entries[1]
	if second.Model.Name != "vendor/no-cache" || second.Model.Cost.CacheRead != nil {
		t.Fatalf("second entry = %+v", second)
	}
	assertOpenRouterPrice(t, second.Model.Cost.Input, 0)
}

func assertOpenRouterPrice(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || *got < want-1e-9 || *got > want+1e-9 {
		t.Fatalf("price = %v, want %v", got, want)
	}
}
