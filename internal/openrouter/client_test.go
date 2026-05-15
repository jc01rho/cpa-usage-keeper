package openrouter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/openrouter"
)

func TestClientFetchesModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":   "anthropic/claude-sonnet-4",
					"name": "Anthropic: Claude Sonnet 4",
				"pricing": map[string]string{
					"prompt":           "0.000003",
					"completion":       "0.000015",
					"input_cache_read": "0.0000003",
				},				},
				{
					"id":   " ",
					"name": "Blank ID",
					"pricing": map[string]string{
						"prompt": "1.0",
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := openrouter.NewClientWithURL("", time.Second, srv.URL)
	models, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model (blank filtered), got %d", len(models))
	}
	if models[0].ID != "anthropic/claude-sonnet-4" {
		t.Fatalf("unexpected model id: %q", models[0].ID)
	}
	if models[0].Pricing.Prompt != "0.000003" {
		t.Fatalf("unexpected prompt price: %q", models[0].Pricing.Prompt)
	}
	if models[0].Pricing.Completion != "0.000015" {
		t.Fatalf("unexpected completion price: %q", models[0].Pricing.Completion)
	}
	if models[0].Pricing.CacheRead != "0.0000003" {
		t.Fatalf("unexpected cache price: %q", models[0].Pricing.CacheRead)
	}
}

func TestClientSendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	client := openrouter.NewClientWithURL("test-key", time.Second, srv.URL)
	_, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("expected Authorization header 'Bearer test-key', got %q", gotAuth)
	}
}

func TestClientOmitsAuthHeaderWhenNoKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	client := openrouter.NewClientWithURL("", time.Second, srv.URL)
	_, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestClientReturnsErrorOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := openrouter.NewClientWithURL("", time.Second, srv.URL)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected error for non-OK status, got nil")
	}
}
