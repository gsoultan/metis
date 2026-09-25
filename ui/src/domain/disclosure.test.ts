import { describe, expect, it } from 'bun:test';

import { advancedVisibility, implementationOptions } from './disclosure';

/**
 * Basic mode may hide an advanced setting only while it is empty.
 *
 * It used to hide them outright. A service task set to run a script showed a
 * blank "What it calls" and no script, and a user task pointing at an outside
 * form showed no sign of it. The step still did those things; the person
 * looking at it could not tell.
 */
describe('advancedVisibility', () => {
  const UNSET: Array<[string, unknown]> = [
    ['undefined', undefined],
    ['null', null],
    ['an empty string', ''],
    ['zero', 0],
    ['NaN', Number.NaN],
    ['false', false],
    ['an empty list', []],
    ['an empty object', {}],
  ];

  const SET: Array<[string, unknown]> = [
    ['a form key', 'invoice-form'],
    // Still a value: the task carries it, and the inbox shows it.
    ['a string of spaces', '  '],
    ['a script', 'setVar("total", 1)'],
    ['a number', 3],
    ['a negative number', -1],
    ['true', true],
    ['a list', ['finance']],
    ['an object', { amount: 'total' }],
  ];

  it.each(SET)('summarises %s in basic mode rather than hiding it', (_name, value) => {
    expect(advancedVisibility(false, value)).toBe('summary');
  });

  it.each(UNSET)('hides %s in basic mode', (_name, value) => {
    expect(advancedVisibility(false, value)).toBe('hidden');
  });

  it.each([...SET, ...UNSET])('lets expert mode edit %s', (_name, value) => {
    expect(advancedVisibility(true, value)).toBe('edit');
  });
});

describe('implementationOptions', () => {
  const values = (expert: boolean, current: string) =>
    implementationOptions(expert, current).map((option) => option.value);

  it('offers basic mode the three ways that need no code', () => {
    expect(values(false, 'push')).toEqual(['push', 'connector', 'external']);
  });

  it('adds running a script in expert mode', () => {
    expect(values(true, 'push')).toEqual(['push', 'connector', 'external', 'script']);
  });

  it('keeps a script step readable in basic mode, under its usual name', () => {
    // Without it the select had no option for the step's value and drew an
    // empty box, as if the step called nothing.
    const script = implementationOptions(false, 'script').find((option) => option.value === 'script');
    const expertScript = implementationOptions(true, 'script').find((option) => option.value === 'script');

    expect(script).toBeDefined();
    expect(script).toEqual(expertScript);
  });

  it('keeps a value this editor does not offer, so it still shows', () => {
    // An imported file, or one saved by a later version, can name a way this
    // list does not know. It is shown as it is rather than as nothing.
    const unknown = implementationOptions(false, 'soap').find((option) => option.value === 'soap');

    expect(unknown?.label).toBe('soap');
    expect(unknown?.description).toBeTruthy();
    expect(values(true, 'soap')).toContain('soap');
  });

  it('always includes the current value, in either mode', () => {
    for (const expert of [false, true]) {
      for (const current of ['push', 'connector', 'external', 'script', 'soap']) {
        expect(values(expert, current), `${current}, expert ${expert}`).toContain(current);
      }
    }
  });

  it('lists each way once', () => {
    for (const expert of [false, true]) {
      for (const current of ['push', 'script', 'soap']) {
        const listed = values(expert, current);
        expect(new Set(listed).size, `${current}, expert ${expert}`).toBe(listed.length);
      }
    }
  });

  it('adds nothing for an empty value', () => {
    expect(values(false, '')).toEqual(['push', 'connector', 'external']);
  });

  it('describes every way it offers', () => {
    for (const option of implementationOptions(true, 'soap')) {
      expect(option.label, option.value).toBeTruthy();
      expect(option.description, option.value).toBeTruthy();
    }
  });
});
