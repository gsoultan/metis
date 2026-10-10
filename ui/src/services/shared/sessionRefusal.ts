/**
 * What happens when the server stops accepting the session's token.
 *
 * A token expires, or is revoked when the password changes on another device.
 * Nothing in the clients looked at a 401, so the app stayed "signed in": every
 * request failed with a bare "Unauthorized", the event stream reconnected
 * forever, and the only way out was to find the sign-out item in a menu.
 *
 * The transports report a refusal here, with the token they sent; the app
 * decides what to do about it (see endSessionOnRefusal). The token is passed
 * so a refusal of a token that is no longer the session's — a slow request
 * sent before the person signed in again — does not sign out the new session.
 */

type RefusalListener = (tokenSent: string) => void;

let listener: RefusalListener | null = null;

/** Sets what a refusal does. Returns the function that removes it. */
export function onSessionRefused(next: RefusalListener): () => void {
  listener = next;
  return () => {
    if (listener === next) listener = null;
  };
}

/** Called by a transport when a request carrying `tokenSent` came back 401 / unauthenticated. */
export function reportSessionRefused(tokenSent: string): void {
  listener?.(tokenSent);
}

interface SessionStore {
  getState: () => { token: string | null; clearAuth: () => void };
}

/**
 * The listener the app installs: sign out, and go to the sign-in page — once.
 *
 * A dozen queries fail together when a token expires. The first refusal
 * clears the token; the rest find it already gone, or replaced by a new
 * sign-in, and do nothing.
 */
export function endSessionOnRefusal(store: SessionStore, goToSignIn: () => void): RefusalListener {
  return (tokenSent) => {
    const { token, clearAuth } = store.getState();
    if (!token || token !== tokenSent) {
      return;
    }
    clearAuth();
    goToSignIn();
  };
}
