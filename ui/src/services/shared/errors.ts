/**
 * Helpers for turning an unknown thrown value into something a person can act
 * on.
 *
 * `catch` bindings are `unknown`, not `Error`, so every call site was either
 * typing them `any` or discarding them. Discarding is the worse of the two: a
 * notification reading "Failed to save organization" tells the user nothing
 * about whether they should retry, fix a field, or call an administrator.
 */

const DEFAULT_FALLBACK = 'Something went wrong';

/**
 * The failure classes the server puts in front of a refusal.
 *
 * internal/pkg/apierr words a refusal as "<class>: <why>" — "forbidden: only
 * the person holding this task, or an administrator, can hand it to someone
 * else" — so that a transport can answer it with a 403, a 400 or a 404. The
 * class is the transport's; the words after it are the person's. These are
 * apierr's three, spelled as it spells them, and there is no fourth: nothing
 * on the server writes a "conflict" class.
 */
const REFUSAL_CLASSES = ['forbidden', 'invalid argument', 'not found'];

/** A class at the very start, lower case and followed by ": ", as apierr writes it. */
const LEADING_REFUSAL_CLASS = new RegExp(`^(?:${REFUSAL_CLASSES.join('|')}): `);

/**
 * The message an unknown thrown value carries, exactly as it was raised.
 *
 * For a log, or for the technical details someone passes on when they ask for
 * help: there the class is information, because it is how a 403 is told from a
 * 404.
 */
export function errorDetail(error: unknown, fallback = DEFAULT_FALLBACK): string {
  return messageOf(error) ?? fallback;
}

/**
 * The message of an unknown thrown value, for a person to read.
 *
 * The server's words without the class it put in front of them. A Connect
 * call's error is read the same way: connect-web writes its code in front of
 * the server's words ("[unknown] forbidden: …") and keeps them, untouched, as
 * `rawMessage`. Only a class at the very start is dropped, so a sentence that
 * merely uses one of the words keeps it; and the whole message is kept when
 * nothing follows the class, because a blank says less than the class does.
 */
export function errorMessage(error: unknown, fallback = DEFAULT_FALLBACK): string {
  const plain = plainWordsFor(error);
  if (plain !== undefined) return plain;
  const words = serverWordsOf(error) ?? messageOf(error);
  if (words === undefined) return fallback;
  return words.replace(LEADING_REFUSAL_CLASS, '') || words;
}

/**
 * Builds the body of a failure notification: what was being attempted, then
 * why it failed.
 */
export function failureMessage(action: string, error: unknown): string {
  return `${action}: ${errorMessage(error)}`;
}

function messageOf(error: unknown): string | undefined {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  if (typeof error === 'string' && error) {
    return error;
  }
  return nonEmptyString(error, 'message');
}

/** A Connect error's own words, without the code connect-web adds in front. */
function serverWordsOf(error: unknown): string | undefined {
  return nonEmptyString(error, 'rawMessage');
}

function nonEmptyString(value: unknown, property: string): string | undefined {
  if (!value || typeof value !== 'object' || !(property in value)) return undefined;
  const found = (value as Record<string, unknown>)[property];
  return typeof found === 'string' && found !== '' ? found : undefined;
}

/*
 * Plain words for a failure the server did not explain.
 *
 * A refusal at the door — no session, an expired one — is answered by the
 * auth middleware with http.Error's bare "Unauthorized", and a fetch that
 * never reached the server fails with the browser's "Failed to fetch". Those
 * landed in alerts and notifications as they were: "Unauthorized" on every
 * card of a page, with nothing saying what had happened or what to do.
 *
 * Words the server wrote for a person are kept. Only these are replaced: a
 * network failure, a status that has one meaning whatever was said (an ended
 * session, an overloaded or unreachable server), and a reply whose words are
 * nothing but the status's own name.
 */

const SESSION_ENDED = 'Your session has ended. Sign in again to continue.';
const UNREACHABLE = "Metis can't be reached right now. Check your connection and try again in a moment.";

/** What a status means to the person who asked, for a reply that carried no words of its own. */
export function statusMessage(status: number): string {
  if (status === 0) return UNREACHABLE;
  if (status === 400) return "That request wasn't accepted. Check what you entered and try again.";
  if (status === 401) return SESSION_ENDED;
  if (status === 403) return "You don't have permission to do this. Ask an administrator if you need access.";
  if (status === 404) return "We couldn't find that. It may have been moved or deleted.";
  if (status === 408 || status === 504) return 'The server took too long to answer. Please try again.';
  if (status === 409) return 'This was changed somewhere else at the same time. Reload the page and try again.';
  if (status === 413) return 'That is too large to send.';
  if (status === 429) return 'Too many requests at once. Wait a moment and try again.';
  if (status === 502 || status === 503) return UNREACHABLE;
  if (status >= 500) {
    return 'Something went wrong on the server. Please try again, and contact an administrator if it keeps happening.';
  }
  return "That couldn't be completed. Please try again.";
}

/** The browser's words for a request that never reached the server, in Chrome, Firefox and Safari. */
const NETWORK_FAILURE = /^(failed to fetch|networkerror when attempting to fetch resource\.?|load failed|network request failed)$/i;

/** A reply whose words are only the name of its status, as http.Error and statusText write them. */
const BARE_STATUS_WORDS = /^(unauthorized|invalid auth header|forbidden|not found|bad request|conflict|request failed|internal server error|internal error|bad gateway|service unavailable|gateway timeout|too many requests|payload too large|request entity too large)\.?$/i;

/**
 * Statuses whose meaning does not depend on what the reply said. Not 401: a
 * wrong password at sign-in, or when changing it, is a 401 that explains
 * itself ("invalid credentials"), and saying the session ended would be wrong.
 */
const ALWAYS_PLAIN = new Set([0, 429, 502, 503, 504]);

/**
 * The HTTP status a Connect code stands for, as the Connect protocol maps
 * them. Numbers rather than the Code enum so this module, which every page
 * imports, does not pull the Connect runtime into the first download.
 */
const CONNECT_CODE_STATUS: Record<number, number> = {
  2: 500, // unknown
  3: 400, // invalid_argument
  4: 504, // deadline_exceeded
  5: 404, // not_found
  6: 409, // already_exists
  7: 403, // permission_denied
  8: 429, // resource_exhausted
  13: 500, // internal
  14: 503, // unavailable
  16: 401, // unauthenticated
};

function isConnectError(error: unknown): boolean {
  return !!error && typeof error === 'object' && (error as { name?: unknown }).name === 'ConnectError';
}

function statusOf(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined;
  const { name, status, code } = error as { name?: unknown; status?: unknown; code?: unknown };
  if (name === 'RequestError' && typeof status === 'number') return status;
  if (name === 'ConnectError' && typeof code === 'number') return CONNECT_CODE_STATUS[code];
  return undefined;
}

function plainWordsFor(error: unknown): string | undefined {
  const words = (serverWordsOf(error) ?? messageOf(error))?.trim();
  if (words && NETWORK_FAILURE.test(words)) return UNREACHABLE;
  const status = statusOf(error);
  if (status === undefined) return undefined;
  if (ALWAYS_PLAIN.has(status) || !words || BARE_STATUS_WORDS.test(words)) return statusMessage(status);
  // No Connect call checks a password, so an unauthenticated one is always the session.
  if (status === 401 && isConnectError(error)) return SESSION_ENDED;
  return undefined;
}
