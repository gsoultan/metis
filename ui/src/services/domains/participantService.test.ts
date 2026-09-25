import { afterEach, beforeEach, describe, expect, it } from 'bun:test';

import { participantService } from './participantService';

type GlobalWithStorage = typeof globalThis & { localStorage: Pick<Storage, 'getItem'> };

/*
 * Whether a project has anybody in its directory is a question about one row.
 * The list used to be asked for whole, however many thousand people a
 * directory import brought in, every time Help was opened.
 */
describe('listing a project’s participants', () => {
  const originalFetch = globalThis.fetch;
  const originalLocalStorage = (globalThis as GlobalWithStorage).localStorage;
  let requested: string[];

  beforeEach(() => {
    requested = [];
    (globalThis as GlobalWithStorage).localStorage = {
      getItem: () => JSON.stringify({ state: { token: 'token-123' } }),
    };
    globalThis.fetch = (async (input) => {
      requested.push(String(input));
      return new Response(JSON.stringify({ participants: [{ id: 'p1', username: 'ada' }] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    (globalThis as GlobalWithStorage).localStorage = originalLocalStorage;
  });

  it('asks for at most as many as it is told to', async () => {
    await participantService.listParticipants('project-1', undefined, { limit: 1 });
    expect(requested).toHaveLength(1);
    expect(new URL(requested[0], 'http://metis.test').searchParams.get('limit')).toBe('1');
  });

  it('asks for the whole directory when not told otherwise, as the People page does', async () => {
    await participantService.listParticipants('project-1');
    expect(new URL(requested[0], 'http://metis.test').searchParams.has('limit')).toBe(false);
  });
});
