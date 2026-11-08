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
 *
 * A request can also be refused for carrying no token at all while this tab
 * still believes it is signed in: the person signed out, or was signed out, in
 * another tab, which empties the storage every tab reads the token from but
 * not this tab's copy of the store. Every page then answered "Unauthorized"
 * and stayed put. That is reported too, with `null` for the token.
 */

type RefusalListener = (tokenSent: string | null) => void;

let listener: RefusalListener | null = null;

/** Sets what a refusal does. Returns the function that removes it. */
export function onSessionRefused(next: RefusalListener): () => void {
  listener = next;
  return () => {
    if (listener === next) listener = null;
  };
}

/**
 * Called by a transport when a request came back 401 / unauthenticated, with
 * the token it carried, or null when it carried none.
 */
export function reportSessionRefused(tokenSent: string | null): void {
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
 *
 * A refusal of a request that carried no token ends the session only when this
 * tab still holds one: the token was taken out of storage under it. With none
 * held there is no session to end — the request belonged to a page that does
 * not need one, such as a People page, which signs in on its own.
 */
export function endSessionOnRefusal(store: SessionStore, goToSignIn: () => void): RefusalListener {
  return (tokenSent) => {
    const { token, clearAuth } = store.getState();
    if (!token || (tokenSent !== null && token !== tokenSent)) {
      return;
    }
    clearAuth();
    goToSignIn();
  };
}
