/**
 * How a webhook's sender signs, and whether it may still sign the old way.
 *
 * A legacy (v1) signature covers the request body alone, so a delivery signed
 * that way can be captured and posted again under a new delivery ID. v2 signs
 * the time of sending and the delivery ID as well. Webhooks that existed before
 * v2 accept the legacy scheme until a deadline, so their senders were not cut
 * off on the day of the upgrade; every webhook created since accepts v2 only.
 *
 * The screen has two jobs here: say which webhooks still accept the old way and
 * until when, while there is time to move their senders, and tell a sender
 * exactly what to send. Both are decided here, where they can be tested.
 */

/** What the server checks, header for header. */
export const V2_SIGNING = {
  timestampHeader: 'X-Metis-Timestamp',
  deliveryIdHeader: 'X-Delivery-Id',
  signatureHeader: 'X-Metis-Signature',
  prefix: 'v2=',
  signedString: '<timestamp>.<delivery id>.<raw body>',
  /** A delivery signed further than this from the server's clock is refused. */
  toleranceMinutes: 5,
} as const;

/** As much of a webhook as this depends on. */
export interface SigningInput {
  legacy_signatures_until?: string | null;
}

export type LegacySigning =
  | { state: 'v2-only' }
  | {
      state: 'accepting';
      until: Date;
      /** How long is left, in words: "90 days left". */
      remaining: string;
      /** Close enough that the sender has to be chased now. */
      urgent: boolean;
    };

/** Inside this, a deadline is a matter for this week rather than this quarter. */
const URGENT_DAYS = 14;

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

/**
 * Whether a webhook still accepts legacy signatures, and for how long.
 *
 * A closed window reads as v2 only because that is what the server does with
 * it. So does a deadline that cannot be read: claiming a webhook still accepts
 * the old way when that is not known would tell somebody they have time they
 * may not have.
 *
 * `now` is passed rather than read so a test can say what time it is.
 */
export function legacySigningOf(hook: SigningInput, now: Date = new Date()): LegacySigning {
  if (!hook.legacy_signatures_until) return { state: 'v2-only' };
  const until = new Date(hook.legacy_signatures_until);
  const left = until.getTime() - now.getTime();
  if (Number.isNaN(left) || left <= 0) return { state: 'v2-only' };
  return { state: 'accepting', until, remaining: remainingText(left), urgent: left < URGENT_DAYS * DAY_MS };
}

function remainingText(ms: number): string {
  if (ms >= DAY_MS) {
    const days = Math.floor(ms / DAY_MS);
    return days === 1 ? '1 day left' : `${days} days left`;
  }
  if (ms >= HOUR_MS) {
    const hours = Math.floor(ms / HOUR_MS);
    return hours === 1 ? '1 hour left' : `${hours} hours left`;
  }
  return 'less than an hour left';
}
