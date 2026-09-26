/**
 * The words Metis uses, in plain language and by their proper names.
 *
 * The palette already speaks plainly, "Choose one path" rather than "Exclusive
 * Gateway", and shows the BPMN name beside it so the notation can be learnt.
 * Nothing answered "what is a live version?" or "what does incident mean?" for
 * somebody who met the word on a screen and not in the palette. This does.
 *
 * The steps come straight from NODE_VOCABULARY, so the glossary and the palette
 * cannot describe the same step two ways. The palette is not translated yet,
 * so a step keeps its English name here in every language: somebody looks a
 * step up by the name they saw on it. The rest are the terms the product uses
 * around the steps, instances, versions, incidents, connections and decisions,
 * and those are in the catalogues (src/i18n/catalogues) with the rest of the
 * translated interface.
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

/** A message key and its values, as words in the chosen language (the translation context's `t`). */
export type Translate = (key: string, values?: Record<string, string | number>) => string;

const STEP_ENTRIES: GlossaryEntry[] = Object.values(NODE_VOCABULARY).map((word) => ({
  term: word.plainName,
  technicalName: word.bpmnName,
  definition: word.whatItDoes,
  example: word.example,
}));

/**
 * The product's own terms. The words are under `glossary.<key>.term`,
 * `.definition` and, where there is one, `.example`. The technical name is the
 * one used outside the product, in the BPMN and DMN specifications and the API,
 * so it is the same in every language.
 */
const PRODUCT_TERMS: readonly { key: string; technicalName: string; hasExample?: boolean }[] = [
  { key: 'instance', technicalName: 'Process instance', hasExample: true },
  { key: 'deploy', technicalName: 'Deployment' },
  { key: 'version', technicalName: 'Process definition version' },
  { key: 'liveVersion', technicalName: 'Promoted version' },
  { key: 'stagedVersion', technicalName: 'Staged deployment' },
  { key: 'incident', technicalName: 'Incident' },
  { key: 'connection', technicalName: 'Connector instance' },
  { key: 'connector', technicalName: 'Connector' },
  { key: 'decisionTable', technicalName: 'DMN decision table', hasExample: true },
  { key: 'hitPolicy', technicalName: 'DMN hit policy', hasExample: true },
];

/** Every entry in the language `t` speaks, in that language's alphabetical order. */
export function glossaryEntries(t: Translate, locale: string): GlossaryEntry[] {
  const productEntries = PRODUCT_TERMS.map(({ key, technicalName, hasExample }) => ({
    term: t(`glossary.${key}.term`),
    technicalName,
    definition: t(`glossary.${key}.definition`),
    example: hasExample ? t(`glossary.${key}.example`) : undefined,
  }));
  return [...STEP_ENTRIES, ...productEntries].sort((a, b) => a.term.localeCompare(b.term, locale));
}

/** The technical name, when showing it beside the term would say something the term does not. */
export function alsoKnownAs(entry: GlossaryEntry): string | undefined {
  return entry.technicalName.toLowerCase() === entry.term.toLowerCase() ? undefined : entry.technicalName;
}

/**
 * Letters and digits only, lower-cased and without accents, so "Sub-Process",
 * "sub process" and "subprocess" compare equal, and "décision" finds
 * "decision". People type the name they half-remember, on the keyboard they
 * have, not the one the specification hyphenates.
 *
 * Accents are folded (decomposed, then the marks dropped) rather than deleted
 * with the letter they sit on. Letters of any script are kept.
 */
function normalized(text: string): string {
  return text
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]/gu, '');
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

/** The entries of one language, normalised once for searching rather than on every keystroke. */
export interface GlossaryIndex {
  entries: readonly GlossaryEntry[];
  searchable: readonly SearchableEntry[];
}

export function glossaryIndex(entries: readonly GlossaryEntry[]): GlossaryIndex {
  return {
    entries,
    searchable: entries.map((entry) => ({
      entry,
      names: [normalized(entry.term), normalized(entry.technicalName)],
      explanation: normalized(`${entry.definition} ${entry.example ?? ''}`),
    })),
  };
}

function matchRank(item: SearchableEntry, needle: string): number {
  if (item.names.includes(needle)) return NAMED_EXACTLY;
  if (item.names.some((name) => name.includes(needle))) return NAME_CONTAINS;
  if (item.explanation.includes(needle)) return EXPLANATION_CONTAINS;
  return NO_MATCH;
}

/**
 * The entries that match, the ones named by the query first.
 *
 * An empty box lists everything. Something typed that no name could contain,
 * such as punctuation alone, matches nothing: listing everything for it read
 * as though everything had matched. Within each rank the alphabetical order
 * stands, because sorting is stable.
 */
export function searchGlossary(index: GlossaryIndex, query: string): readonly GlossaryEntry[] {
  if (query.trim() === '') return index.entries;
  const needle = normalized(query);
  if (needle === '') return [];
  return index.searchable
    .map((item) => ({ entry: item.entry, rank: matchRank(item, needle) }))
    .filter((match) => match.rank !== NO_MATCH)
    .sort((a, b) => a.rank - b.rank)
    .map((match) => match.entry);
}

/** One line under the search box, read out as the results change. */
export function glossarySearchSummary(t: Translate, matchCount: number, total: number): string {
  if (matchCount === 0) return t('glossary.noMatch');
  if (matchCount === total) return t('glossary.count', { count: total });
  return t('glossary.matches', { count: matchCount, total });
}
