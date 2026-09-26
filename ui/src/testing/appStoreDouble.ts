/**
 * The app store, for tests that render a component.
 *
 * Rendering on the server reads a zustand store's initial state rather than its
 * current one, so the real store would render every component in basic mode
 * whatever a test set. It also persists to localStorage, which bun does not
 * have. A test swaps the store module for this one:
 *
 *   mock.module('../../store/useAppStore', () => ({ useAppStore: appStoreDouble }));
 *
 * and imports the component under test afterwards. A module mock stays in
 * place for the rest of the run, so later test files see this store as well.
 * It is made from the real store's own definition, with the same fields,
 * defaults and actions, and resetAppStore() puts it back to those defaults.
 */
import { createStore } from 'zustand/vanilla';

import { appState, type AppState } from '../store/useAppStore';

const store = createStore<AppState>()(appState);
const defaults = store.getState();

function select<T>(selector: (state: AppState) => T): T {
  return selector(store.getState());
}

/** Reads the current state, whole or through a selector, as useAppStore does. */
export const appStoreDouble = Object.assign(
  (selector?: (state: AppState) => unknown) => (selector ? select(selector) : store.getState()),
  {
    getState: store.getState,
    setState: store.setState,
    subscribe: store.subscribe,
    getInitialState: store.getInitialState,
  },
);

/** Sets some fields for the next render. */
export function setAppState(partial: Partial<AppState>): void {
  store.setState(partial);
}

/** Every field back to the real store's default. */
export function resetAppStore(): void {
  store.setState(defaults, true);
}
