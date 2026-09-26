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
