package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/openrouter"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var ErrInvalidPricingInput = errors.New("invalid pricing input")

type PricingProvider interface {
	ListUsedModels(context.Context) ([]string, error)
	ListPricing(context.Context) ([]entities.ModelPriceSetting, error)
	PreviewPricingSync(context.Context) (servicedto.PricingSyncPreview, error)
	UpdatePricing(context.Context, servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error)
	UpdatePricingBatch(context.Context, []servicedto.UpdatePricingInput) ([]entities.ModelPriceSetting, error)
	DeletePricing(context.Context, string) error
	FetchFromOpenRouter(ctx context.Context) ([]entities.ModelPriceSetting, []string, error)
	ListPricingRules(context.Context, string) ([]servicedto.PricingRule, error)
	ReplacePricingRules(context.Context, servicedto.ReplacePricingRulesInput) ([]servicedto.PricingRule, error)
}

type ModelsFetcher interface {
	FetchModels(context.Context) (*response.ModelsResult, error)
}

type pricingService struct {
	db            *gorm.DB
	modelsFetcher ModelsFetcher
	openRouter    *openrouter.Client
	catalog       *pricing.Catalog
	mutationMu    sync.Mutex
}

func NewPricingService(db *gorm.DB, catalog *pricing.Catalog, modelsFetcher ...ModelsFetcher) PricingProvider {
	service := &pricingService{db: db, catalog: requirePricingCatalog(catalog)}
	if len(modelsFetcher) > 0 {
		service.modelsFetcher = modelsFetcher[0]
	}
	return service
}

// NewPricingServiceWithOpenRouter builds a catalog-backed pricing service with optional OpenRouter fetch support.
func NewPricingServiceWithOpenRouter(db *gorm.DB, catalog *pricing.Catalog, modelsFetcher ModelsFetcher, orClient *openrouter.Client) PricingProvider {
	return &pricingService{
		db:            db,
		catalog:       requirePricingCatalog(catalog),
		modelsFetcher: modelsFetcher,
		openRouter:    orClient,
	}
}

func requirePricingCatalog(catalog *pricing.Catalog) *pricing.Catalog {
	if catalog == nil {
		panic("pricing catalog is required")
	}
	return catalog
}

func (s *pricingService) ListUsedModels(ctx context.Context) ([]string, error) {
	return s.effectiveModels(ctx)
}

func (s *pricingService) ListPricing(context.Context) ([]entities.ModelPriceSetting, error) {
	configs := s.catalog.Snapshot().ModelConfigs()
	settings := make([]entities.ModelPriceSetting, len(configs))
	for index := range configs {
		settings[index] = configs[index].Pricing
	}
	return settings, nil
}

func (s *pricingService) UpdatePricing(ctx context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	settings, err := s.UpdatePricingBatch(ctx, []servicedto.UpdatePricingInput{input})
	if err != nil {
		return nil, err
	}
	return &settings[0], nil
}

func (s *pricingService) UpdatePricingBatch(ctx context.Context, inputs []servicedto.UpdatePricingInput) ([]entities.ModelPriceSetting, error) {
	if len(inputs) == 0 {
		return []entities.ModelPriceSetting{}, nil
	}
	normalized := make([]repodto.ModelPriceSettingInput, len(inputs))
	seenModels := make(map[string]struct{}, len(inputs))
	for index := range inputs {
		input, err := normalizePricingInput(inputs[index])
		if err != nil {
			return nil, fmt.Errorf("pricing at index %d: %w", index, err)
		}
		if _, exists := seenModels[input.Model]; exists {
			return nil, fmt.Errorf("pricing at index %d: %w: duplicate model %q", index, ErrInvalidPricingInput, input.Model)
		}
		seenModels[input.Model] = struct{}{}
		normalized[index] = input
	}

	settings := make([]entities.ModelPriceSetting, len(normalized))
	_, err := s.mutatePricing(ctx, func(tx *gorm.DB) error {
		for index := range normalized {
			setting, mutationErr := repository.UpsertModelPriceSetting(tx, normalized[index])
			if mutationErr != nil {
				return mutationErr
			}
			settings[index] = *setting
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrInvalidPricingSnapshot) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPricingInput, err)
		}
		return nil, err
	}
	return settings, nil
}

func normalizePricingInput(input servicedto.UpdatePricingInput) (repodto.ModelPriceSettingInput, error) {
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: model is required", ErrInvalidPricingInput)
	}
	pricingStyle := strings.ToLower(strings.TrimSpace(input.PricingStyle))
	if pricingStyle == "" {
		pricingStyle = entities.ModelPricingStyleOpenAI
	}
	if pricingStyle != entities.ModelPricingStyleOpenAI && pricingStyle != entities.ModelPricingStyleClaude {
		return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: pricing_style must be openai or claude", ErrInvalidPricingInput)
	}
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CacheReadPricePer1M < 0 || input.CacheWritePricePer1M < 0 {
		return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: prices must be non-negative", ErrInvalidPricingInput)
	}
	if input.PriceMultiplier != nil {
		multiplier := *input.PriceMultiplier
		if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: price_multiplier must be non-negative", ErrInvalidPricingInput)
		}
	}
	return repodto.ModelPriceSettingInput{
		Model:                modelName,
		PricingStyle:         pricingStyle,
		PromptPricePer1M:     input.PromptPricePer1M,
		CompletionPricePer1M: input.CompletionPricePer1M,
		CacheReadPricePer1M:  input.CacheReadPricePer1M,
		CacheWritePricePer1M: input.CacheWritePricePer1M,
		PriceMultiplier:      input.PriceMultiplier,
	}, nil
}

func (s *pricingService) DeletePricing(ctx context.Context, model string) error {
	_, err := s.mutatePricing(ctx, func(tx *gorm.DB) error {
		return repository.DeleteModelPriceSetting(tx, model)
	})
	return err
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
// upserts pricing entries via the catalog mutation path, and returns updated settings
// plus unmatched model names. Local-only feature preserved across upstream merges.
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
	_, err = s.mutatePricing(ctx, func(tx *gorm.DB) error {
		updated = updated[:0]
		unmatched = unmatched[:0]
		for _, model := range usedModels {
			info, ok := matches[model]
			if !ok {
				unmatched = append(unmatched, model)
				continue
			}

			promptPrice, _ := strconv.ParseFloat(info.Pricing.Prompt, 64)
			completionPrice, _ := strconv.ParseFloat(info.Pricing.Completion, 64)
			cachePrice, _ := strconv.ParseFloat(info.Pricing.CacheRead, 64)

			// OpenRouter API returns prices per-token, but we store per-1M-token.
			promptPrice *= 1_000_000
			completionPrice *= 1_000_000
			cachePrice *= 1_000_000

			// Clamp negative prices to 0 (OpenRouter returns negative values for free models).
			if promptPrice < 0 {
				promptPrice = 0
			}
			if completionPrice < 0 {
				completionPrice = 0
			}
			if cachePrice < 0 {
				cachePrice = 0
			}

			setting, upsertErr := repository.UpsertModelPriceSetting(tx, repodto.ModelPriceSettingInput{
				Model:                model,
				PromptPricePer1M:     promptPrice,
				CompletionPricePer1M: completionPrice,
				CacheReadPricePer1M:  cachePrice,
			})
			if upsertErr != nil {
				logrus.WithError(upsertErr).WithField("model", model).Warn("upsert OpenRouter pricing failed")
				continue
			}
			updated = append(updated, *setting)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
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
