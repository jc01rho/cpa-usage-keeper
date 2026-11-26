package test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// TestUsageAnalysisMergesSiblingKeyNamedLikeMainRegistration locks the rule that a key whose name matches a
// main instance registration shows as that registration's row instead of a separate per-instance row.
func TestUsageAnalysisMergesSiblingKeyNamedLikeMainRegistration(t *testing.T) {
	bucket := time.Date(2026, 4, 22, 10, 0, 0, 0, time.Local)
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage:  []servicedto.AnalysisTokenUsageBucket{{Bucket: bucket, TotalTokens: 130, Requests: 5}},
		APIKeyComposition: []servicedto.AnalysisCompositionItem{
			{InstanceID: entities.LegacyCPAInstanceID, Key: "main-raw-key", TotalTokens: 100, Requests: 2},
			{InstanceID: "instance-a", Key: "claude", TotalTokens: 30, Requests: 3},
		},
		ModelComposition: []servicedto.AnalysisCompositionItem{{Key: "claude-sonnet", TotalTokens: 130, Requests: 5}},
		Heatmap: []servicedto.AnalysisHeatmapCell{
			{InstanceID: entities.LegacyCPAInstanceID, APIKey: "main-raw-key", Model: "claude-sonnet", TotalTokens: 100, Requests: 2},
			{InstanceID: "instance-a", APIKey: "claude", Model: "claude-sonnet", TotalTokens: 30, Requests: 3},
		},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: usageAnalysisAPIKeyStub{rows: []entities.CPAAPIKey{
		{ID: 59, InstanceID: entities.LegacyCPAInstanceID, APIKey: "main-raw-key", DisplayKey: "sk-masked-main", KeyAlias: "claude"},
		{ID: 70, InstanceID: entities.LegacyCPAInstanceID, APIKey: "deleted-key", DisplayKey: "sk-masked-gone", KeyAlias: "commandcode", IsDeleted: true},
	}}})
	resp := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	var payload struct {
		APIKeyComposition []struct {
			Key         string  `json:"key"`
			Label       string  `json:"label"`
			TotalTokens int64   `json:"total_tokens"`
			Requests    int64   `json:"requests"`
			Percent     float64 `json:"percent"`
		} `json:"api_key_composition"`
		Heatmap struct {
			APIKeys []string `json:"api_keys"`
		} `json:"heatmap"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode analysis response: %v", err)
	}
	if len(payload.APIKeyComposition) != 1 {
		t.Fatalf("expected the sibling key named like the main registration to merge into one row, got %+v", payload.APIKeyComposition)
	}
	merged := payload.APIKeyComposition[0]
	if merged.Key != "59" || merged.Label != "claude" || merged.TotalTokens != 130 || merged.Requests != 5 {
		t.Fatalf("expected the main registration to absorb the same-named sibling key, got %+v", merged)
	}
	if len(payload.Heatmap.APIKeys) != 1 {
		t.Fatalf("expected one heatmap row, got %+v", payload.Heatmap.APIKeys)
	}
}

// TestUsageAnalysisDeletedRegistrationDoesNotCaptureSameNamedKey keeps a deleted main registration from
// swallowing a sibling key that happens to share its name.
func TestUsageAnalysisDeletedRegistrationDoesNotCaptureSameNamedKey(t *testing.T) {
	bucket := time.Date(2026, 4, 22, 10, 0, 0, 0, time.Local)
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage:  []servicedto.AnalysisTokenUsageBucket{{Bucket: bucket, TotalTokens: 40, Requests: 2}},
		APIKeyComposition: []servicedto.AnalysisCompositionItem{
			{InstanceID: "instance-a", Key: "commandcode", TotalTokens: 40, Requests: 2},
		},
		ModelComposition: []servicedto.AnalysisCompositionItem{{Key: "claude-sonnet", TotalTokens: 40, Requests: 2}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: usageAnalysisAPIKeyStub{rows: []entities.CPAAPIKey{
		{ID: 70, InstanceID: entities.LegacyCPAInstanceID, APIKey: "deleted-key", DisplayKey: "sk-masked-gone", KeyAlias: "commandcode", IsDeleted: true},
	}}})
	resp := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	var payload struct {
		APIKeyComposition []struct {
			Key         string `json:"key"`
			TotalTokens int64  `json:"total_tokens"`
		} `json:"api_key_composition"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode analysis response: %v", err)
	}
	if len(payload.APIKeyComposition) != 1 || payload.APIKeyComposition[0].TotalTokens != 40 {
		t.Fatalf("expected the sibling key to keep its own row, got %+v", payload.APIKeyComposition)
	}
}
