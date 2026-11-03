import { describe, expect, test } from 'bun:test';
import { organizationInitials, organizationSummary } from './organizationSummary';

describe('organizationInitials', () => {
  test('two words give their first letters', () => {
    expect(organizationInitials('Acme Logistics')).toBe('AL');
  });
  test('one word gives its first two letters', () => {
    expect(organizationInitials('globex')).toBe('GL');
  });
  test('a blank name still gives something to show', () => {
    expect(organizationInitials('   ')).toBe('?');
  });
});

describe('organizationSummary', () => {
  test('counts people and projects and names the roles held, once each', () => {
    expect(organizationSummary(12, 1, ['ADMIN', 'admin'])).toBe('12 members · 1 project · you are Administrator');
  });
  test('a count not yet read is left out rather than shown as zero', () => {
    expect(organizationSummary(undefined, 3, [])).toBe('3 projects · you hold no role here');
  });
});
