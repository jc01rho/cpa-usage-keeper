package test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func TestResolverPrefersModelThenFallsBackToAlias(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t,
		pricing.ModelConfig{Pricing: testPricingWithPrompt("base-model", 10)},
		pricing.ModelConfig{Pricing: testPricingWithPrompt("alias-model", 2)},
	)
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: " base-model ", ModelAlias: " alias-model "}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	result := resolver.CalculateFee(subject)
	assertResultCost(t, result, 10)

	result = resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing", ModelAlias: "alias-model"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 2)
}

func TestResolverStripsProviderPrefixOnlyWhenSuffixIsPriced(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("deepseek-v4.1-flash", 4)})

	// "deepseek/deepseek-v4.1-flash" 前缀去掉后命中已注册的 "deepseek-v4.1-flash"，应当合并计价。
	// Keeper v2 FeeResult no longer exposes MatchedModel/MatchedBy; the registered suffix price proves the match.
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "deepseek/deepseek-v4.1-flash"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 4)

	// 无注册匹配时，即使包含 "/" 也不能猜测地剥离前缀（可能是模型名自身包含 "/"）。
	unmatched := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "meta-llama/llama-3-70b"}, helper.UsageTokenCostInput{InputTokens: 1}))
	if unmatched.Available {
		t.Fatalf("expected no match without a registered suffix or full name, got %+v", unmatched)
	}
}

func TestResolveModelNameMergesOnlyByModelDimensionNeverByAlias(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t,
		pricing.ModelConfig{Pricing: testPricingWithPrompt("deepseek-v4.1-flash", 4)},
		pricing.ModelConfig{Pricing: testPricingWithPrompt("my-deepseek", 4)},
	)

	// 已注册的原名直接保留。
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "deepseek-v4.1-flash"}); got != "deepseek-v4.1-flash" {
		t.Fatalf("expected registered model name to be kept, got %q", got)
	}
	// 带 provider 前缀且去前缀后命中注册名 → 合并到该注册名。
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "deepseek/deepseek-v4.1-flash"}); got != "deepseek-v4.1-flash" {
		t.Fatalf("expected prefix-stripped canonical name, got %q", got)
	}
	// 同一个 upstream 模型被多个不同 alias 调用，仍归到同一个 model 规范名。
	for _, alias := range []string{"my-deepseek", "your-deepseek", ""} {
		if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "deepseek/deepseek-v4.1-flash", ModelAlias: alias}); got != "deepseek-v4.1-flash" {
			t.Fatalf("alias %q must not change the merged model name, got %q", alias, got)
		}
	}
	// alias 已注册也不能改写展示维度：alias 兜底只用于计价，不用于归并。
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "unregistered-upstream-name", ModelAlias: "my-deepseek"}); got != "unregistered-upstream-name" {
		t.Fatalf("expected real model name to survive alias pricing fallback, got %q", got)
	}
	// "/" 可能是模型名自身的一部分；去前缀后没有注册名时不能猜测地剥离。
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "meta-llama/llama-3-70b"}); got != "meta-llama/llama-3-70b" {
		t.Fatalf("expected unregistered slashed name to stay intact, got %q", got)
	}
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: ""}); got != "unknown" {
		t.Fatalf("expected empty model to canonicalize to unknown, got %q", got)
	}
}

// 带前缀名和无前缀名可能同时存在于价格表（生产环境实际情况）。
// 价格一致时应归并到无前缀名；价格不同时是不同计费实体，必须分开。
func TestResolveModelNameMergesRegisteredPrefixedNameOnlyWhenBillingMatches(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t,
		pricing.ModelConfig{Pricing: testPricingWithPrompt("deepseek-v4.1-flash", 0.15)},
		pricing.ModelConfig{Pricing: testPricingWithPrompt("deepseek/deepseek-v4.1-flash", 0.15)},
		pricing.ModelConfig{Pricing: testPricingWithPrompt("claude-opus-5", 15)},
		pricing.ModelConfig{Pricing: testPricingWithPrompt("aion/claude-opus-5", 3)},
	)

	// 两边都注册且价格完全相同 → 归并到无前缀名。
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "deepseek/deepseek-v4.1-flash"}); got != "deepseek-v4.1-flash" {
		t.Fatalf("expected identically priced prefixed name to merge, got %q", got)
	}
	// 两边都注册但价格不同 → 保持分开，否则成本会被单一单价抹平。
	if got := resolver.ResolveModelName(pricing.UsageDimensions{Model: "aion/claude-opus-5"}); got != "aion/claude-opus-5" {
		t.Fatalf("expected differently priced prefixed name to stay separate, got %q", got)
	}
}

// 乘数或规则不同时，即使单价相同实际扣费也不同，不能归并。
func TestResolveModelNameKeepsPrefixedNameWhenMultiplierOrRulesDiffer(t *testing.T) {
	t.Parallel()

	multiplierResolver := compileResolver(t,
		pricing.ModelConfig{Pricing: testPricingWithPromptAndMultiplier("glm-5.1", 1, 1)},
		pricing.ModelConfig{Pricing: testPricingWithPromptAndMultiplier("glm/glm-5.1", 1, 0.5)},
	)
	if got := multiplierResolver.ResolveModelName(pricing.UsageDimensions{Model: "glm/glm-5.1"}); got != "glm/glm-5.1" {
		t.Fatalf("expected differing price multiplier to block the merge, got %q", got)
	}

	rulesResolver := compileResolver(t,
		pricing.ModelConfig{Pricing: testPricingWithPrompt("kimi-k3", 1)},
		pricing.ModelConfig{
			Pricing: testPricingWithPrompt("aion/kimi-k3", 1),
			Rules:   []pricing.RuleConfig{{Key: "service_tier", Value: "batch", Multiplier: 0.5}},
		},
	)
	if got := rulesResolver.ResolveModelName(pricing.UsageDimensions{Model: "aion/kimi-k3"}); got != "aion/kimi-k3" {
		t.Fatalf("expected differing rules to block the merge, got %q", got)
	}

	// 规则集合相同但顺序不同，仍应视为等价而归并。
	orderResolver := compileResolver(t,
		pricing.ModelConfig{
			Pricing: testPricingWithPrompt("mimo-v2.5-pro", 1),
			Rules: []pricing.RuleConfig{
				{Key: "service_tier", Value: "batch", Multiplier: 0.5},
				{Key: "reasoning_effort", Value: "high", Multiplier: 2},
			},
		},
		pricing.ModelConfig{
			Pricing: testPricingWithPrompt("mim/mimo-v2.5-pro", 1),
			Rules: []pricing.RuleConfig{
				{Key: "reasoning_effort", Value: "high", Multiplier: 2},
				{Key: "service_tier", Value: "batch", Multiplier: 0.5},
			},
		},
	)
	if got := orderResolver.ResolveModelName(pricing.UsageDimensions{Model: "mim/mimo-v2.5-pro"}); got != "mimo-v2.5-pro" {
		t.Fatalf("expected rule order to be irrelevant for the merge, got %q", got)
	}
}

func TestResolverPreservesMissingPriceAvailabilityContract(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t)
	billable := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing"}, helper.UsageTokenCostInput{InputTokens: 1}))
	if billable.Available || billable.TotalCostUSD != 0 {
		t.Fatalf("expected missing billable price to be unavailable, got %+v", billable)
	}
	empty := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing"}, helper.UsageTokenCostInput{}))
	if !empty.Available || empty.TotalCostUSD != 0 {
		t.Fatalf("expected missing zero-token price to be available, got %+v", empty)
	}
}

func TestResolverWithoutRulesPreservesFourTokenTotalAndModelMultiplier(t *testing.T) {
	t.Parallel()

	one := 1.0
	zero := 0.0
	tokens := helper.UsageTokenCostInput{
		InputTokens:         1_000_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}
	for _, testCase := range []struct {
		name       string
		multiplier *float64
		dimensions pricing.UsageDimensions
	}{
		{name: "nil multiplier direct model", dimensions: pricing.UsageDimensions{Model: "priced-model"}},
		{name: "one multiplier direct model", multiplier: &one, dimensions: pricing.UsageDimensions{Model: "priced-model", ModelAlias: "alias-model"}},
		{name: "nil multiplier alias fallback", dimensions: pricing.UsageDimensions{Model: "missing-model", ModelAlias: "priced-model"}},
		{name: "zero multiplier", multiplier: &zero, dimensions: pricing.UsageDimensions{Model: "priced-model"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			setting := entities.ModelPriceSetting{
				Model:                "priced-model",
				PricingStyle:         entities.ModelPricingStyleOpenAI,
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.3,
				CacheWritePricePer1M: 3.75,
				PriceMultiplier:      testCase.multiplier,
			}
			resolver := compileResolver(t, pricing.ModelConfig{Pricing: setting})
			result := resolver.CalculateFee(pricing.NewCostSubject(testCase.dimensions, tokens))
			if !result.Available {
				t.Fatalf("unexpected no-Rules match result: %+v", result)
			}
			want := 0.7*3 + 0.2*0.3 + 0.1*3.75 + 0.5*15
			if testCase.multiplier != nil {
				want *= *testCase.multiplier
			}
			assertResultCost(t, result, want)
		})
	}
}

func TestResolverMultipliesEveryMatchingRuleContinuously(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPromptAndMultiplier("model-a", 10, 1.5),
		Rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: 2},
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
			{Key: "endpoint", Value: "/v1/responses", Multiplier: 4},
		},
	})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{
		Model:           "model-a",
		ServiceTier:     "priority",
		ReasoningEffort: "xhigh",
		Endpoint:        "/v1/responses",
	}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))

	assertResultCost(t, result, 10*1.5*2*3*4)
}

func TestResolverMatchesValuesExactlyAndCaseSensitively(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules:   []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}},
	})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "Priority"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 10)
}

func TestResolverSupportsAllNineRuleFields(t *testing.T) {
	t.Parallel()

	rules := []pricing.RuleConfig{
		{Key: "api_group_key", Value: "group", Multiplier: 2},
		{Key: "model", Value: "model-a", Multiplier: 2},
		{Key: "auth_index", Value: "auth", Multiplier: 2},
		{Key: "model_alias", Value: "alias", Multiplier: 2},
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "response_service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 2},
		{Key: "endpoint", Value: "/v1/responses", Multiplier: 2},
		{Key: "executor_type", Value: "openai", Multiplier: 2},
	}
	resolver := compileResolver(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("model-a", 1), Rules: rules})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{
		APIGroupKey:         "group",
		Model:               "model-a",
		AuthIndex:           "auth",
		ModelAlias:          "alias",
		ServiceTier:         "priority",
		ResponseServiceTier: "priority",
		ReasoningEffort:     "xhigh",
		Endpoint:            "/v1/responses",
		ExecutorType:        "openai",
	}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))

	assertResultCost(t, result, 512)
}

func TestResolverTreatsZeroAsAvailableAndOneAsInactive(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules: []pricing.RuleConfig{
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 1},
			{Key: "service_tier", Value: "priority", Multiplier: 0},
		},
	})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.TotalCostUSD != 0 {
		t.Fatalf("expected matched zero rule to return available zero cost, got %+v", result)
	}
}

func TestResolverZeroRuleResultDoesNotDependOnRuleOrder(t *testing.T) {
	t.Parallel()

	rules := []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 0},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 1e100},
	}
	for _, ordered := range [][]pricing.RuleConfig{rules, {rules[1], rules[0]}} {
		resolver := compileResolver(t, pricing.ModelConfig{
			Pricing: testPricingWithPrompt("model-a", 1e-100),
			Rules:   ordered,
		})
		result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: math.MaxInt64}))
		if !result.Available || result.TotalCostUSD != 0 {
			t.Fatalf("expected finite zero result for rules %+v, got %+v", ordered, result)
		}
	}
}

func TestResolverCalculateFeeHasNoHeapAllocations(t *testing.T) {
	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: 2},
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
		},
	})
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	allocations := testing.AllocsPerRun(1000, func() {
		_ = resolver.CalculateFee(subject)
	})
	if allocations != 0 {
		t.Fatalf("expected zero allocations, got %.2f", allocations)
	}
}

func compileSnapshot(t testing.TB, configs ...pricing.ModelConfig) *pricing.Snapshot {
	t.Helper()
	snapshot, err := pricing.CompileSnapshot(configs)
	if err != nil {
		t.Fatalf("CompileSnapshot returned error: %v", err)
	}
	return snapshot
}

func compileResolver(t *testing.T, configs ...pricing.ModelConfig) pricing.Resolver {
	t.Helper()
	return pricing.NewCatalog(compileSnapshot(t, configs...)).NewResolver()
}

func testPricingWithPrompt(model string, prompt float64) entities.ModelPriceSetting {
	return testPricingWithPromptAndMultiplier(model, prompt, 1)
}

func testPricingWithPromptAndMultiplier(model string, prompt, multiplier float64) entities.ModelPriceSetting {
	return entities.ModelPriceSetting{
		Model:            model,
		PricingStyle:     entities.ModelPricingStyleOpenAI,
		PromptPricePer1M: prompt,
		PriceMultiplier:  &multiplier,
	}
}

func assertResultCost(t *testing.T, result pricing.FeeResult, want float64) {
	t.Helper()
	if !result.Available {
		t.Fatalf("expected available cost, got %+v", result)
	}
	if !(math.Abs(result.TotalCostUSD-want) <= math.Max(1e-9, math.Abs(want)*1e-12)) {
		t.Fatalf("cost = %.12f, want %.12f", result.TotalCostUSD, want)
	}
}
