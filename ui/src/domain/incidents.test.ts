import { describe, expect, it } from 'bun:test';

import { explainIncident, incidentStep } from './incidents';

/**
 * The person reading an incident is usually an operations person, and the
 * record they are handed is an engineer's error — a wrapped Go string naming a
 * node and an HTTP status. They want to know two things: whose fault is this,
 * and can I just try again.
 */
describe('explainIncident', () => {
  it('separates "they are down" from "we sent something wrong"', () => {
    expect(explainIncident('HTTP error: 503 Service Unavailable (status 503)').worthRetrying).toBe(true);
    expect(explainIncident('HTTP error: 400 Bad Request (status 400)').worthRetrying).toBe(false);
  });

  it('names expired credentials as such, because retrying will not fix them', () => {
    const explained = explainIncident('HTTP error: 401 Unauthorized (status 401)');
    expect(explained.cause).toContain('credentials');
    expect(explained.worthRetrying).toBe(false);
  });

  it('recognises the engine holding back from a failing service', () => {
    const explained = explainIncident('service task "charge": not calling host:api.example.com, its circuit breaker is open');
    expect(explained.cause).toContain('failing repeatedly');
    expect(explained.worthRetrying).toBe(true);
  });

  it('recognises a timeout as temporary', () => {
    expect(explainIncident('Post "https://api": context deadline exceeded').worthRetrying).toBe(true);
  });

  it('explains a blocked address rather than leaving it as jargon', () => {
    const explained = explainIncident('httpclient: destination address is blocked by egress policy');
    expect(explained.cause).toContain('not allowed to call');
    expect(explained.worthRetrying).toBe(false);
  });

  it('explains a decision table that contradicts itself', () => {
    const explained = explainIncident('UNIQUE hit policy violated: lines [1 2] all matched');
    expect(explained.cause).toContain('contradicted itself');
    expect(explained.worthRetrying).toBe(false);
  });

  it('says something useful even for an error it has never seen', () => {
    const explained = explainIncident('something nobody anticipated');
    expect(explained.cause).not.toBe('');
    expect(explained.suggestion).not.toBe('');
    // Unknown errors are offered a retry: most transient failures are ones
    // nobody wrote a pattern for, and refusing the button by default would make
    // the common case unrecoverable.
    expect(explained.worthRetrying).toBe(true);
  });

  it('prefers the more specific pattern when two could match', () => {
    // A 500 inside a timeout message is still a timeout — the first pattern that
    // matches wins, and the ordering is the design.
    const explained = explainIncident('HTTP error: 502 Bad Gateway (status 502)');
    expect(explained.cause).toContain('502');
  });
});

describe('incidentStep', () => {
  it('prefers the step name a person gave it', () => {
    expect(incidentStep({ id: '1', error: '', status: 'open', created_at: '', node: { id: 'Task_1', name: 'Charge the card' } }))
      .toBe('Charge the card');
  });

  it('falls back to the identifier, then to something sayable', () => {
    expect(incidentStep({ id: '1', error: '', status: 'open', created_at: '', node: { id: 'Task_1' } })).toBe('Task_1');
    expect(incidentStep({ id: '1', error: '', status: 'open', created_at: '' })).toBe('a step');
  });
});
