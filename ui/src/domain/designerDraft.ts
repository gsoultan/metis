/**
 * The unsaved work the designer keeps in the browser.
 *
 * The canvas autosaves to localStorage every few seconds, but nothing ever read
 * it back: a browser crash, an accidental tab close or a stray navigation threw
 * away everything since the last *deploy*, and deploying was the only way to
 * save at all. The header said "Last saved 14:32" the whole time, which was
 * true of localStorage and useless to the person who had just lost their work.
 *
 * The decisions about a draft live here, away from React, so they can be tested
 * without a DOM: what a draft looks like, whether one is worth offering back,
 * and how to read one written by an older version of the app.
 */

/** The shape written to storage. Kept flat and versioned so it can be grown. */
export interface DesignerDraft {
  version: number;
  /** The definition this draft belongs to, or "new" for an unsaved process. */
  definitionId: string;
  processName: string;
  processKey: string;
  nodes: unknown[];
  edges: unknown[];
  /** ISO-8601. When the draft was written. */
  savedAt: string;
}

export const DRAFT_VERSION = 1;

/** Storage key for a definition's draft. */
export function draftKey(definitionId?: string | null): string {
  return `metis_draft_${definitionId ?? 'new'}`;
}

/**
 * A draft is only worth keeping if it holds something. An empty canvas is not
 * unsaved work; offering to restore one would train people to dismiss the
 * prompt, which is how a real restore gets dismissed too.
 */
export function isWorthSaving(nodes: unknown[]): boolean {
  return nodes.length > 0;
}

/**
 * Reads a stored draft, tolerating anything that is not one.
 *
 * Storage is shared with other tabs, other versions and, in principle, anything
 * else on the origin. A malformed or foreign value must read as "no draft"
 * rather than throw on a JSON parse during a route transition.
 */
export function parseDraft(raw: string | null): DesignerDraft | null {
  if (!raw) return null;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof value !== 'object' || value === null) return null;
  const d = value as Partial<DesignerDraft> & { timestamp?: string };
  if (!Array.isArray(d.nodes) || !Array.isArray(d.edges)) return null;
  // `timestamp` is what the first version of the autosave wrote. It is read
  // here rather than discarded so an upgrade does not throw away the draft
  // somebody had open at the time.
  const savedAt = typeof d.savedAt === 'string' ? d.savedAt : d.timestamp;
  if (typeof savedAt !== 'string' || savedAt === '') return null;
  return {
    version: typeof d.version === 'number' ? d.version : 0,
    definitionId: typeof d.definitionId === 'string' ? d.definitionId : 'new',
    processName: typeof d.processName === 'string' ? d.processName : '',
    processKey: typeof d.processKey === 'string' ? d.processKey : '',
    nodes: d.nodes,
    edges: d.edges,
    savedAt,
  };
}

/**
 * Whether to offer a draft back to the person opening this process.
 *
 * Offered only when it has content and is genuinely newer than what the server
 * returned. A draft older than the deployed version is work that was already
 * published, or was superseded by somebody else — restoring it would quietly
 * undo a colleague.
 */
export function shouldOfferDraft(
  draft: DesignerDraft | null,
  deployedAt: string | Date | null | undefined,
): boolean {
  if (!draft || draft.nodes.length === 0) return false;
  const saved = Date.parse(draft.savedAt);
  if (Number.isNaN(saved)) return false;
  if (!deployedAt) return true;
  const deployed = deployedAt instanceof Date ? deployedAt.getTime() : Date.parse(String(deployedAt));
  if (Number.isNaN(deployed)) return true;
  return saved > deployed;
}

/** A sentence saying how old the draft is, for the restore prompt. */
export function describeDraftAge(draft: DesignerDraft, now: Date = new Date()): string {
  const saved = Date.parse(draft.savedAt);
  if (Number.isNaN(saved)) return 'from an earlier session';
  const minutes = Math.floor((now.getTime() - saved) / 60000);
  if (minutes < 1) return 'from a moment ago';
  if (minutes === 1) return 'from a minute ago';
  if (minutes < 60) return `from ${minutes} minutes ago`;
  const hours = Math.floor(minutes / 60);
  if (hours === 1) return 'from an hour ago';
  if (hours < 24) return `from ${hours} hours ago`;
  const days = Math.floor(hours / 24);
  return days === 1 ? 'from yesterday' : `from ${days} days ago`;
}

/** Builds the value to store. */
export function buildDraft(input: {
  definitionId?: string | null;
  processName: string;
  processKey: string;
  nodes: unknown[];
  edges: unknown[];
  savedAt?: Date;
}): DesignerDraft {
  return {
    version: DRAFT_VERSION,
    definitionId: input.definitionId ?? 'new',
    processName: input.processName,
    processKey: input.processKey,
    nodes: input.nodes,
    edges: input.edges,
    savedAt: (input.savedAt ?? new Date()).toISOString(),
  };
}

/**
 * Where the canvas stands. "Not deployed yet" used to show on every process
 * with no unsaved changes, live ones included.
 */
export function headerStatus(lastSaved: Date | null, deployedVersion: number | null): string {
  if (lastSaved) {
    const base = deployedVersion ? `Changes to version ${deployedVersion}` : 'Draft';
    return `${base} kept in this browser at ${lastSaved.toLocaleTimeString()} · Deploy to publish`;
  }
  return deployedVersion ? `Version ${deployedVersion} · no unsaved changes` : 'Not deployed yet';
}
