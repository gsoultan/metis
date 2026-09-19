import { describe, expect, it } from 'bun:test';

import { signInError } from './signInError';

describe('the message somebody who cannot sign in actually reads', () => {
  /*
   * The reason this exists. The alert was titled "Authentication Failed" and
   * its body repeated the server's wrapped error underneath it.
   */
  it('turns the wrapped credentials error into a sentence', () => {
    const { message, hint } = signInError('authentication failed: invalid credentials');
    expect(message).toBe('That username and password do not match.');
    expect(hint).toContain('caps lock');
  });

  it('says the server is unreachable rather than "Failed to fetch"', () => {
    expect(signInError('Failed to fetch').message).toBe('Cannot reach the Metis server.');
  });

  it('explains a throttled login instead of showing the limiter internals', () => {
    expect(signInError('rate limit exceeded').message).toBe('Too many sign-in attempts from here.');
  });

  it('matches regardless of the casing the server used', () => {
    expect(signInError('INVALID CREDENTIALS').message).toBe('That username and password do not match.');
  });
});

describe('what it refuses to invent', () => {
  /*
   * Replacing an unrecognised failure with "Something went wrong" is how a
   * diagnosable outage becomes an unreportable one. The detail survives.
   */
  it('passes an unrecognised message through rather than inventing one', () => {
    expect(signInError('tenant: could not resolve organisation').message)
      .toBe('Could not resolve organisation.');
  });

  it('keeps only the cause from a wrapped chain', () => {
    expect(signInError('authentication failed: user store: no rows').message).toBe('No rows.');
  });

  it('does not double the full stop', () => {
    expect(signInError('something broke.').message).toBe('Something broke.');
  });

  it('has a message even when the server sent nothing', () => {
    expect(signInError('').message).toBe('Could not sign you in.');
    expect(signInError(null).message).toBe('Could not sign you in.');
    expect(signInError(undefined).message).toBe('Could not sign you in.');
  });
});
