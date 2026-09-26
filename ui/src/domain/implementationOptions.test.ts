import { describe, expect, it } from 'bun:test';

import { canChooseImplementation, implementationOptions } from './implementationOptions';

describe('implementationOptions', () => {
  const values = (current: string) => implementationOptions(current).map((option) => option.value);

  it('offers the three ways a step that calls another system can work', () => {
    expect(values('push')).toEqual(['push', 'connector', 'external']);
  });

  it('offers no way to run a script, which this kind of step cannot do', () => {
    // The engine runs a script only on a script step. "Run a script here" on
    // this one was skipped as if it called nothing, and deploy refuses it now.
    expect(values('push')).not.toContain('script');
  });

  it('shows a step already set to run a script as something it cannot do', () => {
    const script = implementationOptions('script').find((option) => option.value === 'script');

    expect(script?.label).toContain('cannot');
    expect(script?.description).toContain('script step');
  });

  it('keeps a value this editor does not offer, so it still shows', () => {
    // An imported file, or one saved by a later version, can name a way this
    // list does not know. It is shown as it is rather than as nothing.
    const unknown = implementationOptions('soap').find((option) => option.value === 'soap');

    expect(unknown?.label).toBe('soap');
    expect(unknown?.description).toBeTruthy();
  });

  it('always includes the current value', () => {
    for (const current of ['push', 'connector', 'external', 'script', 'soap']) {
      expect(values(current), current).toContain(current);
    }
  });

  it('lists each way once', () => {
    for (const current of ['push', 'script', 'soap']) {
      const listed = values(current);
      expect(new Set(listed).size, current).toBe(listed.length);
    }
  });

  it('adds nothing for an empty value', () => {
    expect(values('')).toEqual(['push', 'connector', 'external']);
  });

  it('lets basic mode change a way it offers, and move a step off running a script', () => {
    expect(canChooseImplementation(false, 'push')).toBe(true);
    // A script cannot run here, so leaving it is the fix, in either mode.
    expect(canChooseImplementation(false, 'script')).toBe(true);
    expect(canChooseImplementation(false, 'soap')).toBe(false);
    expect(canChooseImplementation(true, 'script')).toBe(true);
    expect(canChooseImplementation(true, 'soap')).toBe(true);
  });

  it('describes every way it offers', () => {
    for (const option of implementationOptions('soap')) {
      expect(option.label, option.value).toBeTruthy();
      expect(option.description, option.value).toBeTruthy();
    }
  });
});
