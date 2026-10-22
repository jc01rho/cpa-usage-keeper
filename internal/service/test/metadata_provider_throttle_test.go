package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestProviderMetadataSyncThrottlesProviderFetchWithinInterval(t *testing.T) {
	db := openMetadataTestDatabase(t, "provider-metadata-throttle.db")
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	fetcher := newMetadataTestFetcher()
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-codex", AuthIndex: "auth-codex", Prefix: "prefix-codex", BaseURL: "https://codex.example/v1", Name: "Codex Team"}}}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                         "https://cpa.example.com",
		MetadataFetcher:                 fetcher,
		Now:                             func() time.Time { return now },
		ProviderMetadataRefreshInterval: 10 * time.Minute,
	})

	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("first SyncMetadata returned error: %v", err)
	}
	if got := fetcher.callCount("codex"); got != 1 {
		t.Fatalf("first round provider fetch count = %d, want 1", got)
	}

	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("second SyncMetadata returned error: %v", err)
	}
	if got := fetcher.callCount("codex"); got != 1 {
		t.Fatalf("provider endpoint refetched inside TTL: count = %d, want 1", got)
	}
	if got := fetcher.callCount("xai"); got != 1 {
		t.Fatalf("second provider endpoint refetched inside TTL: count = %d, want 1", got)
	}
	if got := fetcher.callCount("credentials"); got != 2 {
		t.Fatalf("credentials fetch count = %d, want 2", got)
	}
	if got := fetcher.callCount("management-api-keys"); got != 2 {
		t.Fatalf("management api keys fetch count = %d, want 2", got)
	}
	// 节流跳过的一轮不能按空快照做 scoped replace，否则已有 provider 行会被清掉。
	identities := loadMetadataIdentityMap(t, db)
	codexRow, ok := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-codex")]
	if !ok {
		t.Fatalf("throttled round dropped provider identity: %+v", identities)
	}
	if codexRow.IsDeleted {
		t.Fatalf("throttled round marked provider identity deleted: %+v", codexRow)
	}

	now = now.Add(11 * time.Minute)
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("third SyncMetadata returned error: %v", err)
	}
	if got := fetcher.callCount("codex"); got != 2 {
		t.Fatalf("provider endpoint not refetched after TTL: count = %d, want 2", got)
	}
}

func TestProviderMetadataSyncWithoutIntervalFetchesEveryRound(t *testing.T) {
	db := openMetadataTestDatabase(t, "provider-metadata-no-throttle.db")
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	fetcher := newMetadataTestFetcher()
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
		Now:             func() time.Time { return now },
	})
	for round := 1; round <= 2; round++ {
		if err := syncer.SyncMetadata(context.Background()); err != nil {
			t.Fatalf("SyncMetadata round %d returned error: %v", round, err)
		}
		if got := fetcher.callCount("codex"); got != round {
			t.Fatalf("round %d provider fetch count = %d, want %d", round, got, round)
		}
	}
}

func TestProviderMetadataSyncRetriesAfterFetchFailure(t *testing.T) {
	db := openMetadataTestDatabase(t, "provider-metadata-retry.db")
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	fetcher := newMetadataTestFetcher()
	fetcher.standardErrors["codex"] = context.DeadlineExceeded
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                         "https://cpa.example.com",
		MetadataFetcher:                 fetcher,
		Now:                             func() time.Time { return now },
		ProviderMetadataRefreshInterval: 10 * time.Minute,
	})
	if err := syncer.SyncMetadata(context.Background()); err == nil {
		t.Fatal("expected provider fetch warning on first round")
	}
	delete(fetcher.standardErrors, "codex")
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("second SyncMetadata returned error: %v", err)
	}
	if got := fetcher.callCount("codex"); got != 2 {
		t.Fatalf("provider endpoint not retried after failure: count = %d, want 2", got)
	}
}
