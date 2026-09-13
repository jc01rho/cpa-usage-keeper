package pricing

import (
	"strings"

	"cpa-usage-keeper/internal/helper"
)

// CostSubject 是所有 usage 来源进入计价领域的唯一固定输入。
type CostSubject struct {
	Dimensions UsageDimensions
	Tokens     helper.UsageTokenCostInput
}

func NewCostSubject(dimensions UsageDimensions, tokens helper.UsageTokenCostInput) CostSubject {
	return CostSubject{
		Dimensions: canonicalizeUsageDimensions(dimensions),
		Tokens:     tokens,
	}
}

type CostResult struct {
	Cost           helper.UsageTokenCostBreakdown
	Available      bool
	PricingStyle   string
	MatchedModel   string
	MatchedBy      string
	RuleMultiplier float64
}

// Resolver 在创建时固定绑定一个 Snapshot，确保单个响应不会混用新旧价格。
type Resolver struct {
	snapshot *Snapshot
}

func (r Resolver) ActiveFields() ActiveFields {
	if r.snapshot == nil {
		return 0
	}
	return r.snapshot.activeFields
}

func (r Resolver) Calculate(subject CostSubject) CostResult {
	model, matchedModel, matchedBy, found := r.matchModel(subject.Dimensions)
	if !found {
		return CostResult{
			Available:      !helper.UsageTokenInputRequiresPricing(subject.Tokens),
			RuleMultiplier: 1,
		}
	}

	breakdown := helper.CalculateUsageTokenCostBreakdown(subject.Tokens, model.pricing)
	ruleMultiplier := 1.0
	if model.pricing.PriceMultiplier == nil || *model.pricing.PriceMultiplier != 0 {
		ruleMultiplier = matchingRuleMultiplier(model.rules, subject.Dimensions)
		breakdown = helper.ScaleUsageTokenCostBreakdown(breakdown, ruleMultiplier)
	}
	return CostResult{
		Cost:           breakdown,
		Available:      true,
		PricingStyle:   model.pricing.PricingStyle,
		MatchedModel:   matchedModel,
		MatchedBy:      matchedBy,
		RuleMultiplier: ruleMultiplier,
	}
}

func (r Resolver) matchModel(dimensions UsageDimensions) (compiledModel, string, string, bool) {
	if r.snapshot == nil {
		return compiledModel{}, "", "", false
	}
	if model, ok := r.snapshot.modelsByName[dimensions.Model]; ok {
		return model, dimensions.Model, "model", true
	}
	// 有些上游把 provider 前缀拼进 model（如 "deepseek/deepseek-v4.1-flash"）。
	// 只有去掉第一段前缀后的名字本身在价格表里已注册，才按该注册名合并；
	// 否则无法区分前缀是 provider 命名空间还是模型名自身包含的 "/"，保持原样不合并。
	if suffix, ok := stripProviderPrefix(dimensions.Model); ok {
		if model, ok := r.snapshot.modelsByName[suffix]; ok {
			return model, suffix, "model_prefix_stripped", true
		}
	}
	if model, ok := r.snapshot.modelsByName[dimensions.ModelAlias]; ok {
		return model, dimensions.ModelAlias, "model_alias", true
	}
	return compiledModel{}, "", "", false
}

// stripProviderPrefix 拆出 "provider/model" 形式里第一个 "/" 之后的部分。
// 只返回候选值，由调用方决定是否存在匹配的价格设置来验证前缀确实是命名空间。
func stripProviderPrefix(model string) (string, bool) {
	idx := strings.IndexByte(model, '/')
	if idx < 0 || idx == len(model)-1 {
		return "", false
	}
	return model[idx+1:], true
}

// ResolveModelName 返回该 usage 行应归并展示的规范模型名。
//
// 只在 model 维度自身可以被判定为同一个上游模型时才归并：带 provider 前缀且
// 去掉前缀后的名字已注册，则归到该注册名。
//
// 带前缀的名字自己也可能已注册（如 "deepseek/deepseek-v4.1-flash" 和
// "deepseek-v4.1-flash" 同时存在于价格表）。这种情况下仅当两边的价格与规则
// 完全一致时才归并：价格不同意味着它们是不同的计费实体，合并会扭曲成本。
//
// 刻意不使用 model_alias 兜底：alias 兜底是"未定价模型借用 alias 价格"的计价规则，
// 而 alias 是客户端任意取的名字，用它改写展示维度会让真实模型名被别名覆盖
// （见 TestBuildAnalysisWithFilterFallsBackToAliasPricingWhenModelPriceMissing）。
func (r Resolver) ResolveModelName(dimensions UsageDimensions) string {
	canonical := canonicalizeUsageDimensions(dimensions)
	if r.snapshot == nil {
		return canonical.Model
	}
	suffix, hasPrefix := stripProviderPrefix(canonical.Model)
	if !hasPrefix {
		return canonical.Model
	}
	bare, bareRegistered := r.snapshot.modelsByName[suffix]
	if !bareRegistered {
		// 去前缀后没有注册名，无法区分前缀是 provider 命名空间还是模型名自身
		// 包含的 "/"（如 "meta-llama/llama-3-70b"），保持原样。
		return canonical.Model
	}
	prefixed, prefixedRegistered := r.snapshot.modelsByName[canonical.Model]
	if prefixedRegistered && !sameBilling(prefixed, bare) {
		// 两边都注册但计费不同，视为不同实体，不合并。
		return canonical.Model
	}
	return suffix
}

// sameBilling 判断两个已编译模型是否在计费上完全等价。
// 单价相同但规则不同时，实际扣费仍会不同，所以规则也必须逐条比较。
func sameBilling(a, b compiledModel) bool {
	if a.pricing.PricingStyle != b.pricing.PricingStyle ||
		a.pricing.PromptPricePer1M != b.pricing.PromptPricePer1M ||
		a.pricing.CompletionPricePer1M != b.pricing.CompletionPricePer1M ||
		a.pricing.CacheReadPricePer1M != b.pricing.CacheReadPricePer1M ||
		a.pricing.CacheWritePricePer1M != b.pricing.CacheWritePricePer1M {
		return false
	}
	if priceMultiplierValue(a.pricing.PriceMultiplier) != priceMultiplierValue(b.pricing.PriceMultiplier) {
		return false
	}
	if len(a.rules) != len(b.rules) {
		return false
	}
	// rules 按输入顺序编译，未排序，所以按多重集合比较，避免仅顺序不同被误判为不等。
	counts := make(map[compiledRule]int, len(a.rules))
	for _, rule := range a.rules {
		counts[rule]++
	}
	for _, rule := range b.rules {
		counts[rule]--
		if counts[rule] < 0 {
			return false
		}
	}
	return true
}

func priceMultiplierValue(multiplier *float64) float64 {
	if multiplier == nil {
		return 1
	}
	return *multiplier
}

func matchingRuleMultiplier(rules []compiledRule, dimensions UsageDimensions) float64 {
	multiplier := 1.0
	for _, rule := range rules {
		if dimensions.Value(rule.field) != rule.value {
			continue
		}
		if rule.multiplier == 0 {
			return 0
		}
		multiplier *= rule.multiplier
	}
	return multiplier
}
