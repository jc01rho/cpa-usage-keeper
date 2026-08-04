package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

// Baseline characterization tests for the keeper-export/v1 protocol work
// (contract section 12.2). They freeze the current Redis decode and inbox
// transaction behavior that the v1 push path must stay compatible with, and
// they pass against the unchanged pre-v1 code.

// TestBaselineRedisUsageDecodeDuplicateRequestIDYieldsIndependentEvents freezes
// the rule that request_id is correlation-only: two messages sharing one
// request_id decode into two independent events, and nothing in the decode
// path treats the duplicate as a conflict or dedup signal.
func TestBaselineRedisUsageDecodeDuplicateRequestIDYieldsIndependentEvents(t *testing.T) {
	fetchedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	decode := func(model string) entities.UsageEvent {
		t.Helper()
		event, _, err := DecodeRedisUsageMessage(`{
			"timestamp":"2026-04-27T07:59:00Z",
			"provider":"claude",
			"model":"`+model+`",
			"api_key":"raw-key",
			"request_id":"req-duplicated",
			"tokens":{"input_tokens":1,"output_tokens":2}
		}`, fetchedAt)
		if err != nil {
			t.Fatalf("DecodeRedisUsageMessage(%s) returned error: %v", model, err)
		}
		return event
	}

	first := decode("claude-sonnet")
	second := decode("claude-opus")
	if first.RequestID != "req-duplicated" || second.RequestID != "req-duplicated" {
		t.Fatalf("request_id must decode on both events: %q vs %q", first.RequestID, second.RequestID)
	}
	if first.EventKey != "req-duplicated" || second.EventKey != "req-duplicated" {
		t.Fatalf("event_key mirrors request_id for correlation only: %q vs %q", first.EventKey, second.EventKey)
	}
	if first.Model != "claude-sonnet" || second.Model != "claude-opus" {
		t.Fatalf("duplicate request_id events stay independent: %+v vs %+v", first, second)
	}
}

// TestBaselineRedisUsageLegacyPayloadCarriesRawSecrets documents why the v1
// projection must sanitize: the legacy Redis payload carries the raw API key,
// the raw upstream failure body, and arbitrary response headers, and the
// current decoder accepts all of it. The raw key even becomes the legacy
// api_group_key fallback, which v1 replaces with the HMAC fingerprint.
func TestBaselineRedisUsageLegacyPayloadCarriesRawSecrets(t *testing.T) {
	fetchedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	event, raw, snapshot, err := DecodeRedisUsageMessageWithHeaders(`{
		"timestamp":"2026-04-27T07:59:00Z",
		"provider":"openai",
		"model":"gpt-5.6",
		"api_key":"sk-raw-secret-key",
		"request_id":"req-secret-baseline",
		"failed":true,
		"fail":{"status_code":502,"body":"upstream raw secret body"},
		"tokens":{"input_tokens":1,"output_tokens":2},
		"response_headers":{"authorization":["Bearer leaked"],"x-arbitrary-provider-header":["value"]}
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("legacy decoder must stay permissive, got error: %v", err)
	}
	if event.APIGroupKey != "sk-raw-secret-key" {
		t.Fatalf("legacy grouping falls back to the raw api key, got %q", event.APIGroupKey)
	}
	if !strings.Contains(string(raw), "sk-raw-secret-key") || !strings.Contains(string(raw), "upstream raw secret body") {
		t.Fatalf("raw inbox message retains the secret-bearing legacy payload: %s", string(raw))
	}
	// Arbitrary/secret headers must not become a quota snapshot, and must not
	// reject the event either.
	if snapshot != nil {
		t.Fatalf("unexpected quota snapshot from non-codex headers: %+v", snapshot)
	}
}

// TestBaselineProcessRedisUsageInboxAtomicDuplicateRequestID freezes the
// raw-inbox-first atomic transaction: two inbox rows with the same request_id
// persist as two usage_events rows and both inbox rows flip to processed in
// the same commit.
func TestBaselineProcessRedisUsageInboxAtomicDuplicateRequestID(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{
		{
			Source:     redisUsageInboxTestSource,
			RawMessage: `{"timestamp":"2026-04-27T07:59:00Z","provider":"claude","model":"claude-sonnet","request_id":"req-atomic-dup","tokens":{"input_tokens":1,"output_tokens":2}}`,
			PoppedAt:   poppedAt,
		},
		{
			Source:     redisUsageInboxTestSource,
			RawMessage: `{"timestamp":"2026-04-27T07:59:05Z","provider":"claude","model":"claude-opus","request_id":"req-atomic-dup","tokens":{"input_tokens":3,"output_tokens":4}}`,
			PoppedAt:   poppedAt,
		},
	})
	if err != nil {
		t.Fatalf("seed inbox rows: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 2 {
		t.Fatalf("expected both duplicate request_id events to persist, got %+v", result)
	}

	var events []entities.UsageEvent
	if err := db.Order("id asc").Find(&events).Error; err != nil {
		t.Fatalf("list usage events: %v", err)
	}
	if len(events) != 2 || events[0].RequestID != "req-atomic-dup" || events[1].RequestID != "req-atomic-dup" {
		t.Fatalf("expected two independent events sharing request_id, got %+v", events)
	}
	if events[0].ID == events[1].ID || events[0].Model == events[1].Model {
		t.Fatalf("duplicate request_id rows must remain distinct events: %+v", events)
	}

	for _, row := range rows {
		var inbox entities.RedisUsageInbox
		if err := db.First(&inbox, row.ID).Error; err != nil {
			t.Fatalf("load inbox row %d: %v", row.ID, err)
		}
		if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "req-atomic-dup" {
			t.Fatalf("inbox row %d must be marked processed in the same transaction, got %+v", row.ID, inbox)
		}
	}
}
