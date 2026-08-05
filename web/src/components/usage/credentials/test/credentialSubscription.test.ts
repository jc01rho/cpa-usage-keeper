import { describe, expect, it } from 'vitest'
import type { UsageSubscriptionInfo } from '@/lib/types'
import { resolveCredentialSubscriptionBadge } from '../credentialSubscription'

describe('credentialSubscription', () => {
  it.each([
    ['free', 'codex-free', 'usage_stats.credentials_subscription_codex_free'],
    ['plus', 'codex-plus', 'usage_stats.credentials_subscription_codex_plus'],
    ['team', 'codex-team', 'usage_stats.credentials_subscription_codex_team'],
    ['pro-5x', 'codex-pro5x', 'usage_stats.credentials_subscription_codex_pro_5x'],
    ['pro-20x', 'codex-pro20x', 'usage_stats.credentials_subscription_codex_pro_20x'],
    ['enterprise', 'codex-enterprise', 'usage_stats.credentials_subscription_codex_enterprise'],
  ] as const)('maps Codex %s to its namespaced badge', (plan, kind, labelKey) => {
    expect(resolveCredentialSubscriptionBadge({ provider: ' CoDeX ', plan: ` ${plan.toUpperCase()} ` })).toEqual({
      kind,
      labelKey,
    })
  })

  it('keeps unknown Codex plans readable without treating them as known tiers', () => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'codex', plan: ' ChatGPT-Pro-Monthly ' })).toEqual({
      kind: 'codex-unknown',
      fallbackLabel: 'ChatGPT-Pro-Monthly',
    })
  })

  it.each(['constructor', 'toString', '__proto__'])('treats inherited object key %s as an unknown plan', (plan) => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'codex', plan })).toEqual({
      kind: 'codex-unknown',
      fallbackLabel: plan,
    })
  })

  it.each([
    undefined,
    { provider: '', plan: 'plus' },
    { provider: 'codex', plan: '' },
    { provider: 'claude', plan: 'pro' },
    { provider: 'antigravity', plan: 'ultra' },
  ] as Array<UsageSubscriptionInfo | undefined>)('does not invent badges for missing or unregistered subscriptions', (subscription) => {
    expect(resolveCredentialSubscriptionBadge(subscription)).toBeUndefined()
  })
})
