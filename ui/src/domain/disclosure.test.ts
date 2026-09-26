import { describe, expect, it } from 'bun:test';

import { advancedVisibility } from './disclosure';

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
