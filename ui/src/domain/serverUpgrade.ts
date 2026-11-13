/**
 * Whether the server under an open page has been upgraded.
 *
 * The bundle is embedded in the binary, so a page loaded before an upgrade is
 * running the old interface against the new server. Nothing reloads it: the
 * service worker only looks for a new version when the page is opened, and a
 * tab left open on an inbox stays on the old bundle for days. The page keeps
 * asking the liveness probe which build is serving it, and this compares the
 * answer with the one it was loaded with.
 *
 * Kept free of React and of the network so the rules can be tested alone.
 */
import { readBuildIdentity } from './buildVersion';

export type ServerChange =
  | { kind: 'same' }
  | {
      kind: 'upgraded';
      /** Exactly what the server now reports. */
      to: string;
      /** The same, as the sidebar shows a build. */
      label: string;
    };

const SAME: ServerChange = { kind: 'same' };

/**
 * Compares the build the page was loaded with and the one serving it now.
 *
 * Either side missing means "nothing to compare", never "changed": the first
 * answer has not arrived yet, or the probe failed — which is what a server
 * that is stopped for its upgrade looks like, and is not a new version.
 */
export function compareServerBuild(loadedWith: string | null, current: string | null): ServerChange {
  if (loadedWith === null || current === null) {
    return SAME;
  }
  const before = loadedWith.trim();
  const now = current.trim();
  if (before === '' || now === '' || before === now) {
    return SAME;
  }
  return { kind: 'upgraded', to: now, label: readBuildIdentity(now).label };
}

/**
 * Whether a navigation is a safe moment to reload into the new interface.
 *
 * Moving to another page already leaves whatever was typed on the old one, so
 * reloading there loses nothing the navigation was not going to lose. A change
 * of search or hash alone stays on the same page — a tab, a filter, a dialog
 * opened by URL — and may still have a half-filled form on it, so it does not.
 */
export function reloadOnNavigation(upgraded: boolean, fromPath: string | undefined, toPath: string): boolean {
  return upgraded && fromPath !== undefined && fromPath !== toPath;
}
