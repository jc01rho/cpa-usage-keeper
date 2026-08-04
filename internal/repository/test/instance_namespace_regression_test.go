package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/activitystore"
	"cpa-usage-keeper/internal/repository/latencystore"

	"gorm.io/gorm"
)

const futureInstanceID = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"

func TestActivityStoreBlankInputCreatesLegacyRowWithoutMutatingFutureRow(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	future := entities.UsageActivityStat{InstanceID: futureInstanceID, Grain: entities.UsageActivityGrainShort, BucketStart: now, BucketEnd: now.Add(time.Hour), APIGroupKey: "shared", SuccessCount: 10, TotalTokens: 10, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&future).Error; err != nil {
		t.Fatal(err)
	}
	legacy := future
	legacy.ID = 0
	legacy.InstanceID = ""
	legacy.SuccessCount = 1
	legacy.TotalTokens = 1
	if err := activitystore.ApplyRows(db, []entities.UsageActivityStat{legacy}, now); err != nil {
		t.Fatal(err)
	}
	var rows []entities.UsageActivityStat
	if err := db.Order("instance_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].InstanceID != entities.LegacyCPAInstanceID || rows[0].SuccessCount != 1 || rows[1].InstanceID != futureInstanceID || rows[1].SuccessCount != 10 {
		t.Fatalf("cross-instance activity mutation: %+v", rows)
	}
}

func TestLatencyStoreBlankInputDoesNotInheritFutureRow(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	generate := true
	ttft := int64(10)
	built, err := latency.BuildRows([]entities.UsageEvent{{ID: 1, InstanceID: futureInstanceID, APIGroupKey: "shared", Timestamp: now, Generate: &generate, TTFTMS: &ttft, LatencyMS: 20}}, now)
	if err != nil {
		t.Fatal(err)
	}
	for i := range built {
		built[i].CreatedAt = now
		built[i].UpdatedAt = now
	}
	if err := db.Create(&built).Error; err != nil {
		t.Fatal(err)
	}
	legacy := make([]entities.UsageLatencyStat, len(built))
	copy(legacy, built)
	for i := range legacy {
		legacy[i].ID = 0
		legacy[i].InstanceID = ""
	}
	prepared, err := latencystore.PrepareRows(db, legacy, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range prepared {
		if row.ID != 0 || row.InstanceID != entities.LegacyCPAInstanceID {
			t.Fatalf("prepared inherited future identity: %+v", row)
		}
	}
	if err := latencystore.WritePreparedRows(db, prepared); err != nil {
		t.Fatal(err)
	}
	var rows []entities.UsageLatencyStat
	if err := db.Order("instance_id, bucket_type").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("latency rows=%d want 4: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row.SampleCount != 1 {
			t.Fatalf("latency row mutated: %+v", row)
		}
	}
}

func TestLegacyMetadataSyncDoesNotDeleteFutureInstanceRows(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Now()
	if err := db.Create(&entities.CPAAPIKey{InstanceID: futureInstanceID, APIKey: "future-key", DisplayKey: "future"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.UsageIdentity{InstanceID: futureInstanceID, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "future-auth", Name: "future"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.SyncCPAAPIKeys(db, []string{"legacy-key"}, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(context.Background(), db, nil, entities.UsageIdentityAuthTypeAuthFile, now); err != nil {
		t.Fatal(err)
	}
	var key entities.CPAAPIKey
	var identity entities.UsageIdentity
	if err := db.Where("instance_id = ?", futureInstanceID).Take(&key).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("instance_id = ?", futureInstanceID).Take(&identity).Error; err != nil {
		t.Fatal(err)
	}
	if key.IsDeleted || identity.IsDeleted {
		t.Fatalf("future metadata deleted: key=%+v identity=%+v", key, identity)
	}
}

func TestLegacyCheckpointSnapshotIgnoresFutureInstance(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Now()
	if err := db.Create(&[]entities.UsageAggregationCheckpoint{{InstanceID: entities.LegacyCPAInstanceID, Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 5, CreatedAt: now, UpdatedAt: now}, {InstanceID: futureInstanceID, Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 999, CreatedAt: now, UpdatedAt: now}}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadUsageAggregationCheckpointSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.OverviewCursor != 5 {
		t.Fatalf("legacy cursor contaminated: %+v", snapshot)
	}
}

func TestFutureEventBuildsOnlyFutureInstanceRollups(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	generate := true
	ttft := int64(10)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{InstanceID: futureInstanceID, EventKey: "future", APIGroupKey: "shared", Model: "m", Timestamp: now, Generate: &generate, TTFTMS: &ttft, LatencyMS: 20, TotalTokens: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStatsForInstance(context.Background(), db, futureInstanceID, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageActivityStatsForInstance(context.Background(), db, futureInstanceID, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageLatencyStatsForInstance(context.Background(), db, futureInstanceID, now); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_activity_stats", "usage_latency_stats"} {
		var future, legacy int64
		if err := db.Table(table).Where("instance_id = ?", futureInstanceID).Count(&future).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table(table).Where("instance_id = ?", entities.LegacyCPAInstanceID).Count(&legacy).Error; err != nil {
			t.Fatal(err)
		}
		if future == 0 || legacy != 0 {
			t.Fatalf("%s future=%d legacy=%d", table, future, legacy)
		}
	}
}

func seedFutureInstance(t *testing.T, db interface{ Create(any) *gorm.DB }) {
	t.Helper()
	if err := db.Create(&entities.CPAInstance{ID: futureInstanceID, DisplayName: "Future", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
}
