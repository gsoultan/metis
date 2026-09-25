/**
 * The words Metis uses, in plain language and by their proper names.
 *
 * The palette already speaks plainly, "Choose one path" rather than "Exclusive
 * Gateway", and shows the BPMN name beside it so the notation can be learnt.
 * Nothing answered "what is a live version?" or "what does incident mean?" for
 * somebody who met the word on a screen and not in the palette. This does.
 *
 * The steps come straight from NODE_VOCABULARY, so the glossary and the palette
 * cannot describe the same step two ways. The rest are the terms the product
 * uses around them: instances, versions, incidents, connections and decisions.
 */

import { NODE_VOCABULARY } from './bpmnVocabulary';

export interface GlossaryEntry {
  /** What the product calls it. */
  term: string;
  /** Its BPMN or technical name, the one used outside this product. */
  technicalName: string;
  /** What it is, in plain language. */
  definition: string;
  /** A concrete case of it, where one helps. */
  example?: string;
}

const STEP_ENTRIES: GlossaryEntry[] = Object.values(NODE_VOCABULARY).map((word) => ({
  term: word.plainName,
  technicalName: word.bpmnName,
  definition: word.whatItDoes,
  example: word.example,
}));

const PRODUCT_ENTRIES: GlossaryEntry[] = [
  {
    term: 'Instance',
    technicalName: 'Process instance',
    definition: 'One run of a process, from its start to its finish. Each one carries its own information and is at its own step.',
    example: 'Every expense claim somebody submits is its own instance of the expense process.',
  },
  {
    term: 'Deploy',
    technicalName: 'Deployment',
    definition: 'Publishing a process so it can run. Each deploy saves a new version, and instances already running are left as they are.',
  },
  {
    term: 'Version',
    technicalName: 'Process definition version',
    definition: 'A numbered copy of a process, saved each time it is deployed. An instance stays on the version it started on unless somebody migrates it.',
  },
  {
    term: 'Live version',
    technicalName: 'Promoted version',
    definition: 'The version new instances start on. A process has one at a time: deploying normally makes the new version live, and Version history can make a different one live, straight away or at a time you choose.',
  },
  {
    term: 'Staged version',
    technicalName: 'Staged deployment',
    definition: 'A version that is deployed but not live, so new instances keep starting on the live one. In Version history you can run it to try it, without making it live, then make it live with “Make live” or schedule it to take over at a time you choose.',
  },
  {
    term: 'Incident',
    technicalName: 'Incident',
    definition: 'A step that could not finish, so its instance waits there until somebody deals with it. Usually a call to another system that kept failing, or a choice with no path to take. Fix the cause, then retry the step.',
  },
  {
    term: 'Connection',
    technicalName: 'Connector instance',
    definition: 'A connector set up for one project, with that project’s own address and credentials: your Slack workspace, rather than Slack in general. A step that uses a connector calls through its project’s connection.',
  },
  {
    term: 'Connector',
    technicalName: 'Connector',
    definition: 'A ready-made way to call a kind of system, such as Slack, email, a database or a web API. A project sets up a connection to it before its steps can use it.',
  },
  {
    term: 'Decision table',
    technicalName: 'DMN decision table',
    definition: 'Rules written as the lines of a table: when the inputs match a line, that line gives the answer. The policy lives in the table, so it can change without changing the process.',
    example: 'Claims under £500 are approved automatically; larger ones go to a manager.',
  },
  {
    term: 'Hit policy',
    technicalName: 'DMN hit policy',
    definition: 'What a decision table does when more than one line matches: take the first, allow only one, collect every match, and so on.',
    example: 'Two discount lines match the same order, and the first line that matches wins.',
  },
];

export const GLOSSARY: GlossaryEntry[] = [...STEP_ENTRIES, ...PRODUCT_ENTRIES].sort((a, b) =>
  a.term.localeCompare(b.term, 'en'),
);

/** The technical name, when showing it beside the term would say something the term does not. */
export function alsoKnownAs(entry: GlossaryEntry): string | undefined {
  return entry.technicalName.toLowerCase() === entry.term.toLowerCase() ? undefined : entry.technicalName;
}

/**
 * Letters and digits only, lower-cased, so "Sub-Process", "sub process" and
 * "subprocess" compare equal. People type the name they half-remember, not
 * the one the specification hyphenates.
 */
function normalized(text: string): string {
  return text.toLowerCase().replace(/[^a-z0-9]/g, '');
}

/** How well an entry matches, best first. */
const NAMED_EXACTLY = 0;
const NAME_CONTAINS = 1;
const EXPLANATION_CONTAINS = 2;
const NO_MATCH = 3;

interface SearchableEntry {
  entry: GlossaryEntry;
  names: string[];
  explanation: string;
}

/** Normalised once, rather than on every keystroke. */
const SEARCHABLE: SearchableEntry[] = GLOSSARY.map((entry) => ({
  entry,
  names: [normalized(entry.term), normalized(entry.technicalName)],
  explanation: normalized(`${entry.definition} ${entry.example ?? ''}`),
}));

function matchRank(item: SearchableEntry, needle: string): number {
  if (item.names.includes(needle)) return NAMED_EXACTLY;
  if (item.names.some((name) => name.includes(needle))) return NAME_CONTAINS;
  if (item.explanation.includes(needle)) return EXPLANATION_CONTAINS;
  return NO_MATCH;
}

/**
 * The entries that match, the ones named by the query first.
 *
 * An empty query lists everything. Within each rank the alphabetical order
 * stands, because sorting is stable.
 */
export function searchGlossary(query: string): GlossaryEntry[] {
  const needle = normalized(query);
  if (needle === '') return GLOSSARY;
  return SEARCHABLE
    .map((item) => ({ entry: item.entry, rank: matchRank(item, needle) }))
    .filter((match) => match.rank !== NO_MATCH)
    .sort((a, b) => a.rank - b.rank)
    .map((match) => match.entry);
}

/** One line under the search box, read out as the results change. */
export function glossarySearchSummary(matchCount: number): string {
  if (matchCount === 0) return 'Nothing matches. Try a shorter word, or the other name for it.';
  if (matchCount === GLOSSARY.length) return `${GLOSSARY.length} terms`;
  return `${matchCount} of ${GLOSSARY.length} match`;
}
