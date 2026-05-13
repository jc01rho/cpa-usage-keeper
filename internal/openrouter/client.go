package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const defaultModelsEndpoint = "https://openrouter.ai/api/v1/models"

type ModelPricing struct {
	Prompt     string
	Completion string
	CacheRead  string
}

type ModelInfo struct {
	ID      string
	Name    string
	Pricing ModelPricing
}

type Client struct {
	apiKey       string
	httpClient   *http.Client
	modelsEndURL string
}

func NewClient(apiKey string, timeout time.Duration) *Client {
	return newClient(apiKey, timeout, defaultModelsEndpoint)
}

func NewClientWithURL(apiKey string, timeout time.Duration, endpointURL string) *Client {
	return newClient(apiKey, timeout, endpointURL)
}

func newClient(apiKey string, timeout time.Duration, endpointURL string) *Client {
	return &Client{
		apiKey:       strings.TrimSpace(apiKey),
		httpClient:   &http.Client{Timeout: timeout},
		modelsEndURL: endpointURL,
	}
}

func (c *Client) FetchModels(ctx context.Context) ([]ModelInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("openrouter client is nil")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.modelsEndURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build OpenRouter models request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	logrus.WithField("url", c.modelsEndURL).Debug("fetching OpenRouter models")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request OpenRouter models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read OpenRouter models response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("OpenRouter models request returned status %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Pricing struct {
				Prompt         string `json:"prompt"`
				Completion     string `json:"completion"`
				InputCacheRead string `json:"input_cache_read"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse OpenRouter models response: %w", err)
	}

	models := make([]ModelInfo, 0, len(payload.Data))
	for _, m := range payload.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		models = append(models, ModelInfo{
			ID:   id,
			Name: m.Name,
			Pricing: ModelPricing{
				Prompt:     m.Pricing.Prompt,
				Completion: m.Pricing.Completion,
				CacheRead:  m.Pricing.InputCacheRead,
			},
		})
	}
	logrus.WithField("count", len(models)).Debug("fetched OpenRouter models")
	return models, nil
}
