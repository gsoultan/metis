import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Every node type the engine can execute must be placeable in the designer.
 *
 * This failed silently for seven of them. `ErrorEndEventHandler`,
 * `EscalationThrowEventHandler`, `CompensationThrowEventHandler` and the throw
 * handlers were implemented, wired into the handler factory and covered by
 * passing BPMN scenario tests — and none appeared in the palette. The engine
 * ran them; nobody could draw them. The only way to author "throw an error
 * here so that boundary catches it" — a core BPMN pattern — was to hand-write
 * XML and import it.
 *
 * Nothing caught that, because both halves were individually correct: the Go
 * tests proved the engine executes the type, and the UI tests proved the
 * palette renders what it is given. The gap was between them.
 *
 * So this reads the engine's own list of node types and asserts the palette
 * covers it. Reading the Go source rather than keeping a copy here is the
 * point — a second list is a second thing to forget.
 */

const REPO_ROOT = join(import.meta.dir, '..', '..', '..');

function read(...parts: string[]): string {
  return readFileSync(join(REPO_ROOT, ...parts), 'utf8');
}

/** The node types the engine defines, minus the two that only draw boxes. */
function executableNodeTypes(): string[] {
  const source = read('server', 'domains', 'entities', 'node_type.go');
  const declared = [...source.matchAll(/\w+\s+NodeType = "(\w+)"/g)].map((m) => m[1]);
  return declared.filter((t) => t !== 'pool' && t !== 'lane');
}

function paletteTypes(): string[] {
  const source = read('ui', 'src', 'components', 'DesignerSidebar.tsx');
  return [...source.matchAll(/type:\s*'([a-zA-Z]+)'/g)].map((m) => m[1]);
}

/**
 * Types the engine routes to a handler that a palette entry already covers, so
 * a separate item would be a second way to draw the same thing.
 *
 * Each is a narrower spelling the XML importer may produce: the factory sends
 * `timerEvent` to the same handler as `intermediateCatchEvent`, and the
 * signal/message throw handlers do what `intermediateThrowEvent` does when the
 * matching field is filled in. Adding palette entries for these would offer a
 * modeller three ways to draw one step.
 */
const COVERED_BY_A_BROADER_ITEM: Record<string, string> = {
  timerEvent: 'intermediateCatchEvent, whose "wait for a time" variant is this',
  signalEvent: 'intermediateThrowEvent, whose broadcast variant is this',
  messageEvent: 'intermediateThrowEvent, whose send-to-one variant is this',
};

describe('designer palette coverage', () => {
  it('offers every node type the engine can execute', () => {
    const palette = new Set(paletteTypes());
    const missing = executableNodeTypes().filter(
      (type) => !palette.has(type) && !(type in COVERED_BY_A_BROADER_ITEM),
    );

    expect(missing).toEqual([]);
  });

  it('documents why an executable type has no palette entry of its own', () => {
    // The exemption list is only honest if every name in it is real. A stale
    // entry would hide a type that genuinely went missing.
    const executable = new Set(executableNodeTypes());
    for (const type of Object.keys(COVERED_BY_A_BROADER_ITEM)) {
      expect(executable.has(type)).toBe(true);
    }
  });

  it('places the throwing events, not only the catching ones', () => {
    // Named individually because this is the specific gap that shipped: every
    // catch had a palette entry and no throw did, so a process could react to
    // an error nobody could raise.
    const palette = new Set(paletteTypes());
    for (const type of [
      'errorEndEvent',
      'escalationThrowEvent',
      'compensationThrowEvent',
      'intermediateThrowEvent',
    ]) {
      expect(palette.has(type)).toBe(true);
    }
  });

  it('gives every placeable type somewhere to configure it', () => {
    // A palette entry with no config panel is a step you can draw and cannot
    // finish: an error end event whose code cannot be set throws an empty one,
    // which every boundary catches, so two different failures become
    // indistinguishable.
    const panel = read('ui', 'src', 'components', 'PropertyPanel.tsx');
    for (const type of ['errorEndEvent', 'escalationThrowEvent', 'compensationThrowEvent']) {
      expect(panel).toContain(`${type}: ThrowEventConfig`);
    }
  });
});
