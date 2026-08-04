package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestTask6ManualQALiveHTTP(t *testing.T) {
	if os.Getenv("TASK6_MANUAL_QA") == "" {
		t.Skip("manual QA evidence driver")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qa.db")
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() }()
	instanceService := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(db))
	issue := func(name string) service.IssuedInstanceCredential {
		result, err := instanceService.Create(t.Context(), service.CreateInstanceInput{DisplayName: name, CredentialName: "qa", Scopes: []string{service.ScopeMetadataPush, service.ScopeIdentityTest}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	a, b := issue("A"), issue("B")
	usageService := service.NewUsageService(db, pricing.NewCatalog(pricing.EmptySnapshot()))
	qaNow := time.Now().Truncate(time.Second)
	localRankingService, err := ranking.NewLocalRankingService(db, ranking.LocalRankingServiceOptions{Now: func() time.Time { return qaNow }})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(nil, nil, usageService, nil, AuthConfig{}, NewAuthHandler(AuthConfig{}, nil), "", OptionalProviders{MetadataExport: service.NewMetadataExportService(db), MetadataStatus: service.NewMetadataStatusService(db), CPAInstances: instanceService, CPAAPIKeys: service.NewCPAAPIKeyService(db), UsageIdentity: service.NewUsageIdentityService(db), LocalRanking: localRankingService})
	server := httptest.NewServer(router)
	defer server.Close()
	body := func(revision int, alias string, empty bool) []byte {
		items := fmt.Sprintf(`[{"fingerprint":"akf1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","displayKey":"sk-...cdef","alias":%q}]`, alias)
		if empty {
			items = "[]"
		}
		return []byte(fmt.Sprintf(`{"protocolVersion":"keeper-export/v1","revision":%d,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":%s}`, revision, items))
	}
	putPath := func(token, path string, payload []byte) (int, string) {
		req, _ := http.NewRequest(http.MethodPut, server.URL+path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	put := func(token string, payload []byte) (int, string) {
		return putPath(token, "/api/v1/export/metadata/api_keys", payload)
	}
	statusA, responseA := put(a.Token, body(1, "A", false))
	statusB, responseB := put(b.Token, body(1, "B", false))
	usageAt := qaNow.Add(-time.Hour)
	for _, event := range []entities.UsageEvent{
		{InstanceID: a.Instance.ID, EventKey: "same-request", RequestID: "same-request", Timestamp: usageAt, AuthIndex: "same-auth", APIGroupKey: "akf1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Model: "same-model", TotalTokens: 10},
		{InstanceID: b.Instance.ID, EventKey: "same-request", RequestID: "same-request", Timestamp: usageAt, AuthIndex: "same-auth", APIGroupKey: "akf1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Model: "same-model", TotalTokens: 20},
	} {
		if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
			t.Fatal(err)
		}
	}
	replayStatus, replayResponse := put(a.Token, body(1, "A", false))
	conflictStatus, conflictResponse := put(a.Token, body(1, "DIFF", false))
	emptyStatus, emptyResponse := put(a.Token, body(2, "", true))
	staleStatus, staleResponse := put(a.Token, body(1, "A", false))
	selectorStatus, selectorResponse := putPath(b.Token, "/api/v1/export/metadata/api_keys?instance_id="+a.Instance.ID, body(2, "MUTATE", false))
	var rows []entities.CPAAPIKey
	if err := db.Order("instance_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	periodKey := usageAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	for _, row := range rows {
		tokens := int64(10)
		if row.InstanceID == b.Instance.ID {
			tokens = 20
		}
		if err := db.Create(&entities.LocalRankingPeriodStat{InstanceID: row.InstanceID, PeriodKind: entities.LocalRankingPeriodDay, PeriodKey: periodKey, APIKeyID: row.ID, RequestCount: 1, TotalTokens: tokens, UpdatedAt: usageAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	get := func(path string) (int, string) {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	eventsAStatus, eventsAResponse := get("/api/v1/usage/events?range=24h&instance_id=" + a.Instance.ID)
	eventsBStatus, eventsBResponse := get("/api/v1/usage/events?range=24h&instance_id=" + b.Instance.ID)
	eventsAllStatus, eventsAllResponse := get("/api/v1/usage/events?range=24h&instance_id=all")
	legacyStatus, legacyResponse := get("/api/v1/usage/events?range=24h")
	metadataStatusCode, metadataStatusResponse := get("/api/v1/instances/metadata-status?instance_id=all")
	localRankingAStatus, localRankingAResponse := get("/api/v1/ranking/local/leaderboards?period=today&metric=total_tokens&instance_id=" + a.Instance.ID)
	localRankingBStatus, localRankingBResponse := get("/api/v1/ranking/local/leaderboards?period=today&metric=total_tokens&instance_id=" + b.Instance.ID)
	localRankingAllStatus, localRankingAllResponse := get("/api/v1/ranking/local/leaderboards?period=today&metric=total_tokens&instance_id=all")
	var activeA, activeB int64
	db.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND is_deleted = ?", a.Instance.ID, false).Count(&activeA)
	db.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND is_deleted = ?", b.Instance.ID, false).Count(&activeB)
	evidence := map[string]any{"server": server.URL, "database": dbPath, "instanceA": a.Instance.ID, "instanceB": b.Instance.ID, "applyA": []any{statusA, json.RawMessage(responseA)}, "applyB": []any{statusB, json.RawMessage(responseB)}, "replay": []any{replayStatus, json.RawMessage(replayResponse)}, "conflict": []any{conflictStatus, json.RawMessage(conflictResponse)}, "stale": []any{staleStatus, json.RawMessage(staleResponse)}, "emptyA": []any{emptyStatus, json.RawMessage(emptyResponse)}, "selectorRejected": []any{selectorStatus, json.RawMessage(selectorResponse)}, "activeAfterEmpty": map[string]int64{"A": activeA, "B": activeB}, "eventsA": []any{eventsAStatus, json.RawMessage(eventsAResponse)}, "eventsB": []any{eventsBStatus, json.RawMessage(eventsBResponse)}, "eventsAll": []any{eventsAllStatus, json.RawMessage(eventsAllResponse)}, "legacyNoFilter": []any{legacyStatus, json.RawMessage(legacyResponse)}, "localRankingA": []any{localRankingAStatus, json.RawMessage(localRankingAResponse)}, "localRankingB": []any{localRankingBStatus, json.RawMessage(localRankingBResponse)}, "localRankingAll": []any{localRankingAllStatus, json.RawMessage(localRankingAllResponse)}, "metadataStatus": []any{metadataStatusCode, json.RawMessage(metadataStatusResponse)}, "rows": rows, "verifiedAt": time.Now().UTC()}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	fmt.Println(string(raw))
	var failures []string
	check := func(ok bool, name string) {
		if !ok {
			failures = append(failures, name)
		}
	}
	check(statusA == 200, "apply A status")
	check(statusB == 200, "apply B status")
	check(replayStatus == 200, "replay status")
	check(conflictStatus == 409, "conflict status")
	check(staleStatus == 409, "stale status")
	check(emptyStatus == 200, "empty snapshot status")
	check(selectorStatus == 400, "selector rejection status")
	check(activeA == 0, "instance A deletion")
	check(activeB == 1, "instance B preservation")
	check(eventsAStatus == 200, "events A status")
	check(eventsBStatus == 200, "events B status")
	check(eventsAllStatus == 200, "events all status")
	check(legacyStatus == 200, "legacy events status")
	check(metadataStatusCode == 200, "metadata status")
	check(localRankingAStatus == 200, "ranking A status")
	check(localRankingBStatus == 200, "ranking B status")
	check(localRankingAllStatus == 200, "ranking all status")
	check(strings.Count(eventsAResponse, `"instance_id"`) == 1, "events A instance count")
	check(strings.Count(eventsBResponse, `"instance_id"`) == 1, "events B instance count")
	check(strings.Count(eventsAllResponse, `"instance_id"`) == 2, "events all instance count")
	check(strings.Count(legacyResponse, `"instance_id"`) == 0, "legacy instance count")
	check(!strings.Contains(eventsAResponse, `"api_key":"B"`), "events A alias isolation")
	check(strings.Contains(eventsBResponse, `"api_key":"B"`), "events B alias")
	check(strings.Contains(localRankingAResponse, `"instanceId":"`+a.Instance.ID+`"`), "ranking A instance")
	check(!strings.Contains(localRankingAResponse, `"instanceId":"`+b.Instance.ID+`"`), "ranking A excludes B")
	check(strings.Contains(localRankingBResponse, `"instanceId":"`+b.Instance.ID+`"`), "ranking B instance")
	check(!strings.Contains(localRankingBResponse, `"instanceId":"`+a.Instance.ID+`"`), "ranking B excludes A")
	check(strings.Count(localRankingAllResponse, `"instanceId"`) == 2, "ranking all instance count")
	if len(failures) > 0 {
		t.Fatalf("manual QA assertions failed (%s): %s", strings.Join(failures, ", "), raw)
	}
}
