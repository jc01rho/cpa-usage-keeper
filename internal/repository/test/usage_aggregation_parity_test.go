package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageOverviewAggregationPreservesAllExistingHourlyAndDailyFields(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)
	aliasA, aliasB := "alias-a", "alias-b"

	// 不同别名仍落入同一 hourly/daily key，显式覆盖全部旧 Token 字段和成功失败计数。
	events := []entities.UsageEvent{
		{EventKey: "overview-parity-1", APIGroupKey: "provider-a", Model: "model-a", ModelAlias: &aliasA, AuthIndex: "auth-a", Timestamp: now.Add(-time.Hour), Failed: false, InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 90, CacheReadTokens: 10, CacheCreationTokens: 3, TotalTokens: 138},
		{EventKey: "overview-parity-2", APIGroupKey: "provider-a", Model: "model-a", ModelAlias: &aliasB, AuthIndex: "auth-a", Timestamp: now.Add(-30 * time.Minute), Failed: true, InputTokens: 200, OutputTokens: 30, ReasoningTokens: 6, CachedTokens: 80, CacheReadTokens: 20, CacheCreationTokens: 4, TotalTokens: 260},
	}
	if _, _, err := repository.InsertUsageEvents(db, priceOverviewFixtureEvents(t, db, events)); err != nil {
		t.Fatalf("insert overview parity events: %v", err)
	}

	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	// Fork contract: the two events differ by model_alias, which the fork keeps as
	// a dimension, so they land in separate rows. Sum the rows for the key: any
	// loss or duplication from split batches still changes the exact totals.
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var requests, success, failure, input, output, reasoning, cached, cacheRead, cacheCreation, total int64
		if err := db.Table(table).Where("api_group_key = ? AND model = ? AND auth_index = ?", "provider-a", "model-a", "auth-a").
			Select("COALESCE(SUM(request_count),0), COALESCE(SUM(success_count),0), COALESCE(SUM(failure_count),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(reasoning_tokens),0), COALESCE(SUM(cached_tokens),0), COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_creation_tokens),0), COALESCE(SUM(total_tokens),0)").
			Row().Scan(&requests, &success, &failure, &input, &output, &reasoning, &cached, &cacheRead, &cacheCreation, &total); err != nil {
			t.Fatalf("load %s parity totals: %v", table, err)
		}
		assertUsageOverviewParityFields(t, table, requests, success, failure, input, output, reasoning, cached, cacheRead, cacheCreation, total)
	}

	// Overview checkpoint 继续精确推进到最后一条 usage event。
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load overview parity checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 2 {
		t.Fatalf("expected overview checkpoint 2, got %+v", checkpoint)
	}
	if checkpoint.StatsUpdatedAt == nil || !checkpoint.StatsUpdatedAt.Equal(now) {
		t.Fatalf("expected overview stats_updated_at %s, got %+v", now, checkpoint.StatsUpdatedAt)
	}
}

func TestUsageOverviewAggregationRollsBackWhenDailyInsertAndRetryBothMiss(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)
	events := []entities.UsageEvent{{EventKey: "overview-daily-failure", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-time.Minute), InputTokens: 10, CachedTokens: 7, CacheReadTokens: 2, TotalTokens: 12}}
	if _, _, err := repository.InsertUsageEvents(db, priceOverviewFixtureEvents(t, db, events)); err != nil {
		t.Fatalf("insert daily failure event: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_overview_daily_insert
		BEFORE INSERT ON usage_overview_daily_stats
		BEGIN
			SELECT RAISE(ABORT, 'forced overview daily insert failure');
		END`).Error; err != nil {
		t.Fatalf("create daily failure trigger: %v", err)
	}

	err := repository.AggregateUsageOverviewStats(context.Background(), db, now)

	if err == nil {
		t.Fatal("expected overview aggregation error after forced daily insert failure")
	}
	var hourlyCount int64
	if countErr := db.Model(&entities.UsageOverviewHourlyStat{}).Count(&hourlyCount).Error; countErr != nil {
		t.Fatalf("count rolled back hourly rows: %v", countErr)
	}
	var checkpointCount int64
	if countErr := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&checkpointCount).Error; countErr != nil {
		t.Fatalf("count rolled back overview checkpoints: %v", countErr)
	}
	if hourlyCount != 0 || checkpointCount != 0 {
		t.Fatalf("overview failure committed partial state: hourly=%d checkpoints=%d", hourlyCount, checkpointCount)
	}
}

func assertUsageOverviewParityFields(t *testing.T, name string, requests, success, failure, input, output, reasoning, cached, cacheRead, cacheCreation, total int64) {
	// 每个旧字段使用固定期望值，任何异步拆分造成的遗漏或重复都会直接失败。
	t.Helper()
	if requests != 2 || success != 1 || failure != 1 || input != 300 || output != 50 || reasoning != 11 || cached != 170 || cacheRead != 30 || cacheCreation != 7 || total != 398 {
		t.Fatalf("unexpected %s parity fields: requests=%d success=%d failure=%d input=%d output=%d reasoning=%d cached=%d cacheRead=%d cacheCreation=%d total=%d", name, requests, success, failure, input, output, reasoning, cached, cacheRead, cacheCreation, total)
	}
}
