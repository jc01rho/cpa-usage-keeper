import type { UsageSubscriptionInfo } from '@/lib/types'

export type SubscriptionBadgeKind =
  | 'codex-free'
  | 'codex-plus'
  | 'codex-team'
  | 'codex-pro5x'
  | 'codex-pro20x'
  | 'codex-enterprise'
  | 'codex-unknown'

export type SubscriptionBadgeModel = {
  kind: SubscriptionBadgeKind
  labelKey?: string
  fallbackLabel?: string
}

const CODEX_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'codex-free', labelKey: 'usage_stats.credentials_subscription_codex_free' }],
  ['plus', { kind: 'codex-plus', labelKey: 'usage_stats.credentials_subscription_codex_plus' }],
  ['team', { kind: 'codex-team', labelKey: 'usage_stats.credentials_subscription_codex_team' }],
  ['pro-5x', { kind: 'codex-pro5x', labelKey: 'usage_stats.credentials_subscription_codex_pro_5x' }],
  ['pro-20x', { kind: 'codex-pro20x', labelKey: 'usage_stats.credentials_subscription_codex_pro_20x' }],
  ['enterprise', { kind: 'codex-enterprise', labelKey: 'usage_stats.credentials_subscription_codex_enterprise' }],
])

export function resolveCredentialSubscriptionBadge(subscription?: UsageSubscriptionInfo): SubscriptionBadgeModel | undefined {
  const provider = subscription?.provider.trim().toLowerCase()
  const displayPlan = subscription?.plan.trim()
  if (!provider || !displayPlan || provider !== 'codex') {
    return undefined
  }

  const known = CODEX_PRESENTATIONS.get(displayPlan.toLowerCase())
  if (known) {
    return known
  }

  return {
    kind: 'codex-unknown',
    fallbackLabel: displayPlan,
  }
}
