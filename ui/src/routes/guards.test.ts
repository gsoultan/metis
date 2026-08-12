import { describe, expect, it } from 'bun:test';
import { isRedirect, redirect } from '@tanstack/react-router';
import { rethrowRedirect } from './guards';

/**
 * The route guards each wrapped their check in try/catch and re-threw
 * redirects with `if (e instanceof Error && 'to' in e) throw e`.
 *
 * Both halves of that condition are false for what redirect() actually
 * returns, so it could never fire and every redirect was swallowed by the
 * catch that was supposed to pass it through. On /setup this meant a fully
 * configured system showed the setup wizard and stayed on it.
 *
 * These tests pin the shape of redirect() so the assumption cannot silently
 * rot again if the router changes it.
 */
describe('redirect detection', () => {
  const value = redirect({ to: '/login' });

  it('is not an Error, so instanceof checks miss it', () => {
    expect(value instanceof Error).toBe(false);
  });

  it('does not carry `to` as an own property', () => {
    expect('to' in (value as object)).toBe(false);
  });

  it('is identified by isRedirect', () => {
    expect(isRedirect(value)).toBe(true);
  });

  it('the old guard condition never matches a real redirect', () => {
    const oldCondition = value instanceof Error && 'to' in (value as object);
    expect(oldCondition).toBe(false);
  });
});

describe('rethrowRedirect', () => {
  it('re-throws a redirect so a surrounding catch cannot swallow it', () => {
    const value = redirect({ to: '/login' });
    expect(() => rethrowRedirect(value)).toThrow();
  });

  it('ignores ordinary errors, leaving them to the caller', () => {
    expect(() => rethrowRedirect(new Error('network down'))).not.toThrow();
  });

  it('ignores non-error values', () => {
    expect(() => rethrowRedirect(undefined)).not.toThrow();
    expect(() => rethrowRedirect('failed')).not.toThrow();
  });
});
