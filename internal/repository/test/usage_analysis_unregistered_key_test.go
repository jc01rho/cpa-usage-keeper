package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

// Analysis must count usage whose group key is not a registered API key (for example replica
// events grouped by provider name), so its totals match Overview.
func TestAnalysisCountsUsageForUnregisteredAndDeletedGroupKeys(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	if err := db.Create(&[]entities.CPAAPIKey{{APIKey: "key-a", DisplayKey: "Key A"}, {APIKey: "key-deleted", IsDeleted: true}}).Error; err != nil {
		t.Fatalf("seed keys: %v", err)
	}
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cost, unavailable := 1.0, int64(0)
	stats := []entities.UsageOverviewHourlyStat{
		{BucketStart: start, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1, TotalTokens: 100, CostUSD: &cost, UnavailableCostCount: &unavailable},
		{BucketStart: start, APIGroupKey: "key-deleted", Model: "model-a", RequestCount: 2, TotalTokens: 200, CostUSD: &cost, UnavailableCostCount: &unavailable},
		{BucketStart: start, APIGroupKey: "openai-compatible-provider", Model: "model-a", RequestCount: 4, TotalTokens: 400, CostUSD: &cost, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&stats).Error; err != nil {
		t.Fatalf("seed hourly stats: %v", err)
	}
	filter := repodto.UsageQueryFilter{Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true}
	analysis, err := repository.BuildAnalysisWithFilter(db, filter)
	if err != nil {
		t.Fatalf("build Analysis: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || analysis.TokenUsage[0].TotalTokens != 700 || analysis.TokenUsage[0].Requests != 7 {
		t.Fatalf("Analysis dropped unregistered or deleted keys: %+v", analysis.TokenUsage)
	}
	if len(analysis.APIKeyComposition) != 3 {
		t.Fatalf("expected 3 API key composition rows, got %+v", analysis.APIKeyComposition)
	}
}

// The daily path (long ranges) uses a separate query and must not filter by registered keys either.
func TestAnalysisDailyCountsUsageForUnregisteredGroupKeys(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "key-a", DisplayKey: "Key A"}).Error; err != nil {
		t.Fatalf("seed key: %v", err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 4)
	bucket := start.AddDate(0, 0, 1)
	cost, unavailable := 1.0, int64(0)
	stats := []entities.UsageOverviewDailyStat{
		{BucketStart: bucket, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1, TotalTokens: 100, CostUSD: &cost, UnavailableCostCount: &unavailable},
		{BucketStart: bucket, APIGroupKey: "claude", Model: "model-a", RequestCount: 3, TotalTokens: 300, CostUSD: &cost, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&stats).Error; err != nil {
		t.Fatalf("seed daily stats: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop raw events: %v", err)
	}
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("build daily Analysis: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || analysis.TokenUsage[0].TotalTokens != 400 || analysis.TokenUsage[0].Requests != 4 {
		t.Fatalf("daily Analysis dropped unregistered keys: %+v", analysis.TokenUsage)
	}
}
