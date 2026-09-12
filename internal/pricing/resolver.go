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
// 只在 model 维度自身可以被判定为同一个上游模型时才归并：
// model 原名已注册则用原名；带 provider 前缀且去掉前缀后的名字已注册，则归到该注册名。
// 刻意不使用 model_alias 兜底：alias 兜底是"未定价模型借用 alias 价格"的计价规则，
// 而 alias 是客户端任意取的名字，用它改写展示维度会让真实模型名被别名覆盖
// （见 TestBuildAnalysisWithFilterFallsBackToAliasPricingWhenModelPriceMissing）。
func (r Resolver) ResolveModelName(dimensions UsageDimensions) string {
	canonical := canonicalizeUsageDimensions(dimensions)
	if r.snapshot == nil {
		return canonical.Model
	}
	if _, ok := r.snapshot.modelsByName[canonical.Model]; ok {
		return canonical.Model
	}
	if suffix, ok := stripProviderPrefix(canonical.Model); ok {
		if _, ok := r.snapshot.modelsByName[suffix]; ok {
			return suffix
		}
	}
	return canonical.Model
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
