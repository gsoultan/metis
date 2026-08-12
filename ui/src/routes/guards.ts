import { isRedirect, redirect } from '@tanstack/react-router';
import { processService } from '../services/api';

/**
 * Shared route-guard helpers.
 *
 * Two bugs lived in the copy-pasted version of this logic across three routes.
 *
 * 1. Redirects were swallowed by their own catch block.
 *
 *    Every guard wrapped its check in try/catch and re-threw redirects with:
 *
 *        if (e instanceof Error && 'to' in e) throw e
 *
 *    TanStack Router's redirect() returns neither: it is not an Error, and `to`
 *    lives nested inside it rather than on the object. The condition is false
 *    on both counts, so it could never fire.
 *
 *    On /setup that meant `redirect({ to: '/login' })` was thrown, caught, and
 *    quietly dropped — so a fully configured system showed the setup wizard and
 *    stayed there. isRedirect() is the supported check.
 *
 * 2. A server that was not answering yet counted as "not configured".
 *
 *    The catch treated any failure as unconfigured and sent the user to /setup.
 *    In development the Vite server is ready in about a quarter of a second
 *    while `go run` takes several to compile and boot, so the first request
 *    reliably failed and every dev session opened on the setup wizard.
 *
 *    "The server did not answer" and "the system has no configuration" are
 *    different conditions and must not share a branch. The status check now
 *    retries briefly, and a genuine outage is surfaced rather than being
 *    reported to the user as a fresh install waiting to be set up.
 */

/** Re-throws a redirect so it is not swallowed by a surrounding catch. */
export function rethrowRedirect(error: unknown): void {
  if (isRedirect(error)) {
    throw error;
  }
}

interface SetupState {
  initialized: boolean;
  /** True when the server could not be reached at all. */
  unreachable: boolean;
}

const RETRIES = 3;
const RETRY_DELAY_MS = 400;

/**
 * Reads setup status, retrying briefly so a backend that is still starting is
 * not mistaken for an unconfigured one.
 */
export async function readSetupState(): Promise<SetupState> {
  for (let attempt = 0; attempt < RETRIES; attempt++) {
    try {
      const { status } = await processService.getSetupStatus();
      return { initialized: Boolean(status?.is_initialized), unreachable: false };
    } catch (error) {
      // A redirect thrown by something further in must not be retried.
      rethrowRedirect(error);
      if (attempt < RETRIES - 1) {
        await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY_MS * (attempt + 1)));
      }
    }
  }
  return { initialized: false, unreachable: true };
}

/**
 * Guard for routes that require a configured system.
 *
 * An unreachable server does not redirect: sending the user to /setup would
 * invite them to reconfigure a system that is merely restarting. The route
 * renders and its own error state explains the outage.
 */
export async function requireConfigured(): Promise<void> {
  const { initialized, unreachable } = await readSetupState();
  if (unreachable) return;
  if (!initialized) {
    throw redirect({ to: '/setup' });
  }
}

/**
 * Guard for /setup itself: leave if the system is already configured.
 */
export async function requireUnconfigured(): Promise<void> {
  const { initialized, unreachable } = await readSetupState();
  if (unreachable) return;
  if (initialized) {
    throw redirect({ to: '/login' });
  }
}
