import { describe, expect, it } from 'bun:test';

import { NODE_VOCABULARY, type NodeKind } from './bpmnVocabulary';

/**
 * Every node has to explain itself.
 *
 * The palette is the first thing somebody sees, and BPMN's own names are no help
 * to them: "Exclusive Gateway" and "Intermediate Catch Event" are terms of art
 * for a spec nobody outside this field has read. The vocabulary is what turns
 * those into a sentence — what the step does, and an example of when you would
 * use it.
 *
 * A node type added without one is a box in the palette with a machine name on
 * it, and nothing in a typechecker notices. This is what notices.
 */
describe('NODE_VOCABULARY', () => {
  const kinds = Object.keys(NODE_VOCABULARY) as NodeKind[];

  it('covers every node somebody can drop on the canvas', () => {
    expect(kinds.length).toBeGreaterThan(15);
  });

  it('gives each one a name a person would use', () => {
    for (const kind of kinds) {
      const entry = NODE_VOCABULARY[kind];
      expect(entry.plainName, `${kind} has no plain name`).toBeTruthy();
      // The plain name must not simply be the BPMN one — that is the problem it
      // exists to solve.
      expect(entry.plainName, `${kind}'s plain name is just its BPMN name`).not.toBe(entry.bpmnName);
    }
  });

  it('says what each one does, and gives an example', () => {
    for (const kind of kinds) {
      const entry = NODE_VOCABULARY[kind];
      expect(entry.whatItDoes, `${kind} does not say what it does`).toBeTruthy();
      expect(entry.example, `${kind} has no example`).toBeTruthy();

      // A sentence rather than a label. Length is a poor test of that — "Where
      // the process begins." is short and complete — so what is checked is that
      // it is written as a sentence and says something the name does not.
      expect(entry.whatItDoes.trim(), `${kind}'s description is not a sentence`).toMatch(/[.!?]$/);
      expect(
        entry.whatItDoes.toLowerCase().replace(/[^a-z]/g, ''),
        `${kind}'s description just repeats its name`,
      ).not.toBe(entry.plainName.toLowerCase().replace(/[^a-z]/g, ''));
      expect(entry.example.trim(), `${kind}'s example is not a sentence`).toMatch(/[.!?]$/);
    }
  });

  it('files each one under a heading the palette groups by', () => {
    const groups = new Set(kinds.map((kind) => NODE_VOCABULARY[kind].group));
    for (const kind of kinds) {
      expect(NODE_VOCABULARY[kind].group, `${kind} has no group`).toBeTruthy();
    }
    // Groups exist to keep the palette navigable; one group holding everything
    // would be the same as none.
    expect(groups.size).toBeGreaterThan(2);
  });

  it('does not explain two nodes with the same words', () => {
    const seen = new Map<string, string>();
    for (const kind of kinds) {
      const description = NODE_VOCABULARY[kind].whatItDoes;
      const already = seen.get(description);
      expect(already, `${kind} and ${already} share a description, so neither is explained`).toBeUndefined();
      seen.set(description, kind);
    }
  });
});
