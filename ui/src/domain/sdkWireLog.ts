/**
 * What actually went over the wire, kept so it can be pasted into a bug report.
 *
 * When an integration does not work the argument is always the same — "I sent
 * the right thing" / "the server never got it" — and it is settled by the
 * request and the reply side by side. This is that record: one row per call,
 * with the body that was sent, the body that came back, the status and how long
 * it took.
 *
 * It is capped. A sandbox left open all afternoon is a list that grows with
 * every click, holding every request body in memory, in a tab nobody reloads.
 */

export type WireTone = 'ok' | 'refused' | 'failed';

export interface WireCall {
  id: string;
  /** What the person meant — "Report reverse-charge done". */
  label: string;
  method: string;
  /** Relative to the API base, as sent. */
  path: string;
  /** Null when the request never reached a server at all. */
  status: number | null;
  durationMs: number;
  requestBody?: unknown;
  responseBody?: unknown;
  /** A sentence, when the call did not succeed. */
  error?: string;
  /** Epoch milliseconds. */
  at: number;
}

/**
 * How many calls are kept.
 *
 * Enough to cover a whole journey — start, a few fetches, a complete, a message
 * — with room to scroll back, and small enough that the bodies behind them
 * cannot become the largest thing on the page.
 */
export const WIRE_LOG_LIMIT = 40;

/** Newest first, capped. */
export function appendCall(
  log: readonly WireCall[],
  call: WireCall,
  limit: number = WIRE_LOG_LIMIT,
): WireCall[] {
  return [call, ...log].slice(0, Math.max(1, limit));
}

/**
 * Three outcomes, not two.
 *
 * A refusal is the server working correctly and saying no — a missing
 * correlation key, a lock somebody else holds — and it is the most common thing
 * to see here while an integration is being written. Colouring it the same red
 * as an outage teaches people to ignore the colour.
 */
export function callTone(call: WireCall): WireTone {
  if (call.status === null) return 'failed';
  if (call.status >= 200 && call.status < 300) return 'ok';
  if (call.status >= 400 && call.status < 500) return 'refused';
  return 'failed';
}

/** Latency at the precision a person can act on. */
export function formatLatency(ms: number): string {
  if (ms < 1) return '<1 ms';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
}

/**
 * The whole transcript as plain text.
 *
 * Deliberately not JSON: this is written to be pasted into an issue or a chat
 * message, where a reviewer reads it rather than parses it.
 */
export function wireLogAsText(log: readonly WireCall[]): string {
  if (log.length === 0) {
    return 'No calls yet.';
  }
  // Oldest first here, unlike on screen: a transcript is read forwards.
  return [...log]
    .reverse()
    .map((call) => {
      const lines = [
        `${new Date(call.at).toISOString()}  ${call.method} ${call.path}`,
        `  ${call.label} → ${call.status ?? 'no reply'} in ${formatLatency(call.durationMs)}`,
      ];
      if (call.requestBody !== undefined) {
        lines.push(`  request:  ${JSON.stringify(call.requestBody)}`);
      }
      if (call.responseBody !== undefined) {
        lines.push(`  response: ${JSON.stringify(call.responseBody)}`);
      }
      if (call.error !== undefined) {
        lines.push(`  error:    ${call.error}`);
      }
      return lines.join('\n');
    })
    .join('\n\n');
}
