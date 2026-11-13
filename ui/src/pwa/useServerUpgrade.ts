/**
 * Notices when the server under this page has been upgraded.
 *
 * Asks the liveness probe which build is serving every minute while the tab is
 * visible, again when it comes back into view or back online, and compares
 * the answer with the first one. On a change it asks the browser to fetch the
 * new service worker straight away rather than at the next visit, and from
 * then on reloads at the next move to another page — the one moment a reload
 * cannot cost anybody what they were typing.
 */
import { useQuery } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { useEffect, useState } from 'react';

import { compareServerBuild, reloadOnNavigation, type ServerChange } from '../domain/serverUpgrade';
import { buildService } from '../services/domains/buildService';

/**
 * How often an open page asks. The probe is unauthenticated and answers from
 * memory; a minute means a tab finds out within a minute of the server coming
 * back, without a poll anybody would notice in the access log.
 */
const POLL_MS = 60_000;

export function useServerUpgrade(): ServerChange {
  const router = useRouter();
  const [loadedWith, setLoadedWith] = useState<string | null>(null);

  const { data } = useQuery({
    queryKey: ['build-version', 'watch'],
    queryFn: ({ signal }) => buildService.getBuildVersion(signal),
    refetchInterval: POLL_MS,
    // A hidden tab asks when it is looked at again, not every minute.
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
    refetchOnReconnect: 'always',
    // A failure is what a server stopped for its upgrade looks like. The next
    // poll is the retry; retrying sooner only adds requests to a server that
    // is starting.
    retry: false,
  });

  const current = data ?? null;
  // The first answer is the build this bundle came from. Stored in state, set
  // once, rather than read again, so a later answer is compared with it.
  if (loadedWith === null && current !== null) {
    setLoadedWith(current);
  }
  const change = compareServerBuild(loadedWith, current);

  // Fetch the new worker now, so the reload finds it installed. Once per
  // version: the effect re-runs only when the reported build changes.
  const upgradedTo = change.kind === 'upgraded' ? change.to : null;
  useEffect(() => {
    if (upgradedTo === null || typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
      return;
    }
    void navigator.serviceWorker
      .getRegistration()
      .then((registration) => registration?.update())
      .catch(() => {
        // No worker, or the browser refused: the reload still loads the new
        // shell from the network, which names the new bundle.
      });
  }, [upgradedTo]);

  // Subscribed only once the server has changed, so an ordinary navigation
  // pays nothing; replaced if it changes again.
  useEffect(() => {
    const upgraded = upgradedTo !== null;
    if (!upgraded) {
      return;
    }
    return router.subscribe('onResolved', (event) => {
      if (reloadOnNavigation(upgraded, event.fromLocation?.pathname, event.toLocation.pathname)) {
        window.location.reload();
      }
    });
  }, [router, upgradedTo]);

  return change;
}
