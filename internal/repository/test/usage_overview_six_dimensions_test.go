package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageOverviewAggregationGroupsOnlySixDimensions(t *testing.T) {
	db := openTestDatabase(t)
	hour := time.Date(2026, 7, 23, 11, 0, 0, 0, time.Local)
	aliasA, aliasB := "alias-a", "alias-b"
	base := entities.UsageEvent{
		APIGroupKey: "api-a", Model: "gpt-a", AuthIndex: "auth-a", ModelAlias: &aliasA,
		ServiceTier: "default", EffectiveServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh",
		Endpoint: "GET /v1/responses", ExecutorType: "CodexExecutor",
	}
	variations := []func(*entities.UsageEvent){
		func(e *entities.UsageEvent) {},
		func(e *entities.UsageEvent) { e.ModelAlias = &aliasB },
		func(e *entities.UsageEvent) { e.ResponseServiceTier = "priority" },
		func(e *entities.UsageEvent) { e.Endpoint = "POST /v1/responses" },
		func(e *entities.UsageEvent) { e.ExecutorType = "CodexWebsocketsExecutor" },
		func(e *entities.UsageEvent) { e.EffectiveServiceTier = "priority" },
		func(e *entities.UsageEvent) { e.ServiceTier, e.ReasoningEffort = " default ", " xhigh " },
		func(e *entities.UsageEvent) { e.ServiceTier = "priority" },
		func(e *entities.UsageEvent) { e.ReasoningEffort = "max" },
		func(e *entities.UsageEvent) { e.ServiceTier = "" },
		func(e *entities.UsageEvent) { e.ReasoningEffort = "" },
	}
	var events []entities.UsageEvent
	for i, change := range variations {
		event := base
		change(&event)
		event.EventKey = fmt.Sprintf("six-dimensions-%d", i)
		event.Timestamp = hour.Add(time.Duration(i+1) * time.Minute)
		event.InputTokens = int64(i+1) * 10
		event.TotalTokens = event.InputTokens
		events = append(events, event)
	}
	// 先建桶再追赶第二批，分别覆盖六维插入和已有桶累加。
	for _, batch := range [][]entities.UsageEvent{events[:3], events[3:]} {
		if _, _, err := repository.InsertUsageEvents(db, priceOverviewFixtureEvents(t, db, batch)); err != nil {
			t.Fatal(err)
		}
		if err := repository.AggregateUsageOverviewStats(context.Background(), db, hour.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// Fork contract: the overview key keeps every dimension the fork models
	// (model_alias, response_service_tier, endpoint, executor_type), so the
	// variations above stay in separate buckets instead of collapsing to six.
	type key struct{ ModelAlias, ServiceTier, ResponseServiceTier, ReasoningEffort, Endpoint, ExecutorType string }
	type totals struct{ RequestCount, TotalTokens int64 }
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var rows []struct {
			ModelAlias, ServiceTier, ResponseServiceTier, ReasoningEffort, Endpoint, ExecutorType string
			RequestCount, TotalTokens                                                             int64
		}
		if err := db.Table(table).Select("model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, request_count, total_tokens").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		want := map[key]totals{
			{"alias-a", "default", "default", "xhigh", "GET /v1/responses", "CodexExecutor"}:           {3, 140},
			{"alias-b", "default", "default", "xhigh", "GET /v1/responses", "CodexExecutor"}:           {1, 20},
			{"alias-a", "default", "priority", "xhigh", "GET /v1/responses", "CodexExecutor"}:          {1, 30},
			{"alias-a", "default", "default", "xhigh", "POST /v1/responses", "CodexExecutor"}:          {1, 40},
			{"alias-a", "default", "default", "xhigh", "GET /v1/responses", "CodexWebsocketsExecutor"}: {1, 50},
			{"alias-a", "priority", "default", "xhigh", "GET /v1/responses", "CodexExecutor"}:          {1, 80},
			{"alias-a", "default", "default", "max", "GET /v1/responses", "CodexExecutor"}:             {1, 90},
			{"alias-a", "", "default", "xhigh", "GET /v1/responses", "CodexExecutor"}:                  {1, 100},
			{"alias-a", "default", "default", "", "GET /v1/responses", "CodexExecutor"}:                {1, 110},
		}
		if len(rows) != len(want) {
			t.Fatalf("%s: got %d rows, want %d: %+v", table, len(rows), len(want), rows)
		}
		for _, row := range rows {
			k := key{row.ModelAlias, row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType}
			if got := (totals{row.RequestCount, row.TotalTokens}); got != want[k] {
				t.Fatalf("%s key %+v: got %+v, want %+v", table, k, got, want[k])
			}
			delete(want, k)
		}
	}
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil || checkpoint.LastAggregatedUsageEventID != int64(len(events)) {
		t.Fatalf("unexpected Overview checkpoint: %+v, %v", checkpoint, err)
	}
}
