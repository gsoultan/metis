import type { ActionRow } from './instanceMigration';

/**
 * What somebody has said in the migration dialog, and which pair of versions
 * they said it about.
 *
 * The dialog kept its mapping rows, its accepted holds and its "decide instead
 * of moving" rows in state that outlived it: close it, open it for another
 * version's work, and the previous run's mapping, acknowledgements and skips
 * were still there — and were sent with the next plan and the next apply. An
 * acknowledgement is of one plan; a skip is somebody's decision about one
 * version's work. Neither may travel to another.
 */

/** One row of the mapping editor. */
export interface MappingRow {
  /** Stable across edits, so a row keeps its focus when another is removed. */
  id: number;
  from: string;
  to: string;
}

/** Everything somebody has said in the migration dialog, for one pair of versions. */
export interface MigrationDraft {
  /** The source and target it was written for. See versionPair. */
  pair: string;
  /** The mapping as edited, or null until it is: the proposed one shows until then. */
  rows: MappingRow[] | null;
  /** Control-bearing steps whose loss was accepted, by node id. */
  accepted: string[];
  /** Nodes whose work is decided rather than moved. */
  actions: ActionRow[];
}

/** Names a source and target together, which is what a draft belongs to. */
export function versionPair(sourceId: string, targetId: string): string {
  return `${sourceId}→${targetId}`;
}

export function blankDraft(pair: string): MigrationDraft {
  return { pair, rows: null, accepted: [], actions: [] };
}

/**
 * The draft for the pair on screen: the one written for it, or a blank one.
 *
 * Derived when the dialog renders rather than reset by an effect, so there is
 * no render in which one pair's draft is shown, or sent, for another. Closing
 * the dialog drops the draft altogether; this is what makes the target
 * changing underneath an open dialog — the live version moving on — safe too.
 */
export function draftFor(draft: MigrationDraft | null, pair: string): MigrationDraft {
  return draft !== null && draft.pair === pair ? draft : blankDraft(pair);
}

/** The mapping to show and send: the person's, or the proposal until there is one. */
export function mappingOf(draft: MigrationDraft, proposal: MappingRow[]): MappingRow[] {
  return draft.rows ?? proposal;
}

/**
 * A proposed mapping as editor rows. Negative ids, so they never collide with
 * the ones given to rows somebody adds.
 */
export function proposedRows(mapping: Record<string, string>): MappingRow[] {
  return Object.entries(mapping).map(([from, to], index) => ({ id: -(index + 1), from, to }));
}

/** One thing somebody did in the dialog. */
export type DraftEdit =
  | { type: 'mapping'; change: (rows: MappingRow[]) => MappingRow[] }
  | { type: 'accept'; nodeId: string; accepted: boolean }
  | { type: 'actions'; change: (actions: ActionRow[]) => ActionRow[] };

/**
 * Applies an edit to the mapping that was on screen when it was made.
 *
 * Any edit pins that mapping: an acknowledgement is of the plan in front of
 * the person, so a proposal that changes afterwards must not change what they
 * acknowledged. A mapping edit drops every acknowledgement given against the
 * old mapping — carrying one across an edit is how somebody accepts something
 * they never read.
 */
export function editDraft(draft: MigrationDraft, edit: DraftEdit, proposal: MappingRow[]): MigrationDraft {
  const rows = mappingOf(draft, proposal);
  switch (edit.type) {
    case 'mapping': {
      const next = edit.change(rows);
      return { ...draft, rows: next, accepted: sameMapping(rows, next) ? draft.accepted : [] };
    }
    case 'accept': {
      const others = draft.accepted.filter((id) => id !== edit.nodeId);
      return { ...draft, rows, accepted: edit.accepted ? [...others, edit.nodeId] : others };
    }
    case 'actions':
      return { ...draft, rows, actions: edit.change(draft.actions) };
  }
}

function sameMapping(a: MappingRow[], b: MappingRow[]): boolean {
  const pairs = (rows: MappingRow[]) => rows.map((row) => `${row.from}->${row.to}`).join('|');
  return pairs(a) === pairs(b);
}
