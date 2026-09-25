import { describe, expect, it } from 'bun:test';

import { canChooseImplementation, implementationOptions } from './implementationOptions';

describe('implementationOptions', () => {
  const values = (expert: boolean, current: string) =>
    implementationOptions(expert, current).map((option) => option.value);

  it('offers basic mode the three ways that need no code', () => {
    expect(values(false, 'push')).toEqual(['push', 'connector', 'external']);
  });

  it('adds running a script in expert mode', () => {
    expect(values(true, 'push')).toEqual(['push', 'connector', 'external', 'script']);
  });

  it('keeps a script step readable in basic mode, under its usual name', () => {
    // Without it the select had no option for the step's value and drew an
    // empty box, as if the step called nothing.
    const script = implementationOptions(false, 'script').find((option) => option.value === 'script');
    const expertScript = implementationOptions(true, 'script').find((option) => option.value === 'script');

    expect(script).toBeDefined();
    expect(script).toEqual(expertScript);
  });

  it('keeps a value this editor does not offer, so it still shows', () => {
    // An imported file, or one saved by a later version, can name a way this
    // list does not know. It is shown as it is rather than as nothing.
    const unknown = implementationOptions(false, 'soap').find((option) => option.value === 'soap');

    expect(unknown?.label).toBe('soap');
    expect(unknown?.description).toBeTruthy();
    expect(values(true, 'soap')).toContain('soap');
  });

  it('always includes the current value, in either mode', () => {
    for (const expert of [false, true]) {
      for (const current of ['push', 'connector', 'external', 'script', 'soap']) {
        expect(values(expert, current), `${current}, expert ${expert}`).toContain(current);
      }
    }
  });

  it('lists each way once', () => {
    for (const expert of [false, true]) {
      for (const current of ['push', 'script', 'soap']) {
        const listed = values(expert, current);
        expect(new Set(listed).size, `${current}, expert ${expert}`).toBe(listed.length);
      }
    }
  });

  it('adds nothing for an empty value', () => {
    expect(values(false, '')).toEqual(['push', 'connector', 'external']);
  });

  it('lets basic mode change only a way it offers', () => {
    expect(canChooseImplementation(false, 'push')).toBe(true);
    expect(canChooseImplementation(false, 'script')).toBe(false);
    expect(canChooseImplementation(false, 'soap')).toBe(false);
    expect(canChooseImplementation(true, 'script')).toBe(true);
    expect(canChooseImplementation(true, 'soap')).toBe(true);
  });

  it('describes every way it offers', () => {
    for (const option of implementationOptions(true, 'soap')) {
      expect(option.label, option.value).toBeTruthy();
      expect(option.description, option.value).toBeTruthy();
    }
  });
});
