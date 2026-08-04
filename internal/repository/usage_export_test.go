package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	testExportInstanceA = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	testExportInstanceB = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb12"
	testExportStreamA   = "0198aa11-1055-7f12-8a00-e843d1e17522"
	testExportStreamB   = "0198aa11-1055-7f12-8a00-e843d1e17523"
)

func TestCommitUsageBatchContiguousReplayGapAndIsolation(t *testing.T) {
	db := openUsageExportTestDB(t)
	seedUsageExportInstance(t, db, testExportInstanceA)
	seedUsageExportInstance(t, db, testExportInstanceB)
	now := time.Date(2026, 8, 3, 12, 34, 56, 789000000, time.UTC)

	assertCommit := func(instanceID, streamID string, sequences []int64, wantACK uint64, wantAccepted, wantReplayed int) {
		t.Helper()
		batch := usageExportBatch(streamID, sequences...)
		got, err := CommitUsageBatch(context.Background(), db, instanceID, batch, now, UsageBatchCommitHooks{})
		if err != nil {
			t.Fatalf("CommitUsageBatch(%s, %v): %v", instanceID, sequences, err)
		}
		if got.AcknowledgedThrough != wantACK || got.AcceptedCount != wantAccepted || got.ReplayedCount != wantReplayed {
			t.Fatalf("result = %+v, want ACK=%d accepted=%d replayed=%d", got, wantACK, wantAccepted, wantReplayed)
		}
	}

	assertCommit(testExportInstanceA, testExportStreamA, []int64{1, 2}, 2, 2, 0)
	assertCommit(testExportInstanceA, testExportStreamA, []int64{1, 2}, 2, 0, 2)
	assertCommit(testExportInstanceA, testExportStreamA, []int64{4}, 2, 1, 0)
	assertCommit(testExportInstanceA, testExportStreamA, []int64{3}, 4, 1, 0)
	assertCommit(testExportInstanceA, testExportStreamB, []int64{1}, 1, 1, 0)
	assertCommit(testExportInstanceB, testExportStreamA, []int64{1}, 1, 1, 0)

	var deliveries, inboxes, events int64
	db.Model(&entities.CPAUsageDelivery{}).Count(&deliveries)
	db.Model(&entities.RedisUsageInbox{}).Count(&inboxes)
	db.Model(&entities.UsageEvent{}).Count(&events)
	if deliveries != 6 || inboxes != 6 || events != 0 {
		t.Fatalf("counts deliveries=%d inboxes=%d events=%d, want 6/6/0 before async processing", deliveries, inboxes, events)
	}
}

func TestCommitUsageBatchConflictAndFaultRollback(t *testing.T) {
	db := openUsageExportTestDB(t)
	seedUsageExportInstance(t, db, testExportInstanceA)
	now := time.Now().UTC()
	batch := usageExportBatch(testExportStreamA, 1)
	if _, err := CommitUsageBatch(context.Background(), db, testExportInstanceA, batch, now, UsageBatchCommitHooks{}); err != nil {
		t.Fatal(err)
	}
	conflict := usageExportBatch(testExportStreamA, 1, 2)
	conflict.Events[0].RawPayload = []byte(`{"request_id":"different"}`)
	if _, err := CommitUsageBatch(context.Background(), db, testExportInstanceA, conflict, now, UsageBatchCommitHooks{}); !errors.Is(err, ErrConflictingUsageReplay) {
		t.Fatalf("conflict error = %v", err)
	}
	rollback := usageExportBatch(testExportStreamA, 2)
	injected := errors.New("injected before commit")
	if _, err := CommitUsageBatch(context.Background(), db, testExportInstanceA, rollback, now, UsageBatchCommitHooks{BeforeCommit: func() error { return injected }}); !errors.Is(err, injected) {
		t.Fatalf("fault error = %v", err)
	}
	var deliveries int64
	db.Model(&entities.CPAUsageDelivery{}).Where("instance_id = ?", testExportInstanceA).Count(&deliveries)
	if deliveries != 1 {
		t.Fatalf("deliveries after conflict/fault = %d, want 1", deliveries)
	}
}

func TestCommitUsageBatchStoresExactRawPayloadDigest(t *testing.T) {
	db := openUsageExportTestDB(t)
	seedUsageExportInstance(t, db, testExportInstanceA)
	raw := []byte("{ \"request_id\" : \"same\", \"model\" : \"m\" }")
	batch := &protocol.UsageBatch{StreamID: testExportStreamA, Events: []protocol.UsageEvent{{Sequence: 1, RawPayload: raw}}}
	if _, err := CommitUsageBatch(context.Background(), db, testExportInstanceA, batch, time.Now().UTC(), UsageBatchCommitHooks{}); err != nil {
		t.Fatal(err)
	}
	var delivery entities.CPAUsageDelivery
	if err := db.Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(raw)
	if !bytes.Equal(delivery.PayloadDigest, want[:]) {
		t.Fatalf("digest=%x want=%x", delivery.PayloadDigest, want)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, delivery.InboxID).Error; err != nil {
		t.Fatal(err)
	}
	if inbox.RawMessage != string(raw) {
		t.Fatalf("raw inbox=%q want=%q", inbox.RawMessage, raw)
	}
}

func TestCommitUsageBatchConcurrentDuplicate(t *testing.T) {
	db := openUsageExportTestDB(t)
	seedUsageExportInstance(t, db, testExportInstanceA)
	batch := usageExportBatch(testExportStreamA, 1)
	start := make(chan struct{})
	results := make(chan UsageBatchCommitResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := CommitUsageBatch(context.Background(), db, testExportInstanceA, batch, time.Now().UTC(), UsageBatchCommitHooks{})
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent commit: %v", err)
		}
	}
	accepted, replayed := 0, 0
	for result := range results {
		accepted += result.AcceptedCount
		replayed += result.ReplayedCount
		if result.AcknowledgedThrough != 1 {
			t.Fatalf("ACK = %d, want 1", result.AcknowledgedThrough)
		}
	}
	if accepted != 1 || replayed != 1 {
		t.Fatalf("accepted=%d replayed=%d, want 1/1", accepted, replayed)
	}
}

func openUsageExportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-export.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return db
}

func seedUsageExportInstance(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&entities.CPAInstance{ID: id, DisplayName: id, Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
}

func usageExportBatch(streamID string, sequences ...int64) *protocol.UsageBatch {
	events := make([]protocol.UsageEvent, 0, len(sequences))
	for _, sequence := range sequences {
		raw := []byte(`{"timestamp":"2026-08-03T12:34:56.789Z","latency_ms":1,"ttft_ms":null,"source":"openai","auth_index":"auth","client_ip":null,"x_forwarded_for":null,"user_agent":null,"tokens":{"input_tokens":1,"output_tokens":1,"reasoning_tokens":0,"cached_tokens":0,"cache_read_tokens":0,"cache_read_tokens_present":true,"cache_creation_tokens":0,"total_tokens":2},"failed":false,"generate":true,"fail":{"status_code":200,"code":null},"accounting_version":1,"token_breakdown":{"input":1,"cached":0,"cache_read":0,"cache_creation":0,"reasoning":0,"output":1},"provider":"openai","executor_type":"executor","model":"model","alias":"model","endpoint":"/v1/responses","auth_type":"apikey","api_key_fingerprint":null,"request_id":"same-request-id","reasoning_effort":"","service_tier":"","response_service_tier":null,"response_headers":null}`)
		events = append(events, protocol.UsageEvent{Sequence: sequence, RawPayload: raw, Payload: protocol.UsagePayload{RequestID: "same-request-id"}})
	}
	return &protocol.UsageBatch{StreamID: streamID, Events: events}
}
