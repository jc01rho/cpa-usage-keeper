package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	usageAPIInstance = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	usageAPIStream   = "0198aa11-1055-7f12-8a00-e843d1e17522"
)

type usageAPIProvider struct {
	mu       sync.Mutex
	instance string
	batch    *protocol.UsageBatch
	ack      protocol.UsageAck
	perr     *protocol.Error
}

func (p *usageAPIProvider) IngestUsageBatch(_ context.Context, instanceID string, batch *protocol.UsageBatch) (*protocol.UsageAck, *protocol.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.instance = instanceID
	p.batch = batch
	if p.perr != nil {
		return nil, p.perr
	}
	ack := p.ack
	if ack.StreamID == "" {
		ack = protocol.UsageAck{StreamID: batch.StreamID, AcknowledgedThrough: 1, NextExpectedSequence: 2, AcceptedCount: 1}
	}
	return &ack, nil
}

type usageAPIAuthProvider struct {
	service.AuthenticatedIngestCredential
	authErr error
}

func (p usageAPIAuthProvider) Create(context.Context, service.CreateInstanceInput) (service.IssuedInstanceCredential, error) {
	panic("unused")
}
func (p usageAPIAuthProvider) List(context.Context) ([]entities.CPAInstance, error) { panic("unused") }
func (p usageAPIAuthProvider) Get(context.Context, string) (entities.CPAInstance, error) {
	panic("unused")
}
func (p usageAPIAuthProvider) Update(context.Context, string, *string, *bool) (entities.CPAInstance, error) {
	panic("unused")
}
func (p usageAPIAuthProvider) Issue(context.Context, string, service.CredentialInput) (service.IssuedInstanceCredential, error) {
	panic("unused")
}
func (p usageAPIAuthProvider) ListCredentials(context.Context, string) ([]entities.CPAInstanceCredential, error) {
	panic("unused")
}
func (p usageAPIAuthProvider) Rotate(context.Context, string, string, service.CredentialInput) (service.IssuedInstanceCredential, error) {
	panic("unused")
}
func (p usageAPIAuthProvider) Revoke(context.Context, string, string) error { panic("unused") }
func (p usageAPIAuthProvider) Authenticate(context.Context, string) (service.AuthenticatedIngestCredential, error) {
	return p.AuthenticatedIngestCredential, p.authErr
}

func TestUsageExportHTTPStrictBoundaryAndTrustedInstance(t *testing.T) {
	provider := &usageAPIProvider{}
	router := usageAPIRouter(provider, service.ScopeUsagePush)
	body := usageAPIValidBody(1)

	response := usageAPIRequest(router, http.MethodPost, "/api/v1/export/usage", body, map[string]string{"Content-Type": "application/json", "Authorization": "Bearer secret-token"})
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if provider.instance != usageAPIInstance || provider.batch == nil || provider.batch.Events[0].Sequence != 1 {
		t.Fatalf("trusted ingest = instance %q batch %+v", provider.instance, provider.batch)
	}
	expectedDigestBytes := []byte(`{"timestamp":"2026-08-03T12:34:56.789Z","latency_ms":123,"ttft_ms":45,"source":"openai","auth_index":"auth","client_ip":null,"x_forwarded_for":null,"user_agent":null,"tokens":{"input_tokens":10,"output_tokens":4,"reasoning_tokens":1,"cached_tokens":0,"cache_read_tokens":0,"cache_read_tokens_present":true,"cache_creation_tokens":0,"total_tokens":15},"failed":false,"generate":true,"fail":{"status_code":200,"code":null},"accounting_version":1,"token_breakdown":{"input":10,"cached":0,"cache_read":0,"cache_creation":0,"reasoning":1,"output":4},"provider":"openai","executor_type":"CodexExecutor","model":"gpt-5.6","alias":"gpt-5.6","endpoint":"/v1/responses","auth_type":"apikey","api_key_fingerprint":null,"request_id":"same-request-id","reasoning_effort":"medium","service_tier":"default","response_service_tier":null,"response_headers":null}`)
	if !bytes.Equal(provider.batch.Events[0].RawPayload, expectedDigestBytes) {
		t.Fatalf("raw payload changed: %s", provider.batch.Events[0].RawPayload)
	}

	cases := []struct {
		name    string
		body    []byte
		headers map[string]string
		status  int
		code    string
	}{
		{"missing content type", body, map[string]string{"Authorization": "Bearer x"}, 400, "invalid_field"},
		{"encoding", body, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json", "Content-Encoding": "identity"}, 400, "invalid_field"},
		{"invalid utf8", []byte{0xff}, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"}, 400, "invalid_json"},
		{"duplicate key", bytes.Replace(body, []byte(`"streamId":`), []byte(`"streamId":"`+usageAPIStream+`","streamId":`), 1), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"}, 400, "invalid_json"},
		{"body instance", bytes.Replace(body, []byte(`"streamId":`), []byte(`"instanceId":"`+usageAPIInstance+`","streamId":`), 1), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"}, 400, "body_instance_forbidden"},
		{"secret api key", bytes.Replace(body, []byte(`"request_id":`), []byte(`"api_key":"secret","request_id":`), 1), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"}, 400, "unknown_field"},
		{"too large", bytes.Repeat([]byte("x"), protocol.MaxBodyBytes+1), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"}, 413, "request_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := usageAPIRequest(router, http.MethodPost, "/api/v1/export/usage", tc.body, tc.headers)
			if got.Code != tc.status || !strings.Contains(got.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
			}
		})
	}
}

func TestUsageExportHTTPWrongScopeAndLimits(t *testing.T) {
	body := usageAPIValidBody(1)
	wrongScope := usageAPIRequest(usageAPIRouter(&usageAPIProvider{}, service.ScopeIdentityTest), http.MethodPost, "/api/v1/export/usage", body, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
	if wrongScope.Code != 403 || !strings.Contains(wrongScope.Body.String(), "insufficient_scope") {
		t.Fatalf("wrong scope: %d %s", wrongScope.Code, wrongScope.Body.String())
	}
	tooMany := usageAPIRequest(usageAPIRouter(&usageAPIProvider{}, service.ScopeUsagePush), http.MethodPost, "/api/v1/export/usage", usageAPIValidBody(501), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
	if tooMany.Code != 422 || !strings.Contains(tooMany.Body.String(), "batch_limit_exceeded") {
		t.Fatalf("batch limit: %d %s", tooMany.Code, tooMany.Body.String())
	}
}

func usageAPIRouter(provider service.UsageExportProvider, scopes ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	auth := usageAPIAuthProvider{AuthenticatedIngestCredential: service.AuthenticatedIngestCredential{
		Instance:   entities.CPAInstance{ID: usageAPIInstance, DisplayName: "test", Enabled: true},
		Credential: entities.CPAInstanceCredential{ID: "0198aa10-4d88-7a20-8f4e-8c8de4a9cb12"},
		Scopes:     scopes,
	}}
	router := gin.New()
	group := router.Group("/api/v1")
	registerUsageExportRoutes(group, provider, auth)
	return router
}

func usageAPIRequest(router http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func usageAPIValidBody(count int) []byte {
	payload := json.RawMessage(`{"timestamp":"2026-08-03T12:34:56.789Z","latency_ms":123,"ttft_ms":45,"source":"openai","auth_index":"auth","client_ip":null,"x_forwarded_for":null,"user_agent":null,"tokens":{"input_tokens":10,"output_tokens":4,"reasoning_tokens":1,"cached_tokens":0,"cache_read_tokens":0,"cache_read_tokens_present":true,"cache_creation_tokens":0,"total_tokens":15},"failed":false,"generate":true,"fail":{"status_code":200,"code":null},"accounting_version":1,"token_breakdown":{"input":10,"cached":0,"cache_read":0,"cache_creation":0,"reasoning":1,"output":4},"provider":"openai","executor_type":"CodexExecutor","model":"gpt-5.6","alias":"gpt-5.6","endpoint":"/v1/responses","auth_type":"apikey","api_key_fingerprint":null,"request_id":"same-request-id","reasoning_effort":"medium","service_tier":"default","response_service_tier":null,"response_headers":null}`)
	events := make([]map[string]any, count)
	for i := range events {
		events[i] = map[string]any{"sequence": i + 1, "payload": payload}
	}
	body, _ := json.Marshal(map[string]any{"protocolVersion": protocol.ProtocolVersion, "streamId": usageAPIStream, "events": events})
	return body
}
