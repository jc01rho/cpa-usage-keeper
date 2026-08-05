import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

const CODEX_SUBSCRIPTION_KEYS = [
  'usage_stats.credentials_subscription_codex_free',
  'usage_stats.credentials_subscription_codex_plus',
  'usage_stats.credentials_subscription_codex_team',
  'usage_stats.credentials_subscription_codex_pro_5x',
  'usage_stats.credentials_subscription_codex_pro_20x',
  'usage_stats.credentials_subscription_codex_enterprise',
] as const

describe('credential subscription translations', () => {
  it('publishes every known Codex subscription label in each supported language', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      for (const key of CODEX_SUBSCRIPTION_KEYS) {
        const value = i18n.getResource(language, 'translation', key)
        expect(value, `${language}:${key}`).toEqual(expect.any(String))
        expect(value.trim(), `${language}:${key}`).not.toBe('')
      }
    }
  })
})
