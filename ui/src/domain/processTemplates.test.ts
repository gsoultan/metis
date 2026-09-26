import { describe, expect, it } from 'bun:test';

import { PROCESS_TEMPLATES, templateById, type BuiltTemplate, type ProcessTemplate } from './processTemplates';
import { validateProcess } from './processValidation';
import { workerTopic } from './serviceImplementation';

/** Deterministic ids, so a failure names the shape rather than a uuid. */
function counter() {
  let n = 0;
  return () => `n${++n}`;
}

/**
 * The template as it is once the author has made the decisions it leaves
 * open: every path out of a gateway says when it is taken, and every step that
 * asks a person says who does it.
 */
function withDecisionsMade(built: BuiltTemplate): BuiltTemplate {
  const gateways = new Set(built.nodes.filter((n) => n.type.endsWith('Gateway')).map((n) => n.id));
  return {
    ...built,
    nodes: built.nodes.map((n) =>
      n.type === 'userTask' ? { ...n, data: { ...n.data, candidateGroups: ['decided by the author'] } } : n,
    ),
    edges: built.edges.map((e) =>
      gateways.has(e.source) ? { ...e, data: { ...e.data, condition: 'decided by the author' } } : e,
    ),
  };
}

describe('every template is a process somebody could deploy', () => {
  for (const template of PROCESS_TEMPLATES) {
    describe(template.name, () => {
      const built = template.build(counter());

      it('starts and ends', () => {
        const types = built.nodes.map((n) => n.type);
        expect(types).toContain('startEvent');
        expect(types).toContain('endEvent');
      });

      it('gives every node a unique id', () => {
        const ids = built.nodes.map((n) => n.id);
        expect(new Set(ids).size).toBe(ids.length);
      });

      it('connects every edge to nodes that exist', () => {
        const ids = new Set(built.nodes.map((n) => n.id));
        for (const e of built.edges) {
          expect(ids.has(e.source)).toBe(true);
          expect(ids.has(e.target)).toBe(true);
        }
      });

      /*
       * The property that makes a template worth shipping. A node nothing
       * reaches is a step that never runs, and an author who starts from a
       * template will not think to check — they will assume the shape is
       * sound, which is the whole reason for offering one.
       */
      it('leaves no node unreachable from the start', () => {
        const start = built.nodes.find((n) => n.type === 'startEvent');
        expect(start).toBeDefined();

        const out = new Map<string, string[]>();
        for (const e of built.edges) {
          out.set(e.source, [...(out.get(e.source) ?? []), e.target]);
        }

        const seen = new Set<string>([start!.id]);
        const queue = [start!.id];
        while (queue.length > 0) {
          for (const next of out.get(queue.pop()!) ?? []) {
            if (!seen.has(next)) {
              seen.add(next);
              queue.push(next);
            }
          }
        }

        const stranded = built.nodes.filter((n) => !seen.has(n.id)).map((n) => n.data.label);
        expect(stranded).toEqual([]);
      });

      it('leaves every path able to finish', () => {
        const out = new Map<string, string[]>();
        for (const e of built.edges) {
          out.set(e.source, [...(out.get(e.source) ?? []), e.target]);
        }
        const dead = built.nodes
          .filter((n) => n.type !== 'endEvent' && (out.get(n.id) ?? []).length === 0)
          .map((n) => n.data.label);
        expect(dead).toEqual([]);
      });

      it('focuses a node it actually contains', () => {
        expect(built.nodes.some((n) => n.id === built.focusId)).toBe(true);
      });

      it('gives every node a label a person can read', () => {
        for (const n of built.nodes) {
          expect(typeof n.data.label).toBe('string');
          expect((n.data.label as string).length).toBeGreaterThan(0);
        }
      });
    });
  }
});

/*
 * The designer's own validator is the bar a template has to clear.
 *
 * A template leaves two things open on purpose: which way each gateway goes,
 * and who does each step that asks a person. The designer reports the first as
 * errors and the second as warnings when the template lands, and that is the
 * point, because it takes the author straight to the decisions only they can
 * make. Anything else the validator finds is a defect the template shipped
 * with, and an author who starts from a template assumes it has none. The
 * invoice template's two automatic steps were pointed at nothing, so every
 * invoice passed through "Check the invoice" unchecked.
 */
describe('nothing to fix but the decisions a template leaves to you', () => {
  for (const template of PROCESS_TEMPLATES) {
    it(`${template.name} has no errors and no warnings once its paths say when they are taken and its steps who does them`, () => {
      const { nodes, edges } = withDecisionsMade(template.build(counter()));
      expect(validateProcess(nodes, edges)).toEqual([]);
    });
  }
});

/*
 * A template must not arrive looking configured when it is not. An empty
 * gateway condition routes nothing, and an author who sees a filled-in
 * template has no reason to look at it — the same failure the decide-group
 * builder avoids by leaving its condition as a comment.
 */
describe('what a template refuses to decide for you', () => {
  it('sets no gateway conditions', () => {
    for (const template of PROCESS_TEMPLATES) {
      const built = template.build(counter());
      for (const e of built.edges) {
        expect(e.data?.condition ?? '').toBe('');
      }
    }
  });

  /*
   * Who does a step is the organization's to say, and a template cannot know
   * its people or teams. A team it guessed would be one nobody is in, which
   * is worse than naming nobody: then even an administrator could not take
   * the task.
   */
  it('names nobody to do a step that asks a person', () => {
    for (const template of PROCESS_TEMPLATES) {
      const built = template.build(counter());
      for (const n of built.nodes.filter((x) => x.type === 'userTask')) {
        expect(n.data.assignee ?? '').toBe('');
        expect(n.data.candidateUsers ?? []).toEqual([]);
        expect(n.data.candidateGroups ?? []).toEqual([]);
      }
    }
  });

  it('points no service task at a URL', () => {
    for (const template of PROCESS_TEMPLATES) {
      const built = template.build(counter());
      for (const n of built.nodes.filter((x) => x.type === 'serviceTask')) {
        expect(n.data.url ?? '').toBe('');
      }
    }
  });

  it('labels the branches out of a gateway, so the author knows which is which', () => {
    for (const template of PROCESS_TEMPLATES) {
      const built = template.build(counter());
      const gateways = built.nodes.filter((n) => n.type === 'exclusiveGateway');
      for (const g of gateways) {
        const branches = built.edges.filter((e) => e.source === g.id);
        if (branches.length > 1) {
          for (const b of branches) {
            expect(typeof b.label).toBe('string');
            expect((b.label as string).length).toBeGreaterThan(0);
          }
        }
      }
    }
  });
});

describe('looking one up', () => {
  it('finds a template by id', () => {
    expect(templateById('simple-approval')?.name).toBe('Simple approval');
  });

  it('answers null for anything else, so an unknown id opens a blank canvas', () => {
    expect(templateById('does-not-exist')).toBeNull();
    expect(templateById(undefined)).toBeNull();
    expect(templateById(null)).toBeNull();
    expect(templateById('')).toBeNull();
  });

  it('keeps ids unique, or the gallery would open the wrong one', () => {
    const ids = PROCESS_TEMPLATES.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
  });
});

/*
 * The defect this guards. A node carries its kind twice — React Flow's `type`
 * and `data.nodeType` — and the templates originally set only the first. The
 * canvas rendered, the shapes connected, and every task labelled itself "Call
 * another system", because that is the fallback the vocabulary lands on.
 *
 * Nothing about the diagram looked wrong; the words on it were.
 */
describe('a node says what it is in both places', () => {
  it('sets data.nodeType to match the React Flow type', () => {
    for (const template of PROCESS_TEMPLATES) {
      const built = template.build(counter());
      for (const n of built.nodes) {
        expect(n.data.nodeType).toBe(n.type);
      }
    }
  });
});

/*
 * A gateway with one way out decides nothing, and the designer's own validator
 * flags it. A template must not arrive carrying a warning it taught the author
 * to ignore.
 */
describe('no shape that only exists to be deleted', () => {
  it('gives every gateway more than one way out', () => {
    for (const template of PROCESS_TEMPLATES) {
      const built = template.build(counter());
      for (const g of built.nodes.filter((n) => n.type.endsWith('Gateway'))) {
        const out = built.edges.filter((e) => e.source === g.id);
        expect(out.length).toBeGreaterThan(1);
      }
    }
  });
});

/*
 * What a template says it does is what somebody choosing one reads. The
 * invoice template's automatic steps wait for a worker, and until one asks for
 * the work every invoice stops at "Check the invoice". A description that says
 * it is "checked automatically" promises what the template cannot do on its
 * own, and does not say what the author has to provide.
 */
describe('what a template says about the steps a worker has to do', () => {
  const workerSteps = (template: ProcessTemplate) =>
    template.build(counter()).nodes.filter((n) => n.type === 'serviceTask' && workerTopic(n.data) !== '');

  it('covers at least one template, so it tests something', () => {
    expect(PROCESS_TEMPLATES.some((template) => workerSteps(template).length > 0)).toBe(true);
  });

  for (const template of PROCESS_TEMPLATES) {
    if (workerSteps(template).length === 0) continue;
    it(`${template.name} says a worker is needed, and does not call the work automatic`, () => {
      expect(template.description).toMatch(/\bworker\b/i);
      expect(template.description).not.toMatch(/automatic/i);
    });
  }
});
