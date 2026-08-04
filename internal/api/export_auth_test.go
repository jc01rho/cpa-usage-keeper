package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

// exportAuthRouter builds the production export-auth router path (/api/v1/export/identity)
// with an injectable clock so the rate limiter can be driven deterministically.
func exportAuthRouter(provider CPAInstanceProvider, limiter *ingestRateLimiter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	registerExportAuthRoutesWithLimiter(group, provider, limiter)
	return router
}

func exportAuthTestProvider(scopes ...string) usageAPIAuthProvider {
	return usageAPIAuthProvider{AuthenticatedIngestCredential: service.AuthenticatedIngestCredential{
		Instance:   entities.CPAInstance{ID: usageAPIInstance, DisplayName: "test", Enabled: true},
		Credential: entities.CPAInstanceCredential{ID: "0198aa10-4d88-7a20-8f4e-8c8de4a9cb12"},
		Scopes:     scopes,
	}}
}

func TestExportAuthRateLimiterStable429EnvelopeAfterBurst(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	limiter := newIngestRateLimiterWithOptions(ingestRateLimiterOptions{now: func() time.Time { return now }})
	router := exportAuthRouter(exportAuthTestProvider(service.ScopeIdentityTest), limiter)
	headers := map[string]string{"Authorization": "Bearer secret-token"}

	for i := 0; i < int(ingestBurst); i++ {
		resp := usageAPIRequest(router, http.MethodGet, "/api/v1/export/identity", nil, headers)
		if resp.Code != http.StatusOK {
			t.Fatalf("burst request %d: status=%d body=%s", i+1, resp.Code, resp.Body.String())
		}
	}

	resp := usageAPIRequest(router, http.MethodGet, "/api/v1/export/identity", nil, headers)
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst, got %d body=%s", resp.Code, resp.Body.String())
	}
	retry := resp.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("expected Retry-After header on 429")
	}
	if seconds, err := strconv.Atoi(retry); err != nil || seconds < 1 {
		t.Fatalf("Retry-After must be a positive integer, got %q", retry)
	}

	var envelope struct {
		ProtocolVersion string `json:"protocolVersion"`
		Error           struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope decode: %v body=%s", err, resp.Body.String())
	}
	if envelope.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("protocolVersion=%q want %q", envelope.ProtocolVersion, protocol.ProtocolVersion)
	}
	if envelope.Error.Code != "rate_limited" || envelope.Error.Message == "" || !envelope.Error.Retryable {
		t.Fatalf("unexpected rate-limit envelope: %+v", envelope.Error)
	}
}

func TestIngestRateLimiterBoundsTrackedCredentialEntries(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	limiter := newIngestRateLimiterWithOptions(ingestRateLimiterOptions{maxEntries: 2, now: func() time.Time { return now }})

	if ok, _ := limiter.allow("cred-old"); !ok {
		t.Fatal("oldest credential should be allowed")
	}
	now = now.Add(time.Second)
	if ok, _ := limiter.allow("cred-newer"); !ok {
		t.Fatal("newer credential should be allowed")
	}
	now = now.Add(time.Second)
	if ok, _ := limiter.allow("cred-newest"); !ok {
		t.Fatal("newest credential should be allowed")
	}
	if got := len(limiter.entries); got > 2 {
		t.Fatalf("entries not bounded: got %d, want <= 2", got)
	}
	if _, tracked := limiter.entries["cred-old"]; tracked {
		t.Fatal("expected the oldest, least-recently-used entry to be evicted at capacity")
	}

	now = now.Add(time.Second)
	if ok, _ := limiter.allow("cred-old"); !ok {
		t.Fatal("evicted credential should receive a fresh burst")
	}
	if got := len(limiter.entries); got > 2 {
		t.Fatalf("entries not bounded after re-add: got %d, want <= 2", got)
	}
}

func TestIngestRateLimiterActiveCredentialRefillsOnePerSecondBurstCap(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	limiter := newIngestRateLimiterWithOptions(ingestRateLimiterOptions{now: func() time.Time { return now }})

	for i := 0; i < int(ingestBurst); i++ {
		if ok, _ := limiter.allow("active"); !ok {
			t.Fatalf("burst request %d should be allowed", i+1)
		}
	}
	if ok, _ := limiter.allow("active"); ok {
		t.Fatal("expected burst to be exhausted")
	}

	now = now.Add(time.Second) // 60/min => 1 token per second
	if ok, _ := limiter.allow("active"); !ok {
		t.Fatal("expected exactly one token to refill after one second")
	}
	if ok, _ := limiter.allow("active"); ok {
		t.Fatal("expected only one refilled token to be available")
	}

	now = now.Add(10 * time.Minute) // long idle refills but caps at burst
	for i := 0; i < int(ingestBurst); i++ {
		if ok, _ := limiter.allow("active"); !ok {
			t.Fatalf("after idle refill request %d should be allowed", i+1)
		}
	}
	if ok, _ := limiter.allow("active"); ok {
		t.Fatal("tokens must not exceed burst after long idle")
	}
}
