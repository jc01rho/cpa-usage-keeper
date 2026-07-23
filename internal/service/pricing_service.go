package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/openrouter"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type PricingProvider interface {
	ListUsedModels(context.Context) ([]string, error)
	ListPricing(context.Context) ([]entities.ModelPriceSetting, error)
	PreviewPricingSync(context.Context) (servicedto.PricingSyncPreview, error)
	UpdatePricing(context.Context, servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error)
	DeletePricing(context.Context, string) error
	FetchFromOpenRouter(ctx context.Context) ([]entities.ModelPriceSetting, []string, error)
}

type ModelsFetcher interface {
	FetchModels(context.Context) (*response.ModelsResult, error)
}

type pricingService struct {
	db            *gorm.DB
	modelsFetcher ModelsFetcher
	openRouter    *openrouter.Client
}

func NewPricingService(db *gorm.DB, modelsFetcher ...ModelsFetcher) PricingProvider {
	service := &pricingService{db: db}
	if len(modelsFetcher) > 0 {
		service.modelsFetcher = modelsFetcher[0]
	}
	return service
}

func NewPricingServiceWithOpenRouter(db *gorm.DB, modelsFetcher ModelsFetcher, orClient *openrouter.Client) PricingProvider {
	return &pricingService{
		db:            db,
		modelsFetcher: modelsFetcher,
		openRouter:    orClient,
	}
}

func (s *pricingService) ListUsedModels(ctx context.Context) ([]string, error) {
	return s.effectiveModels(ctx)
}

func (s *pricingService) ListPricing(context.Context) ([]entities.ModelPriceSetting, error) {
	return repository.ListModelPriceSettings(s.db)
}

func (s *pricingService) UpdatePricing(ctx context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	pricingStyle := strings.ToLower(strings.TrimSpace(input.PricingStyle))
	if pricingStyle == "" {
		pricingStyle = entities.ModelPricingStyleOpenAI
	}
	if pricingStyle != entities.ModelPricingStyleOpenAI && pricingStyle != entities.ModelPricingStyleClaude {
		return nil, fmt.Errorf("pricing_style must be openai or claude")
	}
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CacheReadPricePer1M < 0 || input.CacheWritePricePer1M < 0 {
		return nil, fmt.Errorf("prices must be non-negative")
	}
	if input.PriceMultiplier != nil {
		multiplier := *input.PriceMultiplier
		if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			return nil, fmt.Errorf("price_multiplier must be non-negative")
		}
	}

	// repository 会把 upsert 的存在性查询固定到 writer，Save 则由写回调自动路由到 writer。
	return repository.UpsertModelPriceSetting(s.db, repodto.ModelPriceSettingInput{
		Model:                modelName,
		PricingStyle:         pricingStyle,
		PromptPricePer1M:     input.PromptPricePer1M,
		CompletionPricePer1M: input.CompletionPricePer1M,
		CacheReadPricePer1M:  input.CacheReadPricePer1M,
		CacheWritePricePer1M: input.CacheWritePricePer1M,
		PriceMultiplier:      input.PriceMultiplier,
	})
}

func (s *pricingService) DeletePricing(_ context.Context, model string) error {
	return repository.DeleteModelPriceSetting(s.db, model)
}

func (s *pricingService) effectiveModels(ctx context.Context) ([]string, error) {
	localModels, err := repository.ListUsedModels(s.db)
	if err != nil {
		return nil, err
	}
	if s.modelsFetcher == nil {
		return localModels, nil
	}

	result, err := s.modelsFetcher.FetchModels(ctx)
	if err != nil {
		logrus.WithError(err).Error("pricing model listing falling back to local usage aggregation")
		return localModels, nil
	}

	logrus.Debug("pricing model listing using CPA models endpoint")
	return mergeModelNames(localModels, extractCPAModelIDs(result)), nil
}

func extractCPAModelIDs(result *response.ModelsResult) []string {
	if result == nil {
		return []string{}
	}
	models := make([]string, 0, len(result.Payload.Data))
	for _, model := range result.Payload.Data {
		models = append(models, model.ID)
	}
	return models
}

func mergeModelNames(modelLists ...[]string) []string {
	total := 0
	for _, list := range modelLists {
		total += len(list)
	}
	seen := make(map[string]struct{}, total)
	models := make([]string, 0, total)
	for _, list := range modelLists {
		for _, model := range list {
			id := strings.TrimSpace(model)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models
}

// FetchFromOpenRouter fetches pricing from OpenRouter, matches against used models,
// upserts pricing entries, and returns updated settings plus unmatched model names.
func (s *pricingService) FetchFromOpenRouter(ctx context.Context) ([]entities.ModelPriceSetting, []string, error) {
	if s.openRouter == nil {
		return nil, nil, fmt.Errorf("OpenRouter client is not configured")
	}

	usedModels, err := repository.ListUsedModels(s.db)
	if err != nil {
		return nil, nil, fmt.Errorf("list used models: %w", err)
	}
	if len(usedModels) == 0 {
		return nil, nil, fmt.Errorf("no models to price")
	}

	orModels, err := s.openRouter.FetchModels(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch OpenRouter models: %w", err)
	}

	matches := matchModelsToOpenRouter(orModels, usedModels)

	var updated []entities.ModelPriceSetting
	var unmatched []string
	for _, model := range usedModels {
		info, ok := matches[model]
		if !ok {
			unmatched = append(unmatched, model)
			continue
		}

		promptPrice, _ := strconv.ParseFloat(info.Pricing.Prompt, 64)
		completionPrice, _ := strconv.ParseFloat(info.Pricing.Completion, 64)
		cachePrice, _ := strconv.ParseFloat(info.Pricing.CacheRead, 64)
		
		// OpenRouter API returns prices per-token, but we store per-1M-token
		// Multiply by 1,000,000 to convert from per-token to per-1M-token
		promptPrice *= 1_000_000
		completionPrice *= 1_000_000
		cachePrice *= 1_000_000

		// Clamp negative prices to 0 (OpenRouter returns negative values for free models)
		if promptPrice < 0 {
			promptPrice = 0
		}
		if completionPrice < 0 {
			completionPrice = 0
		}
		if cachePrice < 0 {
			cachePrice = 0
		}

		setting, err := repository.UpsertModelPriceSetting(s.db, repodto.ModelPriceSettingInput{
			Model:                model,
			PromptPricePer1M:     promptPrice,
			CompletionPricePer1M: completionPrice,
			CacheReadPricePer1M:  cachePrice,
		})
		if err != nil {
			logrus.WithError(err).WithField("model", model).Warn("upsert OpenRouter pricing failed")
			continue
		}
		updated = append(updated, *setting)
	}

	logrus.WithFields(logrus.Fields{
		"matched":   len(updated),
		"unmatched": len(unmatched),
	}).Info("OpenRouter pricing sync completed")

	return updated, unmatched, nil
}

// matchModelsToOpenRouter matches used model names to OpenRouter models using slug and substring matching.
func matchModelsToOpenRouter(orModels []openrouter.ModelInfo, usedModels []string) map[string]openrouter.ModelInfo {
	slugIndex := make(map[string]openrouter.ModelInfo, len(orModels))
	for _, m := range orModels {
		slug := extractModelSlug(m.ID)
		slugIndex[slug] = m
	}

	result := make(map[string]openrouter.ModelInfo, len(usedModels))
	for _, model := range usedModels {
		normalized := strings.ToLower(strings.TrimSpace(model))
		if normalized == "" {
			continue
		}

		// Try exact slug match first.
		if m, ok := slugIndex[normalized]; ok {
			result[model] = m
			continue
		}

		// Try substring match: model name contained in slug or slug contained in model name.
		for slug, m := range slugIndex {
			if strings.Contains(slug, normalized) || strings.Contains(normalized, slug) {
				result[model] = m
				break
			}
		}
	}

	return result
}

// extractModelSlug extracts the model slug after the last '/' in an OpenRouter model ID.
// e.g. "anthropic/claude-sonnet-4" -> "claude-sonnet-4"
func extractModelSlug(openRouterID string) string {
	if idx := strings.LastIndexByte(openRouterID, '/'); idx >= 0 && idx < len(openRouterID)-1 {
		return strings.ToLower(openRouterID[idx+1:])
	}
	return strings.ToLower(openRouterID)
}
