package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageExportServiceACKInvariantConflictAndInterruption(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "service.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	instanceID := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	streamID := "0198aa11-1055-7f12-8a00-e843d1e17522"
	now := time.Now().UTC()
	if err := db.Create(&entities.CPAInstance{ID: instanceID, DisplayName: "test", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewUsageExportService(db)

	batch := func(sequence int64, raw string) *protocol.UsageBatch {
		return &protocol.UsageBatch{StreamID: streamID, Events: []protocol.UsageEvent{{Sequence: sequence, RawPayload: []byte(raw)}}}
	}
	ack, perr := service.IngestUsageBatch(context.Background(), instanceID, batch(2, `{"request_id":"two"}`))
	if perr != nil || ack.AcknowledgedThrough != 0 || ack.NextExpectedSequence != 1 {
		t.Fatalf("gap ack=%+v err=%v", ack, perr)
	}
	ack, perr = service.IngestUsageBatch(context.Background(), instanceID, batch(1, `{"request_id":"one"}`))
	if perr != nil || ack.AcknowledgedThrough != 2 || ack.NextExpectedSequence != 3 {
		t.Fatalf("fill ack=%+v err=%v", ack, perr)
	}
	ack, perr = service.IngestUsageBatch(context.Background(), instanceID, batch(1, `{"request_id":"one"}`))
	if perr != nil || ack.AcceptedCount != 0 || ack.ReplayedCount != 1 || ack.AcknowledgedThrough != 2 {
		t.Fatalf("replay ack=%+v err=%v", ack, perr)
	}
	if _, perr = service.IngestUsageBatch(context.Background(), instanceID, batch(1, `{"request_id":"conflict"}`)); perr == nil || perr.Code != "conflicting_replay" {
		t.Fatalf("conflict=%v", perr)
	}

	injected := errors.New("disconnect before commit")
	service.SetCommitHooks(repository.UsageBatchCommitHooks{BeforeCommit: func() error { return injected }})
	if _, perr = service.IngestUsageBatch(context.Background(), instanceID, batch(3, `{"request_id":"three"}`)); perr == nil || perr.Code != "storage_error" {
		t.Fatalf("fault=%v", perr)
	}
	service.SetCommitHooks(repository.UsageBatchCommitHooks{})
	ack, perr = service.IngestUsageBatch(context.Background(), instanceID, batch(3, `{"request_id":"three"}`))
	if perr != nil || ack.AcknowledgedThrough != 3 || ack.NextExpectedSequence != 4 {
		t.Fatalf("retry ack=%+v err=%v", ack, perr)
	}
}
