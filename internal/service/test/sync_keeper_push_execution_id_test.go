package test

import (
	"context"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/service"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

// The keeper_push contract (protocol.usagePayloadRequiredKeys) carries request_id and no execution_id.
const keeperPushMessageWithoutExecutionID = `{"timestamp":"2026-04-27T08:00:00Z","provider":"openai-compatible-x","executor_type":"OpenAICompatExecutor","model":"m","request_id":"push-request-1","tokens":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`

func processSingleInboxMessage(t *testing.T, source, message string) (entities.RedisUsageInbox, error) {
	t.Helper()
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{Source: source, RawMessage: message, PoppedAt: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatalf("seed inbox: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{PricingCatalog: emptyPricingCatalogForTest(), BaseURL: "https://cpa.example.com"})
	_, err := service.ProcessRedisUsageInbox(context.Background())
	var inbox entities.RedisUsageInbox
	if loadErr := db.First(&inbox).Error; loadErr != nil {
		t.Fatalf("load inbox row: %v", loadErr)
	}
	return inbox, err
}

func TestProcessRedisUsageInboxAcceptsKeeperPushWithoutExecutionID(t *testing.T) {
	inbox, err := processSingleInboxMessage(t, repository.RedisUsageInboxSourceKeeperPush, keeperPushMessageWithoutExecutionID)
	if err != nil {
		t.Fatalf("keeper_push message without execution_id must process: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("expected processed row, got status=%q last_error=%q", inbox.Status, inbox.LastError)
	}
}

func TestProcessRedisUsageInboxStillRejectsRedisPullWithoutExecutionID(t *testing.T) {
	inbox, err := processSingleInboxMessage(t, redisUsageInboxTestSource, keeperPushMessageWithoutExecutionID)
	if err == nil {
		t.Fatalf("redis_pull message without execution_id must still be rejected")
	}
	if inbox.Status != repository.RedisUsageInboxStatusDecodeFailed {
		t.Fatalf("expected decode_failed row, got status=%q", inbox.Status)
	}
}
