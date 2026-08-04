package repository

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"gorm.io/gorm"
)

const (
	metadataInstanceA   = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	metadataInstanceB   = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb22"
	metadataFingerprint = "akf1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestCommitMetadataSnapshotIsolatesInstancesAndRevisionSemantics(t *testing.T) {
	db := openMetadataDatabase(t)
	now := time.Date(2026, 8, 3, 12, 36, 1, 0, time.UTC)
	bodyA := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"fingerprint":"` + metadataFingerprint + `","displayKey":"sk-...cdef","alias":"A"}]}`)
	bodyB := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"fingerprint":"` + metadataFingerprint + `","displayKey":"sk-...cdef","alias":"B"}]}`)
	applyMetadataBody(t, db, metadataInstanceA, protocol.CategoryAPIKeys, bodyA, now)
	applyMetadataBody(t, db, metadataInstanceB, protocol.CategoryAPIKeys, bodyB, now)

	var rows []entities.CPAAPIKey
	if err := db.Order("instance_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].InstanceID != metadataInstanceA || rows[0].KeyAlias != "A" || rows[1].InstanceID != metadataInstanceB || rows[1].KeyAlias != "B" {
		t.Fatalf("colliding keys merged: %+v", rows)
	}

	replay, err := CommitMetadataSnapshot(context.Background(), db, metadataInstanceA, protocol.CategoryAPIKeys, decodeMetadataBody(t, bodyA, protocol.CategoryAPIKeys), bodyA, now.Add(time.Minute))
	if err != nil || replay.Applied {
		t.Fatalf("exact replay = %+v err=%v", replay, err)
	}
	conflictBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[]}`)
	_, err = CommitMetadataSnapshot(context.Background(), db, metadataInstanceA, protocol.CategoryAPIKeys, decodeMetadataBody(t, conflictBody, protocol.CategoryAPIKeys), conflictBody, now)
	if !errors.Is(err, ErrConflictingMetadataRevision) {
		t.Fatalf("conflict error=%v", err)
	}

	emptyA := []byte(`{"protocolVersion":"keeper-export/v1","revision":2,"complete":true,"generatedAt":"2026-08-03T12:37:00.000Z","items":[]}`)
	applyMetadataBody(t, db, metadataInstanceA, protocol.CategoryAPIKeys, emptyA, now.Add(time.Minute))
	var activeA, activeB int64
	db.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND is_deleted = ?", metadataInstanceA, false).Count(&activeA)
	db.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND is_deleted = ?", metadataInstanceB, false).Count(&activeB)
	if activeA != 0 || activeB != 1 {
		t.Fatalf("empty A deleted wrong rows: A=%d B=%d", activeA, activeB)
	}

	staleA := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:38:00.000Z","items":[]}`)
	_, err = CommitMetadataSnapshot(context.Background(), db, metadataInstanceA, protocol.CategoryAPIKeys, decodeMetadataBody(t, staleA, protocol.CategoryAPIKeys), staleA, now)
	if !errors.Is(err, ErrStaleMetadataRevision) {
		t.Fatalf("stale error=%v", err)
	}
}

func TestCommitMetadataSnapshotCategoryIsolationAndProtocolSafeFields(t *testing.T) {
	db := openMetadataDatabase(t)
	now := time.Date(2026, 8, 3, 12, 36, 1, 0, time.UTC)
	authBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"authIndex":"same-auth","name":"a.json","displayName":"same","type":"codex","provider":"codex","prefix":"team","priority":7,"disabled":false,"note":"safe note","accountId":"acct_1","projectId":null,"xaiUserId":null,"activeStart":null,"activeUntil":null,"planType":"team"}]}`)
	providerBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"authIndex":"same-auth","providerType":"openai-compatibility","displayName":"same","prefix":"provider","baseUrl":"https://example.com/v1","priority":4,"disabled":false,"note":"safe provider","apiKeyFingerprint":"` + metadataFingerprint + `"}]}`)
	apiKeyBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"fingerprint":"` + metadataFingerprint + `","displayKey":"sk-...cdef","alias":"same"}]}`)
	applyMetadataBody(t, db, metadataInstanceA, protocol.CategoryAuthFiles, authBody, now)
	applyMetadataBody(t, db, metadataInstanceA, protocol.CategoryProviderIdentities, providerBody, now)
	applyMetadataBody(t, db, metadataInstanceA, protocol.CategoryAPIKeys, apiKeyBody, now)

	emptyAuth := []byte(`{"protocolVersion":"keeper-export/v1","revision":2,"complete":true,"generatedAt":"2026-08-03T12:37:00.000Z","items":[]}`)
	applyMetadataBody(t, db, metadataInstanceA, protocol.CategoryAuthFiles, emptyAuth, now.Add(time.Minute))
	var identities []entities.UsageIdentity
	if err := db.Order("auth_type").Find(&identities).Error; err != nil {
		t.Fatal(err)
	}
	if len(identities) != 2 || !identities[0].IsDeleted || identities[1].IsDeleted {
		t.Fatalf("category isolation failed: %+v", identities)
	}
	provider := identities[1]
	if provider.Type != "openai-compatibility" || provider.LookupKey != metadataFingerprint || provider.BaseURL != "https://example.com/v1" || provider.FilePath != nil {
		t.Fatalf("unsafe or missing provider projection: %+v", provider)
	}
	var apiKey entities.CPAAPIKey
	if err := db.Where("instance_id = ?", metadataInstanceA).First(&apiKey).Error; err != nil {
		t.Fatal(err)
	}
	if apiKey.APIKey != metadataFingerprint || apiKey.DisplayKey != "sk-...cdef" || apiKey.IsDeleted {
		t.Fatalf("unsafe or missing API key projection: %+v", apiKey)
	}

	before := struct{ identities, keys, ledgers int64 }{}
	db.Model(&entities.UsageIdentity{}).Count(&before.identities)
	db.Model(&entities.CPAAPIKey{}).Count(&before.keys)
	db.Model(&entities.CPAMetadataSnapshot{}).Count(&before.ledgers)
	incomplete := []byte(`{"protocolVersion":"keeper-export/v1","revision":3,"complete":false,"generatedAt":"2026-08-03T12:38:00.000Z","items":[]}`)
	if _, perr := protocol.DecodeMetadataSnapshot(incomplete, protocol.CategoryAuthFiles); perr == nil || perr.Code != "incomplete_snapshot" {
		t.Fatalf("incomplete decode error=%v", perr)
	}
	after := struct{ identities, keys, ledgers int64 }{}
	db.Model(&entities.UsageIdentity{}).Count(&after.identities)
	db.Model(&entities.CPAAPIKey{}).Count(&after.keys)
	db.Model(&entities.CPAMetadataSnapshot{}).Count(&after.ledgers)
	if before != after {
		t.Fatalf("incomplete snapshot mutated rows: before=%+v after=%+v", before, after)
	}
}

func TestCommitMetadataSnapshotPersistsAcrossRestartAndConcurrentCategories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := OpenDatabase(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	seedMetadataInstances(t, db)
	now := time.Date(2026, 8, 3, 12, 36, 1, 0, time.UTC)
	authBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"authIndex":"same-auth","name":"a.json","displayName":"A","type":"codex","provider":"codex","prefix":"","priority":null,"disabled":false,"note":null,"accountId":null,"projectId":null,"xaiUserId":null,"activeStart":null,"activeUntil":null,"planType":null}]}`)
	providerBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"authIndex":"same-auth","providerType":"openai-compatibility","displayName":"Provider","prefix":"","baseUrl":"https://example.com/v1","priority":null,"disabled":false,"note":null,"apiKeyFingerprint":null}]}`)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, input := range []struct {
		instance string
		category protocol.MetadataCategory
		body     []byte
	}{{metadataInstanceA, protocol.CategoryAuthFiles, authBody}, {metadataInstanceB, protocol.CategoryProviderIdentities, providerBody}} {
		wg.Add(1)
		go func(input struct {
			instance string
			category protocol.MetadataCategory
			body     []byte
		}) {
			defer wg.Done()
			snapshot := decodeMetadataBody(t, input.body, input.category)
			_, err := CommitMetadataSnapshot(context.Background(), db, input.instance, input.category, snapshot, input.body, now)
			errs <- err
		}(input)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	closeMetadataDB(t, db)

	db, err = OpenDatabase(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer closeMetadataDB(t, db)
	replay, err := CommitMetadataSnapshot(context.Background(), db, metadataInstanceA, protocol.CategoryAuthFiles, decodeMetadataBody(t, authBody, protocol.CategoryAuthFiles), authBody, now.Add(time.Hour))
	if err != nil || replay.Applied {
		t.Fatalf("restart replay=%+v err=%v", replay, err)
	}
	var ledgers []entities.CPAMetadataSnapshot
	if err := db.Find(&ledgers).Error; err != nil || len(ledgers) != 2 {
		t.Fatalf("ledgers=%+v err=%v", ledgers, err)
	}
}

func openMetadataDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "metadata.db")})
	if err != nil {
		t.Fatal(err)
	}
	seedMetadataInstances(t, db)
	t.Cleanup(func() { closeMetadataDB(t, db) })
	return db
}
func seedMetadataInstances(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	for _, id := range []string{metadataInstanceA, metadataInstanceB} {
		if err := db.Create(&entities.CPAInstance{ID: id, DisplayName: id, Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
}
func closeMetadataDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
func decodeMetadataBody(t *testing.T, body []byte, category protocol.MetadataCategory) *protocol.MetadataSnapshot {
	t.Helper()
	snapshot, perr := protocol.DecodeMetadataSnapshot(body, category)
	if perr != nil {
		t.Fatalf("decode metadata: %v", perr)
	}
	return snapshot
}
func applyMetadataBody(t *testing.T, db *gorm.DB, instance string, category protocol.MetadataCategory, body []byte, now time.Time) {
	t.Helper()
	result, err := CommitMetadataSnapshot(context.Background(), db, instance, category, decodeMetadataBody(t, body, category), body, now)
	if err != nil || !result.Applied {
		t.Fatalf("commit result=%+v err=%v", result, err)
	}
}
