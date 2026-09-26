import { describe, expect, it } from 'bun:test';

import { legacySigningOf, V2_SIGNING } from './webhookSigning';

const now = new Date('2026-09-26T12:00:00Z');

function inHours(hours: number): string {
  return new Date(now.getTime() + hours * 3_600_000).toISOString();
}

/**
 * A legacy signature covers the body alone, so a delivery signed that way can
 * be replayed. Webhooks from before v2 accept it until a deadline; the screen
 * has to say which ones, and until when, while there is still time to move
 * their senders.
 */
describe('legacySigningOf', () => {
  it('says nothing about a webhook that accepts v2 only', () => {
    expect(legacySigningOf({}, now)).toEqual({ state: 'v2-only' });
    expect(legacySigningOf({ legacy_signatures_until: undefined }, now)).toEqual({ state: 'v2-only' });
  });

  it('treats a window that has closed as v2 only, because the server does', () => {
    expect(legacySigningOf({ legacy_signatures_until: inHours(-1) }, now)).toEqual({ state: 'v2-only' });
    expect(legacySigningOf({ legacy_signatures_until: now.toISOString() }, now)).toEqual({ state: 'v2-only' });
  });

  it('says until when an open window lasts', () => {
    const until = inHours(90 * 24);
    const signing = legacySigningOf({ legacy_signatures_until: until }, now);
    expect(signing.state).toBe('accepting');
    if (signing.state !== 'accepting') return;
    expect(signing.until.toISOString()).toBe(until);
    expect(signing.remaining).toBe('90 days left');
    expect(signing.urgent).toBe(false);
  });

  it('speaks in units a person uses as the deadline gets close', () => {
    const remaining = (hours: number) => {
      const signing = legacySigningOf({ legacy_signatures_until: inHours(hours) }, now);
      return signing.state === 'accepting' ? signing.remaining : signing.state;
    };
    expect(remaining(24 * 30 + 5)).toBe('30 days left');
    expect(remaining(24 + 1)).toBe('1 day left');
    expect(remaining(5.5)).toBe('5 hours left');
    expect(remaining(1)).toBe('1 hour left');
    expect(remaining(0.25)).toBe('less than an hour left');
  });

  it('marks the last two weeks as urgent', () => {
    const signing = (hours: number) => legacySigningOf({ legacy_signatures_until: inHours(hours) }, now);
    expect(signing(24 * 14 + 1)).toMatchObject({ urgent: false });
    expect(signing(24 * 14 - 1)).toMatchObject({ urgent: true });
  });

  it('survives a deadline nobody can read by claiming nothing', () => {
    expect(legacySigningOf({ legacy_signatures_until: 'next quarter' }, now)).toEqual({ state: 'v2-only' });
  });
});

/**
 * The help a sender is given has to match what the server checks, header for
 * header — a sender told the wrong name has every delivery refused.
 */
describe('V2_SIGNING', () => {
  it('names the headers the server reads', () => {
    expect(V2_SIGNING.timestampHeader).toBe('X-Metis-Timestamp');
    expect(V2_SIGNING.deliveryIdHeader).toBe('X-Delivery-Id');
    expect(V2_SIGNING.signatureHeader).toBe('X-Metis-Signature');
  });

  it('describes the signed string and the tolerance the server applies', () => {
    expect(V2_SIGNING.signedString).toBe('<timestamp>.<delivery id>.<raw body>');
    expect(V2_SIGNING.prefix).toBe('v2=');
    expect(V2_SIGNING.toleranceMinutes).toBe(5);
  });
});
