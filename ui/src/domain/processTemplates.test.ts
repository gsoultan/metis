import { describe, expect, it } from 'bun:test';

import { PROCESS_TEMPLATES, templateById } from './processTemplates';

/** Deterministic ids, so a failure names the shape rather than a uuid. */
function counter() {
  let n = 0;
  return () => `n${++n}`;
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
