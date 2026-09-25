import { describe, expect, it } from 'bun:test';

import { trialTarget } from './decisionTrial';

/**
 * Try it runs a stored table. It used to send the key typed into the editor,
 * with no version: an edited key named another table or none (saving does not
 * change a stored key), and with no version the server answered with the
 * newest table of that key, not necessarily the one on screen.
 */
describe('trialTarget', () => {
  it('runs the table the editor loaded, at the version it loaded', () => {
    expect(trialTarget({ key: 'discount', version: 3 })).toEqual({ key: 'discount', version: 3 });
  });

  it('runs nothing for a table that has never been saved', () => {
    expect(trialTarget(undefined)).toBeNull();
  });
});
