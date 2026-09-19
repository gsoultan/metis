/**
 * What to tell somebody who could not sign in.
 *
 * The form printed the server's own string straight into an alert titled
 * "Authentication Failed":
 *
 *     Authentication Failed
 *     authentication failed: invalid credentials
 *
 * which says the same thing twice, the second time in the Go error-wrapping
 * style the handler happened to use (`fmt.Errorf("authentication failed: %w")`).
 * `.junie/guidelines.md` §5 asks for human-readable language; a wrapped error
 * chain is the least human string the product owns.
 *
 * The mapping is deliberately small. An unrecognised message is passed through
 * with its wrapping trimmed rather than replaced with something reassuring and
 * wrong — an operator debugging a real outage needs the detail, and inventing
 * "something went wrong" for a message we simply have not seen is how a
 * diagnosable failure becomes an unreportable one.
 */

export interface SignInError {
  /** One sentence, in the user's words. */
  message: string;
  /** What to do about it, when there is something to do. */
  hint?: string;
}

/** Recognised failures, matched on a substring of the lowercased message. */
const KNOWN: ReadonlyArray<{ match: string; result: SignInError }> = [
  {
    match: 'invalid credentials',
    result: {
      message: 'That username and password do not match.',
      hint: 'Check for caps lock. If you have forgotten the password, an administrator can reset it.',
    },
  },
  {
    match: 'too many requests',
    result: {
      message: 'Too many sign-in attempts from here.',
      hint: 'Wait a minute and try again.',
    },
  },
  {
    match: 'rate limit',
    result: {
      message: 'Too many sign-in attempts from here.',
      hint: 'Wait a minute and try again.',
    },
  },
  {
    match: 'disabled',
    result: {
      message: 'This account cannot sign in.',
      hint: 'An administrator needs to re-enable it.',
    },
  },
  {
    match: 'failed to fetch',
    result: {
      message: 'Cannot reach the Metis server.',
      hint: 'It may be starting up, or your connection may be down.',
    },
  },
  {
    match: 'networkerror',
    result: {
      message: 'Cannot reach the Metis server.',
      hint: 'It may be starting up, or your connection may be down.',
    },
  },
];

export function signInError(raw: string | null | undefined): SignInError {
  const text = (raw ?? '').trim();
  if (text === '') {
    return { message: 'Could not sign you in.' };
  }

  const haystack = text.toLowerCase();
  for (const { match, result } of KNOWN) {
    if (haystack.includes(match)) return result;
  }

  return { message: unwrap(text) };
}

/**
 * Drops Go's error-wrapping prefixes so an unrecognised message at least reads
 * as a sentence: `authentication failed: user store: no rows` becomes
 * `No rows`. The last segment is the cause; the ones before it are the call
 * path, which is for a log rather than a login form.
 */
function unwrap(text: string): string {
  const cause = text.split(':').pop()?.trim() ?? text;
  if (cause === '') return text;
  const sentence = cause[0].toUpperCase() + cause.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}
