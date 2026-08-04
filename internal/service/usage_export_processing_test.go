package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageExportInboxProcessesForTrustedInstanceWithoutRequestIDDedup(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "process.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	instanceID := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	streamID := "0198aa11-1055-7f12-8a00-e843d1e17522"
	now := time.Now().UTC()
	if err := db.Create(&entities.CPAInstance{ID: instanceID, DisplayName: "push", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"timestamp":"2026-08-03T12:34:56.789Z","latency_ms":1,"ttft_ms":null,"source":"openai","auth_index":"auth","client_ip":null,"x_forwarded_for":null,"user_agent":null,"tokens":{"input_tokens":1,"output_tokens":1,"reasoning_tokens":0,"cached_tokens":0,"cache_read_tokens":0,"cache_read_tokens_present":true,"cache_creation_tokens":0,"total_tokens":2},"failed":false,"generate":true,"fail":{"status_code":200,"code":null},"accounting_version":1,"token_breakdown":{"input":1,"cached":0,"cache_read":0,"cache_creation":0,"reasoning":0,"output":1},"provider":"openai","executor_type":"CodexExecutor","model":"model","alias":"model","endpoint":"/v1/responses","auth_type":"apikey","api_key_fingerprint":null,"request_id":"duplicate-correlation","reasoning_effort":"","service_tier":"","response_service_tier":null,"response_headers":null}`)
	batch := &protocol.UsageBatch{StreamID: streamID, Events: []protocol.UsageEvent{{Sequence: 1, RawPayload: raw}, {Sequence: 2, RawPayload: raw}}}
	if _, err := repository.CommitUsageBatch(context.Background(), db, instanceID, batch, now, repository.UsageBatchCommitHooks{}); err != nil {
		t.Fatal(err)
	}

	syncService := NewSyncServiceWithOptions(db, SyncServiceOptions{})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("process push inbox: %v", err)
	}
	if result.InsertedEvents != 2 {
		t.Fatalf("inserted=%d, want 2", result.InsertedEvents)
	}
	var events []entities.UsageEvent
	if err := db.Where("instance_id = ?", instanceID).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].RequestID != "duplicate-correlation" || events[1].RequestID != "duplicate-correlation" {
		t.Fatalf("events=%+v", events)
	}
	var processed int64
	db.Model(&entities.RedisUsageInbox{}).Where("instance_id = ? AND status = ?", instanceID, repository.RedisUsageInboxStatusProcessed).Count(&processed)
	if processed != 2 {
		t.Fatalf("processed inbox=%d, want 2", processed)
	}
}
