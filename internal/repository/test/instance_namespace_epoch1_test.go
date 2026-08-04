package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestLegacyProviderIdentityReplacementDoesNotDeleteFutureInstanceRow(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	if err := db.Create(&entities.UsageIdentity{InstanceID: futureInstanceID, AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "future-openai", Name: "future-openai", Type: "openai", Provider: "OpenAI"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceUsageIdentitiesForProviderTypes(context.Background(), db, nil, []string{"openai"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var future entities.UsageIdentity
	if err := db.Where("instance_id = ?", futureInstanceID).Take(&future).Error; err != nil {
		t.Fatal(err)
	}
	if future.IsDeleted {
		t.Fatalf("future provider identity should remain active: %+v", future)
	}
}

func TestLegacyCleanupRedisUsageInboxRetainsFutureInstanceRow(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Now().UTC()
	rows, err := repository.InsertRedisUsageInboxMessagesForInstance(db, entities.LegacyCPAInstanceID, []dto.RedisInboxInsert{{Source: "redis_pull:usage", RawMessage: `{"request_id":"legacy-processed"}`, PoppedAt: now.AddDate(0, 0, -3)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRedisUsageInboxProcessed(db, rows[0].ID, "legacy-processed", now.AddDate(0, 0, -3)); err != nil {
		t.Fatal(err)
	}
	futureRows, err := repository.InsertRedisUsageInboxMessagesForInstance(db, futureInstanceID, []dto.RedisInboxInsert{{Source: "keeper_push", RawMessage: `{"request_id":"future-processed"}`, PoppedAt: now.AddDate(0, 0, -3)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRedisUsageInboxProcessed(db, futureRows[0].ID, "future-processed", now.AddDate(0, 0, -3)); err != nil {
		t.Fatal(err)
	}
	result, err := repository.CleanupRedisUsageInbox(db, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProcessedDeleted != 1 {
		t.Fatalf("legacy processed deletion expected 1 got %d", result.ProcessedDeleted)
	}
	var futureCount int64
	if err := db.Model(&entities.RedisUsageInbox{}).Where("instance_id = ?", futureInstanceID).Count(&futureCount).Error; err != nil {
		t.Fatal(err)
	}
	if futureCount != 1 {
		t.Fatalf("future processed inbox row should survive legacy cleanup: count=%d", futureCount)
	}
}

func TestLegacyArchiveExpiredUsageEventsRetainsFutureInstanceRow(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{InstanceID: entities.LegacyCPAInstanceID, EventKey: "legacy-old", Timestamp: now.AddDate(0, 0, -100), TotalTokens: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{InstanceID: futureInstanceID, EventKey: "future-old", Timestamp: now.AddDate(0, 0, -100), TotalTokens: 2}}); err != nil {
		t.Fatal(err)
	}
	seedArchiveCaughtUpCheckpoints(t, db, eventsMaxID(t, db), now)
	result, err := repository.ArchiveExpiredUsageEvents(context.Background(), db, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Archived == 0 {
		t.Fatalf("expected legacy event to be archived: %+v", result)
	}
	var futureCount int64
	if err := db.Model(&entities.UsageEvent{}).Where("instance_id = ?", futureInstanceID).Count(&futureCount).Error; err != nil {
		t.Fatal(err)
	}
	if futureCount != 1 {
		t.Fatalf("future event should survive legacy archive: count=%d", futureCount)
	}
	var futureArchiveCount int64
	if err := db.Model(&entities.UsageEventArchive{}).Where("instance_id = ?", futureInstanceID).Count(&futureArchiveCount).Error; err != nil {
		t.Fatal(err)
	}
	if futureArchiveCount != 0 {
		t.Fatalf("future archive should not receive future rows during legacy archive: count=%d", futureArchiveCount)
	}
}

func TestLegacyIdentityAggregationDoesNotProcessFutureInstanceEvents(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageIdentity{InstanceID: entities.LegacyCPAInstanceID, AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "shared-auth", Name: "legacy", Type: "claude"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.UsageIdentity{InstanceID: futureInstanceID, AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "shared-auth", Name: "future", Type: "claude"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{InstanceID: futureInstanceID, EventKey: "future-event", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now, TotalTokens: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageIdentityStats(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	var legacyIdentity, futureIdentity entities.UsageIdentity
	if err := db.Where("instance_id = ?", entities.LegacyCPAInstanceID).Take(&legacyIdentity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("instance_id = ?", futureInstanceID).Take(&futureIdentity).Error; err != nil {
		t.Fatal(err)
	}
	if legacyIdentity.TotalRequests != 0 {
		t.Fatalf("legacy identity should not aggregate future events: %+v", legacyIdentity)
	}
	if futureIdentity.TotalRequests != 0 {
		t.Fatalf("legacy aggregation must not process future identity rows: %+v", futureIdentity)
	}
}

func TestOrphanInstanceInsertsRejectedOnFreshDatabase(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.Exec(`INSERT INTO usage_events (instance_id, event_key) VALUES ('0198aa10-4d88-7a20-8f4e-000000000099', 'orphan')`).Error; err == nil {
		t.Fatal("orphan usage event should be rejected on fresh DB")
	}
	if err := db.Exec(`INSERT INTO cpa_api_keys (instance_id, api_key, display_key) VALUES ('0198aa10-4d88-7a20-8f4e-000000000099', 'orphan-key', 'orphan')`).Error; err == nil {
		t.Fatal("orphan API key should be rejected on fresh DB")
	}
}

func TestReferencedInstanceDeleteRejectedOnFreshDatabase(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.Exec(`INSERT INTO usage_events (instance_id, event_key) VALUES (?, 'legacy-reference')`, entities.LegacyCPAInstanceID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DELETE FROM cpa_instances WHERE id = ?`, entities.LegacyCPAInstanceID).Error; err == nil {
		t.Fatal("referenced legacy instance delete should be rejected")
	}
}

func TestDeliveryLedgerSurvivesInboxLifecycleCleanup(t *testing.T) {
	db := openTestDatabase(t)
	seedFutureInstance(t, db)
	now := time.Now().UTC()
	inboxRows, err := repository.InsertRedisUsageInboxMessagesForInstance(db, futureInstanceID, []dto.RedisInboxInsert{{Source: "keeper_push", RawMessage: `{"request_id":"future-delivery"}`, PoppedAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	delivery := entities.CPAUsageDelivery{InstanceID: futureInstanceID, StreamID: "0198aa11-1055-7f12-8a00-e843d1e17522", Sequence: 1, PayloadDigest: []byte("abc"), InboxID: inboxRows[0].ID, AcceptedAt: now}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Unscoped().Where("id = ?", inboxRows[0].ID).Delete(&entities.RedisUsageInbox{}).Error; err != nil {
		t.Fatal(err)
	}
	var survived entities.CPAUsageDelivery
	if err := db.Where("instance_id = ? AND stream_id = ?", futureInstanceID, delivery.StreamID).Take(&survived).Error; err != nil {
		t.Fatal(err)
	}
	if survived.Sequence != 1 || survived.InboxID != inboxRows[0].ID || string(survived.PayloadDigest) != "abc" {
		t.Fatalf("delivery ledger mutated: %+v", survived)
	}
	duplicate := delivery
	duplicate.PayloadDigest = []byte("xyz")
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("replay uniqueness should still be enforced after inbox cleanup")
	}
}
