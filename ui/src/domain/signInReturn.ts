/**
 * Where to go after signing in, and what to say on the way there.
 *
 * Sending someone to the sign-in page carries the address they were on, so
 * signing in puts them back on it instead of on the dashboard. That address
 * arrives in the query string, which anybody can write into a link, so it is
 * only followed when it is a path inside this app: a full URL, or one starting
 * "//" or "/\" that a browser reads as another host, is how a sign-in page
 * becomes a way to send people somewhere else after they have typed their
 * password.
 */

/** Pages a return address must not point back at. */
const NOT_A_DESTINATION = ['/login', '/setup'];

/** The in-app address to go to after signing in, or `/` when there is none to trust. */
export function signInReturnAddress(redirect: string | undefined | null): string {
  if (!redirect || !redirect.startsWith('/')) return '/';
  if (redirect.startsWith('//') || redirect.startsWith('/\\')) return '/';
  // eslint-disable-next-line no-control-regex -- control characters are the point
  if (/[\u0000-\u001f]/.test(redirect)) return '/';
  const path = redirect.split(/[?#]/, 1)[0];
  if (NOT_A_DESTINATION.some((page) => path === page || path.startsWith(`${page}/`))) return '/';
  return redirect;
}

/** Whether a sign-in from `pathname` would bring the person back to a sign-in page. */
export function isSignInPage(pathname: string): boolean {
  return NOT_A_DESTINATION.some((page) => pathname === page || pathname.startsWith(`${page}/`));
}

/** Why the sign-in page was opened, when it was not the person's own choice. */
export type SignInReason = 'expired';

/** What the sign-in page says first, for a reason it was sent to. */
export function signInNotice(reason: SignInReason | undefined): string | null {
  if (reason === 'expired') {
    return 'Your session has ended. Sign in again to pick up where you left off.';
  }
  return null;
}
