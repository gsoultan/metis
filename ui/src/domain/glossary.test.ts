import { describe, expect, it } from 'bun:test';

import { NODE_VOCABULARY } from './bpmnVocabulary';
import { alsoKnownAs, GLOSSARY, glossarySearchSummary, searchGlossary } from './glossary';

const firstMatch = (query: string) => searchGlossary(query)[0]?.term;
const definitionOf = (term: string) => GLOSSARY.find((entry) => entry.term === term)?.definition ?? '';

/*
 * Somebody who has read the BPMN specification searches for "Exclusive
 * Gateway". Somebody who has only used the palette searches for "Choose one
 * path". They are looking for the same thing, and both have to find it first.
 */
describe('every step on the palette can be looked up', () => {
  it.each(Object.entries(NODE_VOCABULARY))('%s, by its plain name and by its BPMN name', (_, word) => {
    expect(firstMatch(word.plainName)).toBe(word.plainName);
    expect(firstMatch(word.bpmnName)).toBe(word.plainName);
  });

  it('explains each step in the words the palette uses, so the two cannot drift apart', () => {
    for (const word of Object.values(NODE_VOCABULARY)) {
      const entry = GLOSSARY.find((candidate) => candidate.term === word.plainName);
      expect(entry?.definition).toBe(word.whatItDoes);
      expect(entry?.example).toBe(word.example);
    }
  });
});

describe('the words the rest of the product uses', () => {
  it.each([
    ['Instance', 'Process instance'],
    ['Deploy', 'Deployment'],
    ['Version', 'Process definition version'],
    ['Live version', 'Promoted version'],
    ['Staged version', 'Staged deployment'],
    ['Incident', 'Incident'],
    ['Connection', 'Connector instance'],
    ['Connector', 'Connector'],
    ['Decision table', 'DMN decision table'],
    ['Hit policy', 'DMN hit policy'],
  ])('has %s, found by that and by %s', (term, technicalName) => {
    expect(firstMatch(term)).toBe(term);
    expect(firstMatch(technicalName)).toBe(term);
  });
});

/*
 * Version history is where a version is tried, made live or scheduled, and the
 * glossary has to say what that screen lets somebody do, in its own words. The
 * button says "Make live". A staged version can be run from there ("Try v3
 * without making it live") and scheduled to take over; "nothing starts on it
 * until it is promoted" was wrong twice over.
 */
describe('the versions, as Version history handles them', () => {
  it('says a staged version can be run to try it, without making it live', () => {
    expect(definitionOf('Staged version')).toMatch(/without making it live/i);
  });

  it('says a staged version can be made live, or scheduled to take over', () => {
    expect(definitionOf('Staged version')).toContain('“Make live”');
    expect(definitionOf('Staged version')).toMatch(/schedule/i);
  });

  it.each(['Staged version', 'Live version'])('uses the words on the buttons in %s, not "promote"', (term) => {
    expect(definitionOf(term)).not.toMatch(/promot/i);
  });
});

/*
 * A choice with no path to take does not always leave an incident behind. The
 * engine refuses to guess a path, and what that refusal does depends on what
 * led to the choice: after an automatic step the job fails and raises an
 * incident, but after a person's task the completion itself is refused, so the
 * task stays open and nothing is left for anybody to retry.
 */
describe('what an incident is', () => {
  it('says a choice with no path raises one when an automatic step leads to it', () => {
    expect(definitionOf('Incident')).toMatch(/automatic step leads to it/i);
  });

  it("says that after a person's task, completing the task is refused instead", () => {
    expect(definitionOf('Incident')).toMatch(/completing the task is refused/i);
  });
});

describe('every entry', () => {
  it('is defined in sentences', () => {
    for (const entry of GLOSSARY) {
      expect(entry.definition).toMatch(/^[A-Z].*\.$/);
    }
  });

  /* Two entries under one name would make a search answer a word twice, two ways. */
  it('has a name no other entry uses', () => {
    const terms = GLOSSARY.map((entry) => entry.term.toLowerCase());
    expect(new Set(terms).size).toBe(terms.length);
  });
});

describe('the other name, shown beside the plain one', () => {
  it('is the technical name when it says something different', () => {
    const choose = GLOSSARY.find((entry) => entry.term === NODE_VOCABULARY.exclusiveGateway.plainName);
    expect(choose && alsoKnownAs(choose)).toBe('Exclusive Gateway');
  });

  it('is left out when it would only repeat the name', () => {
    const incident = GLOSSARY.find((entry) => entry.term === 'Incident');
    expect(incident && alsoKnownAs(incident)).toBeUndefined();
  });
});

describe('searching', () => {
  it('lists everything, alphabetically, before anything is typed', () => {
    expect(searchGlossary('')).toEqual(GLOSSARY);
    expect(searchGlossary('   ')).toEqual(GLOSSARY);
    const terms = GLOSSARY.map((entry) => entry.term);
    expect(terms).toEqual([...terms].sort((a, b) => a.localeCompare(b, 'en')));
  });

  it('ignores case, spaces and hyphens, so "subprocess" finds Sub-Process', () => {
    expect(firstMatch('subprocess')).toBe(NODE_VOCABULARY.subProcess.plainName);
    expect(firstMatch('EVENT BASED')).toBe(NODE_VOCABULARY.eventBasedGateway.plainName);
  });

  /*
   * "table" is the name of one entry and only mentioned in the explanation of
   * others. The one it names comes first; the others still come, because they
   * are what somebody asking about tables may also need.
   */
  it('puts a match on a name before a match in an explanation', () => {
    const terms = searchGlossary('table').map((entry) => entry.term);
    expect(terms[0]).toBe('Decision table');
    expect(terms).toContain('Hit policy');
    expect(terms).toContain(NODE_VOCABULARY.businessRuleTask.plainName);
  });

  it('answers nothing for a word it does not know', () => {
    expect(searchGlossary('blockchain')).toEqual([]);
  });
});

describe('saying how a search went', () => {
  it('counts everything when nothing is filtered out', () => {
    expect(glossarySearchSummary(GLOSSARY.length)).toBe(`${GLOSSARY.length} terms`);
  });

  it('says how many of them match', () => {
    expect(glossarySearchSummary(3)).toBe(`3 of ${GLOSSARY.length} match`);
  });

  /* An empty list with no explanation reads as broken. */
  it('suggests what to try when nothing matches', () => {
    expect(glossarySearchSummary(0)).toBe('Nothing matches. Try a shorter word, or the other name for it.');
  });
});
