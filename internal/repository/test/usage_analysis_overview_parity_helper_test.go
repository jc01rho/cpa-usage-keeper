package test

import (
	"testing"

	"gorm.io/gorm"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

// requireAnalysisMatchesOverview asserts that Analysis reports the same non-zero totals as Overview
// for one filter, which is the contract for rolling ranges.
func requireAnalysisMatchesOverview(t *testing.T, db *gorm.DB, filter repodto.UsageQueryFilter, analysis *repodto.AnalysisRecord) {
	t.Helper()
	overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilterAndRecentCache returned error: %v", err)
	}
	var tokens, requests int64
	for _, bucket := range analysis.TokenUsage {
		tokens += bucket.TotalTokens
		requests += bucket.Requests
	}
	if tokens == 0 {
		t.Fatalf("expected non-zero analysis totals, got %+v", analysis.TokenUsage)
	}
	if tokens != overview.Usage.TotalTokens || requests != overview.Usage.TotalRequests {
		t.Fatalf("analysis totals = %d tokens/%d requests, overview = %d/%d", tokens, requests, overview.Usage.TotalTokens, overview.Usage.TotalRequests)
	}
}
