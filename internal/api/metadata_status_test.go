package api

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestMetadataStatusReturnsRedactedDigestAndInstanceLabel(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "status.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() }()
	instanceID := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	now := time.Date(2026, 8, 3, 12, 36, 1, 0, time.UTC)
	instance := entities.CPAInstance{ID: instanceID, DisplayName: "CPA east", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	digest := []byte(strings.Repeat("d", 32))
	if err := db.Create(&entities.CPAMetadataSnapshot{InstanceID: instanceID, Category: "api_keys", Revision: 7, BodyDigest: digest, ItemCount: 2, GeneratedAt: now, AppliedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	instanceService := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(db))
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAInstances: instanceService, MetadataStatus: service.NewMetadataStatusService(db)})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/instances/metadata-status?instance_id="+instanceID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`"instanceId":"` + instanceID + `"`, `"instanceName":"CPA east"`, `"digest":"` + hex.EncodeToString(digest) + `"`, `"lastError":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	for _, forbidden := range []string{"token", "secret", "rawBody", "bodyDigest"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("status leaked forbidden field %q: %s", forbidden, body)
		}
	}
}
