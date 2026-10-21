package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	keeperservice "cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
)

const mergedKeyTestInstanceB = "01a02a13-d927-73ac-807b-ef7daf07f8d4"

func seedMergedKeyInstance(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAInstance{ID: mergedKeyTestInstanceB, DisplayName: "B", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed CPA instance: %v", err)
	}
}

func TestListCPAAPIKeysMergesSameKeyAcrossInstances(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	seedMergedKeyInstance(t, db)
	seededAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	if err := repository.SyncCPAAPIKeysForInstance(db, entities.LegacyCPAInstanceID, []string{"sk-shared123456", "sk-legacy123456"}, seededAt); err != nil {
		t.Fatalf("seed legacy API keys: %v", err)
	}
	if err := repository.SyncCPAAPIKeysForInstance(db, mergedKeyTestInstanceB, []string{"sk-shared123456", "sk-second123456"}, seededAt); err != nil {
		t.Fatalf("seed second instance API keys: %v", err)
	}
	provider := keeperservice.NewCPAAPIKeyService(db)

	rows, err := provider.ListCPAAPIKeys(keeperservice.ContextWithInstanceFilter(context.Background(), ""))
	if err != nil {
		t.Fatalf("ListCPAAPIKeys returned error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("merged row count = %d, want 3: %+v", len(rows), rows)
	}
	shared := findAPIKeyRow(t, rows, "sk-shared123456")
	if shared.InstanceID != entities.LegacyCPAInstanceID {
		t.Fatalf("merged representative instance = %q, want legacy instance", shared.InstanceID)
	}

	scoped, err := provider.ListCPAAPIKeys(keeperservice.ContextWithInstanceFilter(context.Background(), mergedKeyTestInstanceB))
	if err != nil {
		t.Fatalf("scoped ListCPAAPIKeys returned error: %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("scoped row count = %d, want 2: %+v", len(scoped), scoped)
	}
	for _, row := range scoped {
		if row.InstanceID != mergedKeyTestInstanceB {
			t.Fatalf("scoped row leaked another instance: %+v", row)
		}
	}
}

func TestListCPAAPIKeysMergedRowKeepsFirstAlias(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	seedMergedKeyInstance(t, db)
	seededAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	if err := repository.SyncCPAAPIKeysForInstance(db, entities.LegacyCPAInstanceID, []string{"sk-shared123456"}, seededAt); err != nil {
		t.Fatalf("seed legacy API keys: %v", err)
	}
	if err := repository.SyncCPAAPIKeysForInstance(db, mergedKeyTestInstanceB, []string{"sk-shared123456"}, seededAt); err != nil {
		t.Fatalf("seed second instance API keys: %v", err)
	}
	rows, err := repository.ListActiveCPAAPIKeysForInstance(db, "")
	if err != nil {
		t.Fatalf("list API keys: %v", err)
	}
	second := findAPIKeyRow(t, rows, "sk-shared123456", mergedKeyTestInstanceB)
	if err := repository.UpdateCPAAPIKeyAlias(db, second.ID, "team-alias"); err != nil {
		t.Fatalf("set alias on second instance: %v", err)
	}

	provider := keeperservice.NewCPAAPIKeyService(db)
	merged, err := provider.ListCPAAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListCPAAPIKeys returned error: %v", err)
	}
	if len(merged) != 1 {
		t.Fatalf("merged row count = %d, want 1: %+v", len(merged), merged)
	}
	if merged[0].KeyAlias != "team-alias" {
		t.Fatalf("merged alias = %q, want the only non-empty alias", merged[0].KeyAlias)
	}
}

func TestUpdateCPAAPIKeyAliasAppliesToEveryInstanceInAllView(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	seedMergedKeyInstance(t, db)
	seededAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	if err := repository.SyncCPAAPIKeysForInstance(db, entities.LegacyCPAInstanceID, []string{"sk-shared123456"}, seededAt); err != nil {
		t.Fatalf("seed legacy API keys: %v", err)
	}
	if err := repository.SyncCPAAPIKeysForInstance(db, mergedKeyTestInstanceB, []string{"sk-shared123456"}, seededAt); err != nil {
		t.Fatalf("seed second instance API keys: %v", err)
	}
	rows, err := repository.ListActiveCPAAPIKeysForInstance(db, "")
	if err != nil {
		t.Fatalf("list API keys: %v", err)
	}
	representative := findAPIKeyRow(t, rows, "sk-shared123456")

	provider := keeperservice.NewCPAAPIKeyService(db)
	if _, err := provider.UpdateCPAAPIKeyAlias(keeperservice.ContextWithInstanceFilter(context.Background(), ""), representative.ID, "shared-alias"); err != nil {
		t.Fatalf("UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	after, err := repository.ListActiveCPAAPIKeysForInstance(db, "")
	if err != nil {
		t.Fatalf("relist API keys: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("stored row count = %d, want 2", len(after))
	}
	for _, row := range after {
		if row.KeyAlias != "shared-alias" {
			t.Fatalf("instance %q alias = %q, want shared-alias", row.InstanceID, row.KeyAlias)
		}
	}

	second := findAPIKeyRow(t, after, "sk-shared123456", mergedKeyTestInstanceB)
	if _, err := provider.UpdateCPAAPIKeyAlias(keeperservice.ContextWithInstanceFilter(context.Background(), mergedKeyTestInstanceB), second.ID, "scoped-alias"); err != nil {
		t.Fatalf("scoped UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	scoped, err := repository.ListActiveCPAAPIKeysForInstance(db, "")
	if err != nil {
		t.Fatalf("relist API keys: %v", err)
	}
	if scoped[0].KeyAlias != "shared-alias" || scoped[1].KeyAlias != "scoped-alias" {
		t.Fatalf("scoped alias edit leaked across instances: %+v", scoped)
	}
}

func findAPIKeyRow(t *testing.T, rows []entities.CPAAPIKey, apiKey string, instanceID ...string) entities.CPAAPIKey {
	t.Helper()
	for _, row := range rows {
		if row.APIKey != apiKey {
			continue
		}
		if len(instanceID) > 0 && row.InstanceID != instanceID[0] {
			continue
		}
		return row
	}
	t.Fatalf("API key %q not found in %+v", apiKey, rows)
	return entities.CPAAPIKey{}
}
