import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import i18n from '../../../../i18n'
import { CredentialSubscriptionBadge } from '../CredentialSubscriptionBadge'
import type { SubscriptionBadgeModel } from '../credentialSubscription'

const dynamicModel: SubscriptionBadgeModel = {
  kind: 'codex-pro20x',
  fallbackLabel: 'Pro 20x',
}

describe('CredentialSubscriptionBadge', () => {
  it('preserves the existing premium Codex DOM and shared motion layers', () => {
    const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, { model: dynamicModel }))

    expect(html).toContain('credentialPlanBadge')
    expect(html).toContain('credentialPlanBadgePro20x')
    expect(html).toContain('credentialPlanBadgeFlow')
    expect(html).toContain('credentialPlanBadgeCorona')
    expect(html).toContain('credentialPlanBadgeLabel')
    expect(html).toMatch(/credentialPlanBadgeFlow[^>]+aria-hidden="true"/)
    expect(html).toMatch(/credentialPlanBadgeCorona[^>]+aria-hidden="true"/)
    expect(html).toContain('Pro 20x')
  })

  it('keeps lightweight and unknown Codex badges free of premium DOM layers', () => {
    for (const model of [
      { kind: 'codex-free', fallbackLabel: 'Free' },
      { kind: 'codex-unknown', fallbackLabel: 'Custom' },
    ] satisfies SubscriptionBadgeModel[]) {
      const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, { model }))
      expect(html).toContain('credentialPlanBadgeLabel')
      expect(html).toContain(model.fallbackLabel)
      expect(html).not.toContain('credentialPlanBadgeFlow')
      expect(html).not.toContain('credentialPlanBadgeCorona')
    }
  })

  it('resolves known labels through i18n', async () => {
    await i18n.changeLanguage('en')
    const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
      model: { kind: 'codex-team', labelKey: 'usage_stats.credentials_subscription_codex_team' },
    }))
    expect(html).toContain('Team')
  })

  it.each([10, 50])('uses two shared decorative nodes for each of %i dynamic badges', (count) => {
    const html = renderToStaticMarkup(createElement('div', null, Array.from({ length: count }, (_, index) => (
      createElement(CredentialSubscriptionBadge, { key: String(index), model: dynamicModel })
    ))))

    expect(html.match(/credentialPlanBadgeFlow/g)).toHaveLength(count)
    expect(html.match(/credentialPlanBadgeCorona/g)).toHaveLength(count)
    expect(html.match(/credentialPlanBadgeLabel/g)).toHaveLength(count)
  })
})
