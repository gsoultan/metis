import { describe, expect, it } from 'bun:test';

import { DEFAULT_LOCALE, LOCALES, localeFor, resolveLocale, type Locale } from './locales';
import { format } from './translate';

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

/*
 * Handing a task back, and the sentences the timeline tells a hand-over in,
 * were written with their words in the catalogues from the start.
 */
describe('handing a task over', () => {
  const AREAS = ['handover.', 'timeline.'];

  it('is translated into Indonesian, every word of it, and not copied', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    const keys = Object.keys(english).filter((key) => AREAS.some((area) => key.startsWith(area)));
    expect(keys.length).toBeGreaterThan(10);
    for (const key of keys) {
      expect(indonesian[key], `id has no "${key}"`).toBeDefined();
      expect(indonesian[key], `id copies the English for "${key}"`).not.toBe(english[key]);
    }
  });
});

/*
 * Why a migration left an instance where it was. The server sends the cause
 * as a code and the dialog says it from the catalogue, so each cause has its
 * words in both languages from the start. Which causes there are is the
 * server's to say: tests/roledrift holds these keys to its closed set.
 */
describe('why a migration passed an instance over', () => {
  it('is translated into Indonesian, every cause, and not copied', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    const keys = Object.keys(english).filter((key) => key.startsWith('migration.passedOver.'));
    expect(keys.length).toBeGreaterThan(0);
    for (const key of keys) {
      expect(indonesian[key], `id has no "${key}"`).toBeDefined();
      expect(indonesian[key], `id copies the English for "${key}"`).not.toBe(english[key]);
    }
  });

  /*
   * An instance that left its step may have gone on to finish. The sentence
   * for it must not tell the reader to apply the migration again whatever
   * became of the instance: for one that is no longer running there is
   * nothing to apply it to.
   */
  it('tells the reader to apply again only if the instance is still running', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    expect(english['migration.passedOver.left_the_step']).toContain('if it is still running, apply the same migration again');
    expect(indonesian['migration.passedOver.left_the_step']).toContain('jika masih berjalan, terapkan migrasi yang sama lagi');
  });

  /*
   * The server lists ten of the steps a cause is about, beside how many there
   * were. The rest are said as every other cut list in the catalogue says
   * what it leaves out: a plural of its own, which the dialog puts at the end
   * of the names it has.
   */
  it('has words for the steps a list leaves out, in both languages', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    expect(format(english, 'migration.passedOverStepsMore', { count: 1 })).toBe('and 1 more');
    expect(format(english, 'migration.passedOverStepsMore', { count: 15 })).toBe('and 15 more');
    expect(format(indonesian, 'migration.passedOverStepsMore', { count: 15 })).toBe('dan 15 lainnya');
    const steps = `"Legal review", "Credit check" ${format(english, 'migration.passedOverStepsMore', { count: 15 })}`;
    expect(format(english, 'migration.passedOver.nowhere_to_land', { steps, version: 2 })).toBe(
      'It had work at "Legal review", "Credit check" and 15 more, which the new version has nowhere to put. It stays on v2; plan again with a mapping or a decision for that work.',
    );
  });

  it('names the same things in both languages', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    const placeholders = (message: string) => [...message.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort();
    for (const key of Object.keys(english).filter((k) => k.startsWith('migration.passedOver.'))) {
      expect(placeholders(indonesian[key] ?? ''), `id fills in other things than English for "${key}"`).toEqual(
        placeholders(english[key]),
      );
    }
  });
});

/*
 * What the migration dialog says of an apply that was sent to a second
 * administrator, and of one that passed instances over. The rest of the dialog
 * is English still; these were written with their words in the catalogues.
 */
describe('the migration dialog’s own words', () => {
  const OWN = [
    'migration.passedOverAllTitle',
    'migration.passedOverInstance',
    'migration.passedOverListTitle',
    'migration.passedOverMore',
    'migration.passedOverSomeTitle',
    'migration.passedOverSummary',
    'migration.pendingAskedBy',
    'migration.pendingMessage',
    'migration.pendingTitle',
    'migration.pendingWhy',
    'migration.secondApproverMessage',
    'migration.secondApproverTitle',
    'migration.sendForApproval',
  ];

  it('are in both languages, every one, and not copied', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    for (const key of OWN) {
      expect(english[key], `en has no "${key}"`).toBeDefined();
      expect(indonesian[key], `id has no "${key}"`).toBeDefined();
      expect(indonesian[key], `id copies the English for "${key}"`).not.toBe(english[key]);
    }
  });

  it('are every migration key there is, beside the causes', async () => {
    // A key added for the dialog and left out of the list above would be in
    // one language only and pass. And none may begin "migration.passedOver.":
    // what follows that is a cause the server gives (tests/roledrift).
    const english = (await import('./catalogues/en')).default;
    const own = Object.keys(english).filter((key) => key.startsWith('migration.') && !key.startsWith('migration.passedOver.'));
    expect(own.sort()).toEqual([...OWN, 'migration.passedOverStepsMore'].sort());
  });

  it('fill in the same things in both languages', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    const placeholders = (message: string) => [...message.matchAll(/\{(\w+)[,}]/g)].map((match) => match[1]).sort();
    for (const key of OWN) {
      expect(placeholders(indonesian[key] ?? ''), `id fills in other things than English for "${key}"`).toEqual(
        placeholders(english[key] ?? ''),
      );
    }
    expect(placeholders(english['migration.passedOverSummary'])).toEqual(['count']);
    expect(placeholders(english['migration.pendingMessage'])).toEqual(['date']);
  });

  it('count in words that fit one and many', async () => {
    const english = (await import('./catalogues/en')).default;
    const indonesian = (await import('./catalogues/id')).default;
    expect(format(english, 'migration.passedOverSummary', { count: 1 })).toBe('1 instance was not moved. The list below says why.');
    expect(format(english, 'migration.passedOverSummary', { count: 12 })).toBe('12 instances were not moved. The list below says why.');
    expect(format(indonesian, 'migration.passedOverSummary', { count: 12 })).toBe(
      '12 instansi tidak dipindahkan. Daftar di bawah menjelaskan alasannya.',
    );
    expect(format(english, 'migration.passedOverMore', { count: 1 })).toBe('and 1 more instance');
    expect(format(english, 'migration.passedOverMore', { count: 140 })).toBe('and 140 more instances');
    expect(format(indonesian, 'migration.passedOverMore', { count: 140 })).toBe('dan 140 instansi lainnya');
  });
});
