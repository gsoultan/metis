import { describe, expect, it } from 'bun:test';

import { isSignInPage, signInNotice, signInReturnAddress } from './signInReturn';

describe('the address signing in returns to', () => {
  it('is the page the person was on, query and all', () => {
    expect(signInReturnAddress('/instances')).toBe('/instances');
    expect(signInReturnAddress('/designer?definitionId=abc#step-2')).toBe('/designer?definitionId=abc#step-2');
  });

  it('is the dashboard when there is none', () => {
    expect(signInReturnAddress(undefined)).toBe('/');
    expect(signInReturnAddress('')).toBe('/');
  });

  it('never leaves the app', () => {
    for (const elsewhere of [
      'https://example.com/',
      '//example.com/path',
      '/\\example.com',
      'javascript:alert(1)',
      'instances',
      '/\nexample',
    ]) {
      expect(signInReturnAddress(elsewhere)).toBe('/');
    }
  });

  it('never goes back to the sign-in or setup page', () => {
    expect(signInReturnAddress('/login?redirect=/tasks')).toBe('/');
    expect(signInReturnAddress('/setup')).toBe('/');
    expect(signInReturnAddress('/loginsomething')).toBe('/loginsomething');
  });
});

describe('the sign-in page', () => {
  it('is recognised so a refusal there does not send the person to it again', () => {
    expect(isSignInPage('/login')).toBe(true);
    expect(isSignInPage('/setup')).toBe(true);
    expect(isSignInPage('/tasks')).toBe(false);
  });

  it('says why when the session ended', () => {
    expect(signInNotice('expired')).toContain('session has ended');
    expect(signInNotice(undefined)).toBeNull();
  });
});
