package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageQueriesFilterInstancesAndAllEqualsSum(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "queries.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() }()
	instanceA := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	instanceB := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb22"
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	for _, id := range []string{instanceA, instanceB} {
		if err := db.Create(&entities.CPAInstance{ID: id, DisplayName: id, Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []entities.UsageEvent{{InstanceID: instanceA, EventKey: "same-request", RequestID: "same-request", Timestamp: now, AuthIndex: "same-auth", APIGroupKey: "shared-fingerprint", TotalTokens: 10}, {InstanceID: instanceB, EventKey: "same-request", RequestID: "same-request", Timestamp: now, AuthIndex: "same-auth", APIGroupKey: "shared-fingerprint", TotalTokens: 20}} {
		if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
			t.Fatal(err)
		}
	}
	provider := NewUsageService(db, pricing.NewCatalog(pricing.EmptySnapshot()))
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	query := func(instance string) *servicedto.UsageEventsPage {
		page, err := provider.ListUsageEvents(context.Background(), servicedto.UsageFilter{InstanceID: instance, StartTime: &start, EndTime: &end, Page: 1, PageSize: 100})
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	a, b, all := query(instanceA), query(instanceB), query("")
	if len(a.Events) != 1 || a.Events[0].InstanceID != instanceA || len(b.Events) != 1 || b.Events[0].InstanceID != instanceB || len(all.Events) != 2 {
		t.Fatalf("A=%+v B=%+v all=%+v", a.Events, b.Events, all.Events)
	}
	var sum int64
	for _, event := range all.Events {
		sum += event.TotalTokens
	}
	if sum != a.Events[0].TotalTokens+b.Events[0].TotalTokens {
		t.Fatalf("all sum=%d A+B=%d", sum, a.Events[0].TotalTokens+b.Events[0].TotalTokens)
	}
}
