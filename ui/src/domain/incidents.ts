/**
 * What an incident means, in the words of whoever has to fix it.
 *
 * A job that runs out of retries raises an incident and stops. The record that
 * lands carries an engineer's error — a wrapped Go string naming a node and an
 * HTTP status — and the person reading it is usually an operations person who
 * wants to know two things: whose fault is this, and can I just try again.
 *
 * These functions answer both from the error text alone. They are deliberately
 * pattern matching rather than a taxonomy the engine emits: the errors come from
 * connectors, from partners' APIs and from the standard library, and a scheme
 * that required each of those to declare itself would go stale on the first one
 * that did not.
 */

/** An incident as the API returns it. */
export interface ApiIncident {
  id: string;
  error: string;
  status: string;
  created_at: string;
  node?: { id?: string; name?: string };
  job?: { id?: string; retries?: number };
}

/** How the incident should be read and what to do about it. */
export interface IncidentExplanation {
  /** One line naming the likely cause, in plain English. */
  cause: string;
  /** What the operator can do, if anything. */
  suggestion: string;
  /**
   * Whether trying again is likely to help. Retrying a timeout is sensible;
   * retrying a malformed URL just fails again in the same way, and offering it
   * as the obvious button teaches people the button does not work.
   */
  worthRetrying: boolean;
}

const PATTERNS: Array<{ match: RegExp; explain: (matched: RegExpMatchArray) => IncidentExplanation }> = [
  {
    match: /circuit breaker is (open|half-open)/i,
    explain: () => ({
      cause: 'The service being called has been failing repeatedly, so the engine stopped calling it.',
      suggestion: 'Check whether that service is back up. The engine tries again on its own once it is.',
      worthRetrying: true,
    }),
  },
  {
    match: /context deadline exceeded|timeout|timed out/i,
    explain: () => ({
      cause: 'The service being called did not answer in time.',
      suggestion: 'Usually temporary. Try again; if it keeps happening, the service is overloaded or unreachable.',
      worthRetrying: true,
    }),
  },
  {
    match: /status (5\d\d)/i,
    explain: (m) => ({
      cause: `The service being called returned ${m[1]} — it failed on its side, not ours.`,
      suggestion: 'Try again once that service is healthy.',
      worthRetrying: true,
    }),
  },
  {
    match: /status (401|403)/i,
    explain: (m) => ({
      cause: `The service being called refused the request (${m[1]}) — the credentials are wrong or expired.`,
      suggestion: 'Reconnect the connector with valid credentials, then try again.',
      worthRetrying: false,
    }),
  },
  {
    match: /status (4\d\d)/i,
    explain: (m) => ({
      cause: `The service being called rejected the request (${m[1]}) — it did not like what was sent.`,
      suggestion: 'Check the step’s input mapping against what that service expects. Retrying sends the same thing again.',
      worthRetrying: false,
    }),
  },
  {
    match: /blocked by egress policy/i,
    explain: () => ({
      cause: 'The address is one this server is not allowed to call.',
      suggestion: 'Private and loopback addresses are blocked by default. An administrator can allow it if it is genuinely internal.',
      worthRetrying: false,
    }),
  },
  {
    match: /no such host|dial tcp|connection refused/i,
    explain: () => ({
      cause: 'The address could not be reached at all.',
      suggestion: 'Check the URL on the step, and that the service is running.',
      worthRetrying: true,
    }),
  },
  {
    match: /connector .*not found|connector lookup failed/i,
    explain: () => ({
      cause: 'The step names a connection that no longer exists.',
      suggestion: 'Point the step at an existing connection, or recreate the one it expects.',
      worthRetrying: false,
    }),
  },
  {
    match: /decision .*not found|could not get decision/i,
    explain: () => ({
      cause: 'The step names a decision table that no longer exists.',
      suggestion: 'Restore the decision table, or point the step at another one.',
      worthRetrying: false,
    }),
  },
  {
    match: /hit policy|UNIQUE hit policy violated|disagree|priority order/i,
    explain: () => ({
      cause: 'A decision table contradicted itself: more lines matched than its hit policy allows.',
      suggestion: 'Open the table and narrow the overlapping lines, or change how it settles ties.',
      worthRetrying: false,
    }),
  },
];

/** Reads an incident's error and says what it means. */
export function explainIncident(error: string): IncidentExplanation {
  for (const pattern of PATTERNS) {
    const matched = error.match(pattern.match);
    if (matched) return pattern.explain(matched);
  }
  return {
    cause: 'The step failed and the engine ran out of attempts.',
    suggestion: 'The full error is below. Try again if you believe the cause has been dealt with.',
    worthRetrying: true,
  };
}

/** The step's name, falling back to whatever identifies it. */
export function incidentStep(incident: ApiIncident): string {
  return incident.node?.name || incident.node?.id || 'a step';
}
