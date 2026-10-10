import { describe, expect, it } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { create } from 'zustand';

import { clearQueriesOnSignOut } from './clearOnSignOut';

describe('signing out', () => {
  it('leaves nothing of the last person in the query cache', () => {
    const store = create<{ token: string | null }>(() => ({ token: 'a-token' }));
    const client = new QueryClient();
    client.setQueryData(['organizations'], [{ id: 'theirs' }]);

    const stop = clearQueriesOnSignOut(store, client);
    store.setState({ token: 'refreshed' });
    expect(client.getQueryData(['organizations'])).toBeDefined();

    store.setState({ token: null });
    expect(client.getQueryData(['organizations'])).toBeUndefined();
    stop();
  });
});
