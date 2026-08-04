package api

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type metadataAPIProvider struct {
	instance string
	category protocol.MetadataCategory
	body     []byte
	result   protocol.MetadataApplyResponse
	perr     *protocol.Error
}

func (p *metadataAPIProvider) IngestMetadataSnapshot(_ context.Context, instance string, category protocol.MetadataCategory, snapshot *protocol.MetadataSnapshot, body []byte) (*protocol.MetadataApplyResponse, *protocol.Error) {
	p.instance, p.category, p.body = instance, category, append([]byte(nil), body...)
	if p.perr != nil {
		return nil, p.perr
	}
	result := p.result
	if result.Category == "" {
		result = protocol.MetadataApplyResponse{Category: category, Revision: snapshot.Revision, Applied: true, ItemCount: int64(len(snapshot.APIKeys)), ServerTime: "2026-08-03T12:36:01.000Z"}
	}
	return &result, nil
}

func TestMetadataExportHTTPStrictCompleteSnapshotAndTrustedInstance(t *testing.T) {
	provider := &metadataAPIProvider{}
	router := metadataAPIRouter(provider, service.ScopeMetadataPush)
	body := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"fingerprint":"akf1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","displayKey":"sk-...cdef","alias":"team"}]}`)
	response := usageAPIRequest(router, http.MethodPut, "/api/v1/export/metadata/api_keys", body, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
	if response.Code != 200 || provider.instance != usageAPIInstance || provider.category != protocol.CategoryAPIKeys || !bytes.Equal(provider.body, body) {
		t.Fatalf("status=%d body=%s provider=%+v", response.Code, response.Body.String(), provider)
	}
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"incomplete", strings.Replace(string(body), `"complete":true`, `"complete":false`, 1), "incomplete_snapshot", 422},
		{"secret field", strings.Replace(string(body), `"displayKey":`, `"apiKey":"secret","displayKey":`, 1), "unknown_field", 400},
		{"unmasked display key", strings.Replace(string(body), `"sk-...cdef"`, `"sk-raw-secret"`, 1), "invalid_field", 400},
		{"body instance", strings.Replace(string(body), `"revision":`, `"instanceId":"`+usageAPIInstance+`","revision":`, 1), "body_instance_forbidden", 400},
		{"malformed", `{`, "invalid_json", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := usageAPIRequest(router, http.MethodPut, "/api/v1/export/metadata/api_keys", []byte(tc.body), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
			if got.Code != tc.status || !strings.Contains(got.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
			}
		})
	}
}

func TestMetadataExportHTTPValidatesAllCategoriesAndCredentialLifecycle(t *testing.T) {
	tests := []struct {
		path     string
		category protocol.MetadataCategory
		body     string
	}{
		{path: "/api/v1/export/metadata/auth_files", category: protocol.CategoryAuthFiles, body: `{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"authIndex":"same-auth","name":"a.json","displayName":"same","type":"codex","provider":"codex","prefix":"","priority":null,"disabled":false,"note":null,"accountId":null,"projectId":null,"xaiUserId":null,"activeStart":null,"activeUntil":null,"planType":null}]}`},
		{path: "/api/v1/export/metadata/api_keys", category: protocol.CategoryAPIKeys, body: `{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"fingerprint":"akf1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","displayKey":"sk-...cdef","alias":"same"}]}`},
		{path: "/api/v1/export/metadata/provider_identities", category: protocol.CategoryProviderIdentities, body: `{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[{"authIndex":"same-auth","providerType":"openai-compatibility","displayName":"same","prefix":"","baseUrl":"https://example.com/v1","priority":null,"disabled":false,"note":null,"apiKeyFingerprint":null}]}`},
	}
	for _, tc := range tests {
		t.Run(string(tc.category), func(t *testing.T) {
			provider := &metadataAPIProvider{}
			response := usageAPIRequest(metadataAPIRouter(provider, service.ScopeMetadataPush), http.MethodPut, tc.path, []byte(tc.body), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
			if response.Code != http.StatusOK || provider.category != tc.category || provider.instance != usageAPIInstance {
				t.Fatalf("status=%d body=%s provider=%+v", response.Code, response.Body.String(), provider)
			}
		})
	}

	for _, tc := range []struct {
		name string
		err  error
		code string
		want int
	}{
		{name: "revoked", err: service.ErrInvalidCredential, code: "invalid_credential", want: http.StatusUnauthorized},
		{name: "disabled", err: service.ErrInstanceDisabled, code: "instance_disabled", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := usageAPIAuthProvider{authErr: tc.err}
			router := gin.New()
			group := router.Group("/api/v1")
			registerMetadataExportRoutes(group, &metadataAPIProvider{}, auth)
			response := usageAPIRequest(router, http.MethodPut, "/api/v1/export/metadata/api_keys", []byte(tests[1].body), map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
			if response.Code != tc.want || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMetadataExportHTTPRejectsRequestInstanceSelectorsBeforeMutation(t *testing.T) {
	body := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[]}`)
	for _, tc := range []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{name: "snake query", path: "/api/v1/export/metadata/api_keys?instance_id=" + usageAPIInstance},
		{name: "camel query", path: "/api/v1/export/metadata/api_keys?instanceId=" + usageAPIInstance},
		{name: "path selector", path: "/api/v1/export/metadata/api_keys/" + usageAPIInstance},
		{name: "snake header", path: "/api/v1/export/metadata/api_keys", headers: map[string]string{"Instance_Id": usageAPIInstance}},
		{name: "camel header", path: "/api/v1/export/metadata/api_keys", headers: map[string]string{"Instanceid": usageAPIInstance}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &metadataAPIProvider{}
			auth := &metadataCountingAuthProvider{usageAPIAuthProvider: usageAPIAuthProvider{AuthenticatedIngestCredential: service.AuthenticatedIngestCredential{Instance: entities.CPAInstance{ID: usageAPIInstance, DisplayName: "test", Enabled: true}, Credential: entities.CPAInstanceCredential{ID: "0198aa10-4d88-7a20-8f4e-8c8de4a9cb12"}, Scopes: []string{service.ScopeMetadataPush}}}}
			router := gin.New()
			group := router.Group("/api/v1")
			registerMetadataExportRoutes(group, provider, auth)
			headers := map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"}
			for key, value := range tc.headers {
				headers[key] = value
			}
			got := usageAPIRequest(router, http.MethodPut, tc.path, body, headers)
			if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), `"code":"body_instance_forbidden"`) {
				t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
			}
			if auth.calls != 0 || provider.instance != "" || len(provider.body) != 0 {
				t.Fatalf("request mutated state before rejection: auth_calls=%d provider=%+v", auth.calls, provider)
			}
		})
	}
}

func TestMetadataExportHTTPRejectsBodySelectorsBeforeInvalidCredentialAndPreservesBody(t *testing.T) {
	validBody := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[]}`)
	selectors := []struct {
		name string
		body []byte
	}{
		{name: "top level camel", body: bytes.Replace(validBody, []byte(`"revision":`), []byte(`"instanceId":"`+usageAPIInstance+`","revision":`), 1)},
		{name: "nested snake", body: bytes.Replace(validBody, []byte(`"items":[]`), []byte(`"items":[{"nested":{"instance_id":"`+usageAPIInstance+`"}}]`), 1)},
		{name: "normalized separator and case", body: bytes.Replace(validBody, []byte(`"items":[]`), []byte(`"items":[{"nested":{"InStAnCe-Id":"`+usageAPIInstance+`"}}]`), 1)},
	}
	for _, tc := range selectors {
		t.Run(tc.name, func(t *testing.T) {
			provider := &metadataAPIProvider{}
			auth := &metadataCountingAuthProvider{usageAPIAuthProvider: usageAPIAuthProvider{authErr: service.ErrInvalidCredential}}
			router := gin.New()
			registerMetadataExportRoutes(router.Group("/api/v1"), provider, auth)
			got := usageAPIRequest(router, http.MethodPut, "/api/v1/export/metadata/api_keys", tc.body, map[string]string{"Authorization": "Bearer invalid", "Content-Type": "application/json"})
			if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), `"code":"body_instance_forbidden"`) {
				t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
			}
			if auth.calls != 0 || provider.instance != "" || len(provider.body) != 0 {
				t.Fatalf("request reached auth or mutation: auth_calls=%d provider=%+v", auth.calls, provider)
			}
		})
	}

	provider := &metadataAPIProvider{}
	got := usageAPIRequest(metadataAPIRouter(provider, service.ScopeMetadataPush), http.MethodPut, "/api/v1/export/metadata/api_keys", validBody, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
	if got.Code != http.StatusOK || !bytes.Equal(provider.body, validBody) {
		t.Fatalf("preserved body status=%d got=%q want=%q", got.Code, provider.body, validBody)
	}
}

func TestMetadataExportHTTPRejectsHyphenatedHeaderBeforeInvalidCredential(t *testing.T) {
	provider := &metadataAPIProvider{}
	auth := &metadataCountingAuthProvider{usageAPIAuthProvider: usageAPIAuthProvider{authErr: service.ErrInvalidCredential}}
	router := gin.New()
	registerMetadataExportRoutes(router.Group("/api/v1"), provider, auth)
	body := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[]}`)
	got := usageAPIRequest(router, http.MethodPut, "/api/v1/export/metadata/api_keys", body, map[string]string{"Authorization": "Bearer invalid", "Content-Type": "application/json", "Instance-Id": usageAPIInstance})
	if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), `"code":"body_instance_forbidden"`) || auth.calls != 0 || provider.instance != "" {
		t.Fatalf("status=%d body=%s auth_calls=%d provider=%+v", got.Code, got.Body.String(), auth.calls, provider)
	}
}

func TestMetadataExportHTTPRevisionErrorsAndScope(t *testing.T) {
	body := []byte(`{"protocolVersion":"keeper-export/v1","revision":1,"complete":true,"generatedAt":"2026-08-03T12:36:00.000Z","items":[]}`)
	provider := &metadataAPIProvider{perr: protocol.ErrorForCode("conflicting_revision")}
	got := usageAPIRequest(metadataAPIRouter(provider, service.ScopeMetadataPush), http.MethodPut, "/api/v1/export/metadata/auth_files", body, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
	if got.Code != 409 || !strings.Contains(got.Body.String(), "conflicting_revision") {
		t.Fatalf("conflict=%d %s", got.Code, got.Body.String())
	}
	got = usageAPIRequest(metadataAPIRouter(&metadataAPIProvider{}, service.ScopeUsagePush), http.MethodPut, "/api/v1/export/metadata/auth_files", body, map[string]string{"Authorization": "Bearer x", "Content-Type": "application/json"})
	if got.Code != 403 || !strings.Contains(got.Body.String(), "insufficient_scope") {
		t.Fatalf("scope=%d %s", got.Code, got.Body.String())
	}
}

func metadataAPIRouter(provider service.MetadataExportProvider, scopes ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	auth := &metadataCountingAuthProvider{usageAPIAuthProvider: usageAPIAuthProvider{AuthenticatedIngestCredential: service.AuthenticatedIngestCredential{Instance: entities.CPAInstance{ID: usageAPIInstance, DisplayName: "test", Enabled: true}, Credential: entities.CPAInstanceCredential{ID: "0198aa10-4d88-7a20-8f4e-8c8de4a9cb12"}, Scopes: scopes}}}
	router := gin.New()
	group := router.Group("/api/v1")
	registerMetadataExportRoutes(group, provider, auth)
	return router
}

type metadataCountingAuthProvider struct {
	usageAPIAuthProvider
	calls int
}

func (p *metadataCountingAuthProvider) Authenticate(ctx context.Context, token string) (service.AuthenticatedIngestCredential, error) {
	p.calls++
	return p.usageAPIAuthProvider.Authenticate(ctx, token)
}
