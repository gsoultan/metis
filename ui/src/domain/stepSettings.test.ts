import { describe, expect, it } from 'bun:test';

import { describeTimer, indexVersion, settingDifferences, stepSettings } from './stepSettings';
import type { ApiNode } from '../services/types';

const step = (over: Partial<ApiNode> = {}): ApiNode => ({ id: 'send', name: 'Send', type: 'serviceTask', x: 0, y: 0, ...over });

const differences = (before: ApiNode, after: ApiNode) => {
  const version = indexVersion(null);
  return settingDifferences(stepSettings(before, version), stepSettings(after, version));
};

describe('describeTimer', () => {
  it('reads an ISO-8601 wait as a length of time', () => {
    expect(describeTimer('PT5M')).toBe('5 minutes');
    expect(describeTimer('PT1H')).toBe('1 hour');
    expect(describeTimer('P1DT12H')).toBe('1 day, 12 hours');
    expect(describeTimer('P2W')).toBe('2 weeks');
  });

  it('reads a repeating timer as how often and how many times', () => {
    expect(describeTimer('R3/PT1H')).toBe('every 1 hour, 3 times');
    expect(describeTimer('R/P1D')).toBe('every 1 day, with no end');
  });

  it('quotes what it cannot read rather than guessing', () => {
    expect(describeTimer('2026-01-01T12:00:00Z')).toBe('2026-01-01T12:00:00Z');
    expect(describeTimer('PT')).toBe('PT');
    expect(describeTimer('${reminderDelay}')).toBe('${reminderDelay}');
  });
});

describe('settingDifferences', () => {
  it('compares a setting it has no words for, under its own name', () => {
    // A connector step's own fields are not in any list here. Leaving them out
    // is how two versions that post to different channels read the same.
    expect(differences(step({ properties: { slack_channel: '#sales' } }), step({ properties: { slack_channel: '#finance' } })))
      .toEqual(['slack channel: #sales → #finance']);
  });

  it('never prints a value whose name says it is a secret', () => {
    expect(differences(step({ properties: { webhook_secret: 'old' } }), step({ properties: { webhook_secret: 'new' } })))
      .toEqual(['webhook secret changed']);
  });

  it('says a setting was added or removed when it cannot print it', () => {
    expect(differences(step(), step({ properties: { input_mapping: '{"amount":"total"}' } })))
      .toEqual(['data passed in added']);
    expect(differences(step({ script: 'x = 1' }), step())).toEqual(['script removed']);
  });

  it('reads an unset switch as off', () => {
    expect(differences(step({ type: 'boundaryEvent' }), step({ type: 'boundaryEvent', properties: { non_interrupting: true } })))
      .toEqual(['lets the step carry on: no → yes']);
  });

  it('calls a step’s kind by its plain name', () => {
    expect(differences(step({ type: 'userTask' }), step({ type: 'serviceTask' })))
      .toEqual(['kind of step: Ask a person → Call another system']);
  });
});
