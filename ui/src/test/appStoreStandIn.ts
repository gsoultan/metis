/**
 * The app store, stood in for while a test renders.
 *
 * A server render reads a zustand store's initial state (its server snapshot),
 * never what a test sets on it, so a test that renders to markup cannot sign
 * somebody in by setting state. The module is replaced instead, with a hook
 * that reads whatever the test sets, and put back when the test file is done:
 * bun's module mocks otherwise outlive the file that made them and reach the
 * next one.
 *
 * `mockModule` is bun:test's `mock.module`, passed in so this file does not
 * import bun:test and can be typechecked with the app.
 */

import { useAppStore } from '../store/useAppStore';

export type AppState = ReturnType<typeof useAppStore.getState>;
export type SignedInUser = NonNullable<AppState['user']>;

type ModuleMocker = (specifier: string, factory: () => Record<string, unknown>) => unknown;

export interface AppStoreStandIn {
  /** What every component reads from the store until the next call. */
  set: (state: Partial<AppState>) => void;
  /** Puts the real store back. Call it from afterAll. */
  restore: () => void;
}

const STORE_MODULE = new URL('../store/useAppStore.ts', import.meta.url).pathname;

export function standInForAppStore(mockModule: ModuleMocker): AppStoreStandIn {
  const realUseAppStore = useAppStore;
  let current: Partial<AppState> = {};
  const read = () => current as AppState;
  const useStandIn = (selector?: (state: AppState) => unknown) => (selector ? selector(read()) : read());

  mockModule(STORE_MODULE, () => ({ useAppStore: Object.assign(useStandIn, { getState: read }) }));
  return {
    set: (state) => {
      current = state;
    },
    restore: () => {
      mockModule(STORE_MODULE, () => ({ useAppStore: realUseAppStore }));
    },
  };
}

/** Somebody signed in with these roles. */
export function userWithRoles(roles: string[]): SignedInUser {
  return {
    id: 'user-1',
    name: 'Ana',
    displayName: 'Ana',
    organization: 'Acme',
    username: 'ana',
    role: roles.join(','),
    roles,
  };
}
