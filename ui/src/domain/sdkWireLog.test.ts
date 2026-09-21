import { describe, expect, it } from 'bun:test';
import { appendCall, callTone, formatLatency, wireLogAsText, type WireCall } from './sdkWireLog';

function call(partial: Partial<WireCall> = {}): WireCall {
  return {
    id: 'c1',
    label: 'Start refund',
    method: 'POST',
    path: '/process/start',
    status: 200,
    durationMs: 12,
    at: Date.UTC(2026, 8, 21, 10, 0, 0),
    ...partial,
  };
}

describe('appendCall', () => {
  it('puts the newest call first, which is the one being looked at', () => {
    const log = appendCall([call({ id: 'old' })], call({ id: 'new' }));
    expect(log.map((c) => c.id)).toEqual(['new', 'old']);
  });

  it('drops the oldest past the cap, so an afternoon of clicking stays bounded', () => {
    let log: WireCall[] = [];
    for (let i = 0; i < 10; i++) {
      log = appendCall(log, call({ id: `c${i}` }), 3);
    }
    expect(log.map((c) => c.id)).toEqual(['c9', 'c8', 'c7']);
  });

  it('keeps at least one call even if asked for none', () => {
    expect(appendCall([], call(), 0)).toHaveLength(1);
  });
});

describe('callTone', () => {
  it('separates a refusal from an outage, because they need different reactions', () => {
    expect(callTone(call({ status: 200 }))).toBe('ok');
    expect(callTone(call({ status: 204 }))).toBe('ok');
    expect(callTone(call({ status: 400 }))).toBe('refused');
    expect(callTone(call({ status: 409 }))).toBe('refused');
    expect(callTone(call({ status: 500 }))).toBe('failed');
  });

  it('treats a request that never arrived as a failure', () => {
    expect(callTone(call({ status: null }))).toBe('failed');
  });
});

describe('formatLatency', () => {
  it('reads at the precision a person can act on', () => {
    expect(formatLatency(0.4)).toBe('<1 ms');
    expect(formatLatency(12.4)).toBe('12 ms');
    expect(formatLatency(1500)).toBe('1.50 s');
    expect(formatLatency(65_000)).toBe('65.0 s');
  });
});

describe('wireLogAsText', () => {
  it('reads forwards, unlike the screen, because a transcript is read in order', () => {
    const log = appendCall([call({ id: 'first', label: 'One' })], call({ id: 'second', label: 'Two' }));
    const text = wireLogAsText(log);
    expect(text.indexOf('One')).toBeLessThan(text.indexOf('Two'));
  });

  it('says so plainly when nothing has been sent', () => {
    expect(wireLogAsText([])).toBe('No calls yet.');
  });

  it('includes the bodies, which is the whole reason to paste it anywhere', () => {
    const text = wireLogAsText([call({ requestBody: { a: 1 }, responseBody: { b: 2 }, error: 'nope' })]);
    expect(text).toContain('request:  {"a":1}');
    expect(text).toContain('response: {"b":2}');
    expect(text).toContain('error:    nope');
  });
});
