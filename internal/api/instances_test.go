package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

type issuedResponse struct {
	ProtocolVersion string `json:"protocolVersion"`
	Instance        struct {
		InstanceID string `json:"instanceId"`
	} `json:"instance"`
	Credential struct {
		CredentialID, Token string
		Scopes              []string `json:"scopes"`
	} `json:"credential"`
}

func TestInstanceCredentialLifecycleAndAuthentication(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "keeper.db")
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	service1 := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(db))
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAInstances: service1})

	created := issueInstance(t, router, `{"displayName":"CPA east","credential":{"name":"primary","scopes":["usage:push","metadata:push","identity:test"]}}`)
	if len(created.Credential.Token) < 80 {
		t.Fatalf("token too short: %d", len(created.Credential.Token))
	}

	list := perform(t, router, http.MethodGet, "/api/v1/instances", "", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), created.Credential.Token) || strings.Contains(list.Body.String(), `"token"`) {
		t.Fatalf("list leaked token: %s", list.Body.String())
	}
	credentials := perform(t, router, http.MethodGet, fmt.Sprintf("/api/v1/instances/%s/credentials", created.Instance.InstanceID), "", nil)
	if strings.Contains(credentials.Body.String(), created.Credential.Token) || strings.Contains(credentials.Body.String(), "tokenHash") || strings.Contains(credentials.Body.String(), `"token"`) {
		t.Fatalf("credential list leaked token material: %s", credentials.Body.String())
	}

	identity := identityRequest(t, router, created.Credential.Token)
	if identity.Code != http.StatusOK || !strings.Contains(identity.Body.String(), created.Instance.InstanceID) {
		t.Fatalf("identity status=%d body=%s", identity.Code, identity.Body.String())
	}

	wrongScope := issueCredential(t, router, created.Instance.InstanceID, `{"name":"usage only","scopes":["usage:push"],"expiresAt":null}`)
	assertProtocolError(t, identityRequest(t, router, wrongScope.Credential.Token), http.StatusForbidden, "insufficient_scope")

	rotated := rotateCredential(t, router, created.Instance.InstanceID, created.Credential.CredentialID, `{"name":"replacement","scopes":["identity:test"],"expiresAt":null}`)
	assertProtocolError(t, identityRequest(t, router, created.Credential.Token), http.StatusUnauthorized, "invalid_credential")
	if got := identityRequest(t, router, rotated.Credential.Token); got.Code != http.StatusOK {
		t.Fatalf("replacement identity status=%d body=%s", got.Code, got.Body.String())
	}

	revoke := perform(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/instances/%s/credentials/%s", created.Instance.InstanceID, rotated.Credential.CredentialID), "", intentHeaders())
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	assertProtocolError(t, identityRequest(t, router, rotated.Credential.Token), http.StatusUnauthorized, "invalid_credential")

	active := issueCredential(t, router, created.Instance.InstanceID, `{"name":"disable test","scopes":["identity:test"],"expiresAt":null}`)
	disable := perform(t, router, http.MethodPatch, "/api/v1/instances/"+created.Instance.InstanceID, `{"enabled":false}`, jsonHeaders(true))
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	assertProtocolError(t, identityRequest(t, router, active.Credential.Token), http.StatusForbidden, "instance_disabled")

	var rows []entities.CPAInstanceCredential
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.TokenHash == "" || strings.Contains(row.TokenHash, created.Credential.Token) || !strings.HasPrefix(row.TokenHash, "$argon2id$") {
			t.Fatalf("bad persisted verifier: %q", row.TokenHash)
		}
	}
	var rawFound int64
	if err := db.Raw(`SELECT COUNT(*) FROM cpa_instance_credentials WHERE CAST(token_hash AS TEXT) LIKE ?`, "%"+created.Credential.Token+"%").Scan(&rawFound).Error; err != nil {
		t.Fatal(err)
	}
	if rawFound != 0 {
		t.Fatal("raw token persisted")
	}

	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	restartedDB, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	restartedService := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(restartedDB))
	restartedRouter := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAInstances: restartedService})
	assertProtocolError(t, identityRequest(t, restartedRouter, active.Credential.Token), http.StatusForbidden, "instance_disabled")
}

func TestInstanceCredentialExpiryMalformedBodiesAndAdminConventions(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "keeper.db")})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(db))
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAInstances: svc})

	noIntent := perform(t, router, http.MethodPost, "/api/v1/instances", `{}`, jsonHeaders(false))
	if noIntent.Code != http.StatusForbidden {
		t.Fatalf("request intent status=%d", noIntent.Code)
	}
	assertProtocolError(t, perform(t, router, http.MethodPost, "/api/v1/instances", `{"displayName":"a","displayName":"b","credential":{"name":"x","scopes":["identity:test"]}}`, jsonHeaders(true)), http.StatusBadRequest, "invalid_json")
	assertProtocolError(t, perform(t, router, http.MethodPost, "/api/v1/instances", `{"displayName":" padded ","credential":{"name":"x","scopes":["identity:test"]}}`, jsonHeaders(true)), http.StatusBadRequest, "invalid_field")
	assertProtocolError(t, perform(t, router, http.MethodPost, "/api/v1/instances", `{"displayName":"a","credential":{"name":"x","scopes":["identity:test"]},"extra":1}`, jsonHeaders(true)), http.StatusBadRequest, "unknown_field")
	oversized := `{"displayName":"` + strings.Repeat("x", 1048577) + `","credential":{"name":"x","scopes":["identity:test"]}}`
	assertProtocolError(t, perform(t, router, http.MethodPost, "/api/v1/instances", oversized, jsonHeaders(true)), http.StatusRequestEntityTooLarge, "request_too_large")

	created := issueInstance(t, router, `{"displayName":"expiry","credential":{"name":"primary","scopes":["identity:test"]}}`)
	expired := time.Now().UTC().Add(-time.Minute)
	if err := db.Model(&entities.CPAInstanceCredential{}).Where("id = ?", created.Credential.CredentialID).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	assertProtocolError(t, identityRequest(t, router, created.Credential.Token), http.StatusUnauthorized, "invalid_credential")
}

func TestParallelRotateAndRevokePermitOnlyOneTerminalMutation(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "keeper.db")})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(db))
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAInstances: svc})
	created := issueInstance(t, router, `{"displayName":"race","credential":{"name":"primary","scopes":["identity:test"]}}`)

	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		result := perform(t, router, http.MethodPost, fmt.Sprintf("/api/v1/instances/%s/credentials/%s/rotate", created.Instance.InstanceID, created.Credential.CredentialID), `{"name":"replacement","scopes":["identity:test"],"expiresAt":null}`, jsonHeaders(true))
		codes <- result.Code
	}()
	go func() {
		defer wg.Done()
		<-start
		result := perform(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/instances/%s/credentials/%s", created.Instance.InstanceID, created.Credential.CredentialID), "", intentHeaders())
		codes <- result.Code
	}()
	close(start)
	wg.Wait()
	close(codes)
	success, notFound := 0, 0
	for code := range codes {
		if code == http.StatusCreated || code == http.StatusNoContent {
			success++
		}
		if code == http.StatusNotFound {
			notFound++
		}
	}
	if success != 1 || notFound != 1 {
		t.Fatalf("success=%d notFound=%d", success, notFound)
	}
	assertProtocolError(t, identityRequest(t, router, created.Credential.Token), http.StatusUnauthorized, "invalid_credential")
}

func issueInstance(t *testing.T, router http.Handler, body string) issuedResponse {
	t.Helper()
	response := perform(t, router, http.MethodPost, "/api/v1/instances", body, jsonHeaders(true))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded issuedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
func issueCredential(t *testing.T, router http.Handler, instanceID, body string) issuedResponse {
	t.Helper()
	response := perform(t, router, http.MethodPost, "/api/v1/instances/"+instanceID+"/credentials", body, jsonHeaders(true))
	if response.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded issuedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
func rotateCredential(t *testing.T, router http.Handler, instanceID, credentialID, body string) issuedResponse {
	t.Helper()
	response := perform(t, router, http.MethodPost, fmt.Sprintf("/api/v1/instances/%s/credentials/%s/rotate", instanceID, credentialID), body, jsonHeaders(true))
	if response.Code != http.StatusCreated {
		t.Fatalf("rotate status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded issuedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
func identityRequest(t *testing.T, router http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	return perform(t, router, http.MethodGet, "/api/v1/export/identity", "", map[string]string{"Authorization": "Bearer " + token})
}
func perform(t *testing.T, router http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}
func jsonHeaders(intent bool) map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	if intent {
		headers[requestIntentHeaderName] = requestIntentHeaderValueFetch
	}
	return headers
}
func intentHeaders() map[string]string {
	return map[string]string{requestIntentHeaderName: requestIntentHeaderValueFetch}
}
func assertProtocolError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("status=%d body=%s want=%d/%s", response.Code, response.Body.String(), status, code)
	}
}
