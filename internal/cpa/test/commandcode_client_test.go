package cpa_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

// CommandCode is a fork-only provider; CLIProxyAPIPlus serves it only on the legacy /v0 route.
func TestFetchCommandCodeAPIKeysUsesLegacyRouteAndPayloads(t *testing.T) {
	for name, body := range map[string]string{
		"direct":  `[{"api-key":"commandcode-key","prefix":"commandcode-prefix","base-url":"https://commandcode.example/v1","name":"CommandCode","auth_index":"commandcode-auth"}]`,
		"wrapped": `{"commandcode-api-key":[{"api-key":"commandcode-key","prefix":"commandcode-prefix","base-url":"https://commandcode.example/v1","name":"CommandCode","auth_index":"commandcode-auth"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v0/management/commandcode-api-key" {
					t.Errorf("path = %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			result, err := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false).FetchCommandCodeAPIKeys(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Payload) != 1 || result.Payload[0].APIKey != "commandcode-key" || result.Payload[0].AuthIndex != "commandcode-auth" || result.Payload[0].BaseURL != "https://commandcode.example/v1" {
				t.Fatalf("payload = %+v", result.Payload)
			}
		})
	}
}
