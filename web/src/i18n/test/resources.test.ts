import { describe, expect, it } from 'vitest';
import i18n, { SUPPORTED_LANGUAGES } from '../index';

type Messages = { [key: string]: string | Messages };

const entries = (messages: Messages, prefix = ''): [string, string][] => (
  Object.entries(messages).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof value === 'string' ? [[path, value]] : entries(value, path);
  })
);
const placeholders = (message: string) => (message.match(/\{\{[^{}]+\}\}/g) ?? []).sort();
const english = Object.fromEntries(entries(i18n.getResourceBundle('en', 'translation')));

// Fork is English-only (SUPPORTED_LANGUAGES = ['en'], see LanguageSwitcher):
// pin the safety copy for the supported language only.
const criticalMessages = [
  {
    key: 'usage_stats.credentials_quota_reset_message_prompt',
    en: 'Consume 1 credit to reset now?',
  },
  {
    key: 'usage_stats.credentials_quota_reset_recovery_failed',
    en: 'Quota was reset, but CPA account recovery failed. Recover the account in CPA; do not reset quota again.',
  },
  {
    key: 'auth.session_expired',
    en: 'Your session expired. Please sign in again.',
  },
] as const;

describe('i18n resources', () => {
  it.each(SUPPORTED_LANGUAGES)('provides nonempty messages and matching interpolation parameters in %s', (language) => {
    const translated = Object.fromEntries(entries(i18n.getResourceBundle(language, 'translation')));
    expect(Object.keys(translated).sort()).toEqual(Object.keys(english).sort());
    for (const [key, message] of Object.entries(translated)) {
      expect(message.trim(), `${language}:${key}`).not.toBe('');
      expect(placeholders(message), `${language}:${key}`).toEqual(placeholders(english[key]));
    }
  });

  it.each(criticalMessages)('preserves the safety meaning of $key', ({ key, ...expected }) => {
    expect(Object.fromEntries(SUPPORTED_LANGUAGES.map((language) => [
      language,
      i18n.getResource(language, 'translation', key),
    ]))).toEqual(expected);
  });
});
