import type { QueryClient } from '@tanstack/react-query';

interface TokenStore {
  subscribe: (listener: (state: { token: string | null }, previous: { token: string | null }) => void) => () => void;
}

/**
 * Empties the query cache when the session ends.
 *
 * Query keys are not scoped to the person, and directory lists stay fresh for
 * minutes, so after one person signed out and another signed in in the same
 * tab the second was served the first one's organizations, definitions and
 * tasks from the cache — without a request that would have refused them.
 */
export function clearQueriesOnSignOut(store: TokenStore, queryClient: QueryClient): () => void {
  return store.subscribe((state, previous) => {
    if (previous.token && !state.token) {
      queryClient.clear();
    }
  });
}
