import { createJSONStorage } from 'zustand/middleware';

/**
 * localStorage for a store whose key has been renamed.
 *
 * The product is Metis and its keys are `metis-` prefixed. They were `gobpm-`
 * prefixed, and simply renaming them would have discarded whatever every open
 * browser had already saved: the auth token lives in one of these, so everyone
 * signed in would have been silently signed out on the next deploy, along with
 * losing their selected project, theme and sidebar state.
 *
 * So the first read of a renamed key falls back to the old one and carries the
 * value across. The old key is removed once its value is safely under the new
 * name — a copy left behind would be restored again on the next browser, and
 * would quietly become a second source of truth.
 */
export function renamedStorage(legacyName: string) {
  return createJSONStorage(() => ({
    getItem: (name: string): string | null => {
      const current = localStorage.getItem(name);
      if (current !== null) return current;

      const legacy = localStorage.getItem(legacyName);
      if (legacy === null) return null;

      // Write before removing. If the browser dies between the two, the worst
      // case is the value existing under both names, which the next read
      // resolves — losing it is not recoverable.
      localStorage.setItem(name, legacy);
      localStorage.removeItem(legacyName);
      return legacy;
    },
    setItem: (name: string, value: string) => {
      localStorage.setItem(name, value);
    },
    removeItem: (name: string) => {
      localStorage.removeItem(name);
      localStorage.removeItem(legacyName);
    },
  }));
}
