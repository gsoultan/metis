import { describe, expect, it } from 'bun:test';

import { DEFAULT_LOCALE, LOCALES, localeFor, resolveLocale, type Locale } from './locales';

const available: Locale[] = [
  { tag: 'en', endonym: 'English', load: async () => ({}) },
  { tag: 'id', endonym: 'Bahasa Indonesia', load: async () => ({}) },
];

describe('choosing the language to start in', () => {
  /*
   * An explicit choice outranks everything. Somebody who has said what they
   * want should not be second-guessed because they are travelling, or because
   * their employer's laptop is configured in another language.
   */
  it('honours a stored choice above the browser', () => {
    expect(resolveLocale('id', ['en-GB', 'en'], available)).toBe('id');
  });

  it('ignores a stored choice for a language that no longer exists', () => {
    expect(resolveLocale('kl', ['id'], available)).toBe('id');
  });

  it('falls back to the browser when nothing is stored', () => {
    expect(resolveLocale(null, ['id-ID', 'en'], available)).toBe('id');
  });

  /*
   * Matching on the language before the region. Somebody with `en-AU` wants
   * English; refusing because there is no Australian catalogue would be
   * pedantic, and they would get Indonesian.
   */
  it('matches a language even when the region differs', () => {
    expect(resolveLocale(null, ['en-AU'], available)).toBe('en');
    expect(resolveLocale(null, ['id-ID'], available)).toBe('id');
  });

  it('reads the browser’s preferences in order', () => {
    // The first one that can be served wins, which is what the order means.
    expect(resolveLocale(null, ['fr-FR', 'id', 'en'], available)).toBe('id');
  });

  it('falls back to English when nothing matches', () => {
    expect(resolveLocale(null, ['fr-FR', 'de'], available)).toBe(DEFAULT_LOCALE);
    expect(resolveLocale(null, [], available)).toBe(DEFAULT_LOCALE);
  });

  it('is not confused by case', () => {
    expect(resolveLocale(null, ['ID-id'], available)).toBe('id');
  });
});

describe('the languages on offer', () => {
  it('names each one the way it names itself', () => {
    // Somebody looking for their own language is looking for the word they use,
    // not the English name for it.
    for (const locale of LOCALES) {
      expect(locale.endonym.length).toBeGreaterThan(0);
    }
    expect(LOCALES.find((l) => l.tag === 'id')?.endonym).toBe('Bahasa Indonesia');
  });

  it('always resolves a locale, even for a tag it does not have', () => {
    expect(localeFor('nonsense').tag).toBe(DEFAULT_LOCALE);
  });
});

describe('the catalogues themselves', () => {
  it('translates every key English defines, or visibly does not', async () => {
    /*
     * Not an equality assertion: a partial catalogue is allowed, because a
     * missing key shows the key rather than falling back to English, which is
     * what keeps the gap visible. This asserts the *shape* — that every key a
     * catalogue does define is one English knows about — so a typo in a
     * translated key is caught here rather than by somebody seeing
     * "inbox.titel" on screen.
     */
    const english = (await import('./catalogues/en')).default;
    for (const locale of LOCALES.filter((l) => l.tag !== 'en')) {
      const catalogue = await locale.load();
      for (const key of Object.keys(catalogue)) {
        expect(english[key], `${locale.tag} defines "${key}", which English does not`).toBeDefined();
      }
    }
  });
});

/*
 * The getting-started card and Help sit on the Dashboard, which is translated,
 * and were written in English only: in Indonesian the card read "Getting
 * started … Deploy a process" beside "Dasbor". Their words are in the
 * catalogues now, and a translation that only copies the English is not one.
 */
describe('the getting-started card, Help and the glossary', () => {
  const AREAS = ['start.', 'help.', 'glossary.'];

  it('are translated into Indonesian, every word of them, and not copied', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    const keys = Object.keys(english).filter((key) => AREAS.some((area) => key.startsWith(area)));
    expect(keys.length).toBeGreaterThan(40);
    for (const key of keys) {
      expect(indonesian[key], `id has no "${key}"`).toBeDefined();
      expect(indonesian[key], `id copies the English for "${key}"`).not.toBe(english[key]);
    }
  });
});

/*
 * The role view on the Platform access page was written with its words in the
 * catalogues from the start, so a translation has every one of them.
 */
describe('the Platform access role view', () => {
  it('is translated into Indonesian, every word of it, and not copied', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    const keys = Object.keys(english).filter((key) => key.startsWith('access.'));
    expect(keys.length).toBeGreaterThan(20);
    for (const key of keys) {
      expect(indonesian[key], `id has no "${key}"`).toBeDefined();
      expect(indonesian[key], `id copies the English for "${key}"`).not.toBe(english[key]);
    }
  });
});
