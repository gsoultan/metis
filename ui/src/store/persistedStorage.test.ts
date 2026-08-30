import { beforeEach, describe, expect, test } from 'bun:test';
import { renamedStorage } from './persistedStorage';

/**
 * The auth token lives in one of these keys, so getting this wrong signs every
 * open browser out on the next deploy — and loses their project, theme and
 * sidebar state with it.
 */

// A minimal localStorage, since bun's test environment has no DOM.
function installLocalStorage(): Record<string, string> {
  const data: Record<string, string> = {};
  (globalThis as { localStorage?: unknown }).localStorage = {
    getItem: (k: string) => (k in data ? data[k] : null),
    setItem: (k: string, v: string) => { data[k] = v; },
    removeItem: (k: string) => { delete data[k]; },
  };
  return data;
}

let store: Record<string, string>;
beforeEach(() => { store = installLocalStorage(); });

const state = JSON.stringify({ state: { token: 'a-real-session' }, version: 0 });

describe('renamedStorage', () => {
  test('carries a value saved under the old key across to the new one', () => {
    store['gobpm-app-storage'] = state;
    const storage = renamedStorage('gobpm-app-storage')!;

    expect(storage.getItem('metis-app-storage')).toEqual(JSON.parse(state));
  });

  test('removes the old key once the value is safely under the new one', () => {
    store['gobpm-app-storage'] = state;
    renamedStorage('gobpm-app-storage')!.getItem('metis-app-storage');

    // A copy left behind becomes a second source of truth: it would be
    // restored again later and silently overwrite newer state.
    expect(store['metis-app-storage']).toBe(state);
    expect('gobpm-app-storage' in store).toBe(false);
  });

  test('prefers the new key when both exist', () => {
    store['gobpm-app-storage'] = JSON.stringify({ state: { token: 'stale' }, version: 0 });
    store['metis-app-storage'] = state;

    expect(renamedStorage('gobpm-app-storage')!.getItem('metis-app-storage')).toEqual(JSON.parse(state));
  });

  test('a first-time visitor gets nothing rather than an error', () => {
    expect(renamedStorage('gobpm-app-storage')!.getItem('metis-app-storage')).toBeNull();
  });

  test('writes only ever go to the new key', () => {
    const storage = renamedStorage('gobpm-app-storage')!;
    storage.setItem('metis-app-storage', JSON.parse(state));

    expect(store['metis-app-storage']).toBeDefined();
    expect('gobpm-app-storage' in store).toBe(false);
  });

  test('signing out clears both, so no stale session can be restored', () => {
    store['gobpm-app-storage'] = state;
    store['metis-app-storage'] = state;

    renamedStorage('gobpm-app-storage')!.removeItem('metis-app-storage');

    expect('metis-app-storage' in store).toBe(false);
    expect('gobpm-app-storage' in store).toBe(false);
  });
});
