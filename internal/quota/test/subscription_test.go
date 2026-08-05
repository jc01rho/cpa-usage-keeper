package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestNormalizeCodexSubscription(t *testing.T) {
	tests := []struct {
		name string
		plan string
		want string
	}{
		{name: "free", plan: "free", want: "free"},
		{name: "plus", plan: "Plus", want: "plus"},
		{name: "team", plan: "team", want: "team"},
		{name: "pro", plan: " PRO ", want: "pro-20x"},
		{name: "prolite", plan: "prolite", want: "pro-5x"},
		{name: "pro-dash-lite", plan: "pro-lite", want: "pro-5x"},
		{name: "pro-underscore-lite", plan: "pro_lite", want: "pro-5x"},
		{name: "enterprise", plan: "enterprise", want: "enterprise"},
		{name: "unknown", plan: " ChatGPT-Pro-Monthly ", want: "ChatGPT-Pro-Monthly"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := quota.NormalizeSubscription(quota.ProviderOutput{
				Provider: " CoDeX ",
				Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
					PlanType: test.plan,
				}},
			})
			if got == nil || got.Provider != "codex" || got.Plan != test.want || got.TierID != "" || got.TierName != "" {
				t.Fatalf("NormalizeSubscription() = %#v, want provider=codex plan=%s", got, test.want)
			}
		})
	}
}

func TestNormalizeCodexSubscriptionSupportsPointerResult(t *testing.T) {
	got := quota.NormalizeSubscription(quota.ProviderOutput{
		Provider: "codex",
		Result:   &quota.CodexResult{Usage: &quota.CodexUsagePayload{PlanType: "plus"}},
	})
	if got == nil || got.Provider != "codex" || got.Plan != "plus" {
		t.Fatalf("NormalizeSubscription() = %#v, want codex plus", got)
	}
}

func TestNormalizeSubscriptionRejectsMissingOrUnregisteredValues(t *testing.T) {
	for _, output := range []quota.ProviderOutput{
		{},
		{Provider: "codex", Result: quota.CodexResult{}},
		{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{PlanType: "   "}}},
		{Provider: "gemini-cli", Result: quota.GeminiCLIResult{}},
		{Provider: "xai", Result: quota.XAIResult{}},
	} {
		if got := quota.NormalizeSubscription(output); got != nil {
			t.Fatalf("NormalizeSubscription(%#v) = %#v, want nil", output, got)
		}
	}
}

func TestResolveIdentitySubscriptionOnlyPublishesCodexMetadata(t *testing.T) {
	pro := " pro "
	got := quota.ResolveIdentitySubscription(entities.UsageIdentity{
		AuthType: entities.UsageIdentityAuthTypeAuthFile,
		Type:     "codex",
		Provider: "Codex",
		PlanType: &pro,
	})
	if got == nil || got.Provider != "codex" || got.Plan != "pro-20x" {
		t.Fatalf("ResolveIdentitySubscription() = %#v, want codex pro-20x", got)
	}

	for _, identity := range []entities.UsageIdentity{
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, Type: "claude", Provider: "Claude", PlanType: &pro},
		{AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "codex", Provider: "Codex", PlanType: &pro},
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, Type: "codex", Provider: "Codex"},
	} {
		if got := quota.ResolveIdentitySubscription(identity); got != nil {
			t.Fatalf("ResolveIdentitySubscription(%#v) = %#v, want nil", identity, got)
		}
	}
}
