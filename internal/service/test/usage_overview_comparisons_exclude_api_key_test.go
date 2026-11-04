package test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServiceComparisonsExcludeSelectedAPIKeys(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	kept := entities.CPAAPIKey{APIKey: "sk-kept-key-000001", KeyAlias: "Kept"}
	excluded := entities.CPAAPIKey{APIKey: "sk-excluded-key-0002", KeyAlias: "Excluded"}
	if err := db.Create(&[]*entities.CPAAPIKey{&kept, &excluded}).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	fee, available := 0.5, int64(0)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: day, APIGroupKey: kept.APIKey, Model: "model-kept", RequestCount: 1, SuccessCount: 1, InputTokens: 100, TotalTokens: 100, CostUSD: &fee, UnavailableCostCount: &available},
		{BucketStart: day, APIGroupKey: excluded.APIKey, Model: "model-excluded", RequestCount: 5, SuccessCount: 5, InputTokens: 900, TotalTokens: 900, CostUSD: &fee, UnavailableCostCount: &available},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := service.NewUsageService(db, pricing.NewCatalog(snapshot)).(service.UsageComparisonProvider)
	if !ok {
		t.Fatal("usage service does not expose comparisons")
	}
	end := day.AddDate(0, 0, 1)
	filter := servicedto.UsageFilter{Range: "custom", CustomUnit: "day", StartTime: &day, EndTime: &end, EndExclusive: true}

	all, err := provider.GetUsageOverviewComparisons(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Comparisons.Models) != 2 || len(all.Comparisons.APIKeys) != 2 {
		t.Fatalf("unfiltered comparison should include both keys: %+v", all.Comparisons)
	}

	filter.ExcludedAPIKeyIDs = []string{strconv.FormatInt(excluded.ID, 10)}
	result, err := provider.GetUsageOverviewComparisons(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	comparisons := result.Comparisons
	if len(comparisons.Models) != 1 || comparisons.Models["model-kept"] == nil || comparisons.Models["model-kept"].TotalTokens != 100 {
		t.Fatalf("model distribution still counts the excluded API key: %+v", comparisons.Models)
	}
	if len(comparisons.APIKeys) != 1 || comparisons.APIKeys[kept.APIKey] == nil || comparisons.APIKeys[excluded.APIKey] != nil {
		t.Fatalf("API key distribution still contains the excluded key: %+v", comparisons.APIKeys)
	}
	for _, item := range comparisons.Models {
		for _, tokens := range item.TokenBuckets {
			if tokens > 100 {
				t.Fatalf("token trend bucket still counts the excluded API key: %+v", item.TokenBuckets)
			}
		}
	}
}
