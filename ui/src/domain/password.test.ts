import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import { MIN_PASSWORD_LENGTH } from './password';

describe('the password rule', () => {
  // The form tells the user the rule before they submit, which means the rule
  // is written down twice. Two copies drift: the server would start refusing
  // passwords the form had already called acceptable, and the user would get a
  // rejection with no field to blame it on.
  it('is the same length the server enforces', () => {
    const source = readFileSync(
      new URL('../../../server/domains/services/impl/user.go', import.meta.url),
      'utf8',
    );
    const match = source.match(/const MinPasswordLength = (\d+)/);

    expect(match).not.toBeNull();
    expect(MIN_PASSWORD_LENGTH).toBe(Number(match![1]));
  });
});
