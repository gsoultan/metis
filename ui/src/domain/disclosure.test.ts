import { describe, expect, it } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

import {
  advancedVisibility,
  editRawSettings,
  implementationOptions,
  loopSummary,
  rawEditorView,
  type RawDraft,
} from './disclosure';

const SRC = join(import.meta.dir, '..');

function readSource(path: string): string {
  return readFileSync(join(SRC, path), 'utf8');
}

/**
 * Basic mode may hide an advanced setting only while it is empty.
 *
 * It used to hide them outright. A service task set to run a script showed a
 * blank "What it calls" and no script, and a user task pointing at an outside
 * form showed no sign of it. The step still did those things; the person
 * looking at it could not tell.
 */
describe('advancedVisibility', () => {
  const UNSET: Array<[string, unknown]> = [
    ['undefined', undefined],
    ['null', null],
    ['an empty string', ''],
    ['zero', 0],
    ['NaN', Number.NaN],
    ['false', false],
    ['an empty list', []],
    ['an empty object', {}],
  ];

  const SET: Array<[string, unknown]> = [
    ['a form key', 'invoice-form'],
    // Still a value: the task carries it, and the inbox shows it.
    ['a string of spaces', '  '],
    ['a script', 'setVar("total", 1)'],
    ['a number', 3],
    ['a negative number', -1],
    ['true', true],
    ['a list', ['finance']],
    ['an object', { amount: 'total' }],
  ];

  it.each(SET)('summarises %s in basic mode rather than hiding it', (_name, value) => {
    expect(advancedVisibility(false, value)).toBe('summary');
  });

  it.each(UNSET)('hides %s in basic mode', (_name, value) => {
    expect(advancedVisibility(false, value)).toBe('hidden');
  });

  it.each([...SET, ...UNSET])('lets expert mode edit %s', (_name, value) => {
    expect(advancedVisibility(true, value)).toBe('edit');
  });
});

describe('implementationOptions', () => {
  const values = (expert: boolean, current: string) =>
    implementationOptions(expert, current).map((option) => option.value);

  it('offers basic mode the three ways that need no code', () => {
    expect(values(false, 'push')).toEqual(['push', 'connector', 'external']);
  });

  it('adds running a script in expert mode', () => {
    expect(values(true, 'push')).toEqual(['push', 'connector', 'external', 'script']);
  });

  it('keeps a script step readable in basic mode, under its usual name', () => {
    // Without it the select had no option for the step's value and drew an
    // empty box, as if the step called nothing.
    const script = implementationOptions(false, 'script').find((option) => option.value === 'script');
    const expertScript = implementationOptions(true, 'script').find((option) => option.value === 'script');

    expect(script).toBeDefined();
    expect(script).toEqual(expertScript);
  });

  it('keeps a value this editor does not offer, so it still shows', () => {
    // An imported file, or one saved by a later version, can name a way this
    // list does not know. It is shown as it is rather than as nothing.
    const unknown = implementationOptions(false, 'soap').find((option) => option.value === 'soap');

    expect(unknown?.label).toBe('soap');
    expect(unknown?.description).toBeTruthy();
    expect(values(true, 'soap')).toContain('soap');
  });

  it('always includes the current value, in either mode', () => {
    for (const expert of [false, true]) {
      for (const current of ['push', 'connector', 'external', 'script', 'soap']) {
        expect(values(expert, current), `${current}, expert ${expert}`).toContain(current);
      }
    }
  });

  it('lists each way once', () => {
    for (const expert of [false, true]) {
      for (const current of ['push', 'script', 'soap']) {
        const listed = values(expert, current);
        expect(new Set(listed).size, `${current}, expert ${expert}`).toBe(listed.length);
      }
    }
  });

  it('adds nothing for an empty value', () => {
    expect(values(false, '')).toEqual(['push', 'connector', 'external']);
  });

  it('describes every way it offers', () => {
    for (const option of implementationOptions(true, 'soap')) {
      expect(option.label, option.value).toBeTruthy();
      expect(option.description, option.value).toBeTruthy();
    }
  });
});

describe('loopSummary', () => {
  it.each([
    ['no type', {}],
    ['an empty type', { multiInstanceType: '' }],
    ['"none"', { multiInstanceType: 'none', collection: 'orders' }],
  ])('says nothing for a step with %s, which runs once', (_name, data) => {
    expect(loopSummary(data)).toBeUndefined();
  });

  it('says what a parallel loop goes through', () => {
    expect(loopSummary({ multiInstanceType: 'parallel', collection: 'orders' }))
      .toBe('Runs once for each item in orders, all at the same time.');
  });

  it('says what a sequential loop goes through', () => {
    expect(loopSummary({ multiInstanceType: 'sequential', collection: 'orders' }))
      .toBe('Runs once for each item in orders, one after another.');
  });

  it('counts, when there is no list', () => {
    expect(loopSummary({ multiInstanceType: 'sequential', loopCardinality: 3 }))
      .toBe('Runs 3 times, one after another.');
    expect(loopSummary({ multiInstanceType: 'parallel', loopCardinality: 1 }))
      .toBe('Runs once, all at the same time.');
  });

  it('goes by the list when both are set, as the engine does', () => {
    expect(loopSummary({ multiInstanceType: 'parallel', collection: 'orders', loopCardinality: 3 }))
      .toBe('Runs once for each item in orders, all at the same time.');
  });

  it('names the item each run sees, when there is a list', () => {
    expect(loopSummary({ multiInstanceType: 'parallel', collection: 'orders', elementVariable: 'order' }))
      .toBe('Runs once for each item in orders, all at the same time. Each run sees its item as order.');
    // With only a count there is no item to name.
    expect(loopSummary({ multiInstanceType: 'parallel', loopCardinality: 2, elementVariable: 'order' }))
      .toBe('Runs 2 times, all at the same time.');
  });

  it('says what ends it, when a condition does', () => {
    expect(loopSummary({
      multiInstanceType: 'parallel',
      collection: 'bids',
      completionCondition: 'nrOfCompletedInstances >= 2',
    })).toBe('Runs once for each item in bids, all at the same time. Moves on once this is true: nrOfCompletedInstances >= 2.');
  });

  it('says so when a loop names nothing to go through', () => {
    // The engine finds nothing to repeat over and runs the step once.
    expect(loopSummary({ multiInstanceType: 'parallel' }))
      .toBe('Set to repeat, but it names no list and no count, so it runs once.');
  });

  it.each(['loop', 'constructor'])('shows a type this editor does not offer, %s, rather than hiding it', (type) => {
    // The engine treats any type but "none" as a loop, so this is in effect.
    expect(loopSummary({ multiInstanceType: type, collection: 'orders' }))
      .toBe(`Set to repeat once for each item in orders as "${type}", which this editor does not recognise.`);
  });

  it('is what decides whether basic mode shows the loop', () => {
    expect(advancedVisibility(false, loopSummary({ multiInstanceType: 'none' }))).toBe('hidden');
    expect(advancedVisibility(false, loopSummary({ multiInstanceType: 'parallel', collection: 'orders' }))).toBe('summary');
    expect(advancedVisibility(true, loopSummary({}))).toBe('edit');
  });
});

/**
 * Loop settings follow the same rule on every kind of task.
 *
 * A user or service task hid them in basic mode even while the step ran once
 * per item, and a script task showed the full editor to everybody. Three
 * panels, three answers to one question.
 */
describe('every panel with loop settings', () => {
  const panels = readdirSync(join(SRC, 'components', 'properties'))
    .filter((file) => file.endsWith('.tsx'))
    .map((file) => [file, readSource(join('components', 'properties', file))] as const)
    .filter(([, source]) => source.includes('<MultiInstanceConfig'));

  it('includes the three task panels that offer them', () => {
    // Guards the scan itself: a renamed component would otherwise match
    // nothing, and every check below would pass having checked nothing.
    expect(panels.map(([file]) => file)).toEqual(
      expect.arrayContaining(['ScriptTaskConfig.tsx', 'ServiceTaskConfig.tsx', 'UserTaskConfig.tsx']),
    );
  });

  it.each(panels)('%s offers the editor only where it may be edited', (_file, source) => {
    const editors = source.match(/<MultiInstanceConfig/g) ?? [];
    const gated = source.match(/=== 'edit' && \(?\s*<MultiInstanceConfig/g) ?? [];

    expect(gated.length).toBe(editors.length);
  });

  it.each(panels)('%s summarises a loop in effect in basic mode', (_file, source) => {
    expect(source.includes('loopSummary(data)')).toBe(true);
  });
});

/** The opening tag of each control whose `checked` is bound to the flag. */
function flagControls(source: string): string[] {
  return [...source.matchAll(/checked=\{expertMode\}/g)].map((match) =>
    openingTag(source, source.lastIndexOf('<', match.index)),
  );
}

/** A JSX opening tag, skipping the `>` of any arrow inside its braces. */
function openingTag(source: string, start: number): string {
  let depth = 0;
  for (let index = start; index < source.length; index++) {
    const character = source[index];
    if (character === '{') depth++;
    if (character === '}') depth--;
    if (character === '>' && depth === 0) return source.slice(start, index + 1);
  }
  return source.slice(start);
}

/** What a screen reader calls the control: its aria-label, or else its label. */
function accessibleName(tag: string): string | undefined {
  return /aria-label="([^"]*)"/.exec(tag)?.[1] ?? /\slabel="([^"]*)"/.exec(tag)?.[1];
}

/**
 * The switch for the flag has one name wherever it appears.
 *
 * The property panel's read "BPMN names" while it flipped the whole flag: the
 * raw schema, the API example and every advanced setting. The note beside it
 * said to toggle "Expert Mode" at the top, and nothing there had that name.
 */
describe('the expert-mode switch', () => {
  it.each([
    ['components/shell/AppHeader.tsx'],
    ['pages/Settings.tsx'],
    ['components/PropertyPanel.tsx'],
  ])('is called Expert mode in %s', (path) => {
    const names = flagControls(readSource(path)).map(accessibleName);

    expect(names.length).toBeGreaterThan(0);
    expect(names).toEqual(names.map(() => 'Expert mode'));
  });
});

/**
 * The raw schema editor keeps what is typed.
 *
 * It rebuilt its text from the step's settings on every keystroke and applied
 * only text that parsed, so a keystroke that left the JSON invalid vanished as
 * it was typed. The first keystroke of any new key does that, so no key could
 * be added at all.
 */
describe('the raw schema editor', () => {
  type Settings = Record<string, unknown>;

  const step: Settings = { label: 'Approve', nodeType: 'userTask', assignee: 'ana' };

  /** What the designer does with an update: merges it into what is there. */
  function applied(current: Settings, patch: Settings | undefined): Settings {
    return patch === undefined ? current : { ...current, ...patch };
  }

  /** Types each text in turn, as the designer would apply it. */
  function typing(start: Settings, texts: string[]): { settings: Settings; draft: RawDraft | null; shown: string[] } {
    let settings = start;
    let draft: RawDraft | null = null;
    const shown: string[] = [];
    for (const text of texts) {
      const edit = editRawSettings(settings, text);
      settings = applied(settings, edit.patch);
      draft = edit.draft;
      shown.push(rawEditorView(settings, draft).text);
    }
    return { settings, draft, shown };
  }

  /** The settings as saving sees them: a key set to undefined is left out. */
  const saved = (settings: Settings) => JSON.parse(JSON.stringify(settings)) as Settings;

  it('shows the settings until something is typed', () => {
    expect(rawEditorView(step, null)).toEqual({ text: JSON.stringify(step, null, 2) });
  });

  it('can add a key, one keystroke at a time', () => {
    const start = JSON.stringify(step, null, 2);
    const at = start.lastIndexOf('\n}');
    const addition = ',\n  "priority": 5';
    const texts = [...addition].map((_, index) => start.slice(0, at) + addition.slice(0, index + 1) + start.slice(at));

    const { settings, shown } = typing(step, texts);

    expect(shown).toEqual(texts);
    expect(settings.priority).toBe(5);
  });

  it('says why it has not applied text that does not parse', () => {
    const { draft, patch } = editRawSettings(step, '{ "label": "Approve", ');
    const view = rawEditorView(step, draft);

    expect(patch).toBeUndefined();
    expect(view.problem).toStartWith('This is not valid JSON yet, so it has not been applied.');
  });

  it('keeps the text as typed once it applies, in whatever order', () => {
    const typed = '{"assignee": "bo", "label": "Approve", "nodeType": "userTask"}';
    const { settings, shown } = typing(step, [typed]);

    expect(settings.assignee).toBe('bo');
    expect(shown).toEqual([typed]);
    expect(rawEditorView(settings, editRawSettings(step, typed).draft).problem).toBeUndefined();
  });

  it('removes a key that is deleted', () => {
    const { settings } = typing(step, ['{"label": "Approve", "nodeType": "userTask"}']);

    expect(saved(settings)).toEqual({ label: 'Approve', nodeType: 'userTask' });
  });

  it('renames a key without leaving the names it passed through', () => {
    // Every step of the rename parses, so each is applied; without clearing
    // the key it replaces, "assigne", "assign" and the rest would all be left.
    const names = ['assigne', 'assign', 'assig', 'assi', 'ass', 'as', 'a', '', 'o', 'ow', 'own', 'owne', 'owner'];
    const texts = names.map((name) => `{"label": "Approve", "nodeType": "userTask", "${name}": "ana"}`);

    const { settings } = typing(step, texts);

    expect(saved(settings)).toEqual({ label: 'Approve', nodeType: 'userTask', owner: 'ana' });
  });

  it('leaves alone what JSON cannot show', () => {
    const onPick = () => undefined;
    const { settings } = typing({ ...step, onPick }, ['{"label": "Approve"}']);

    expect(settings.onPick).toBe(onPick);
  });

  it('applies nothing when only the layout changes', () => {
    expect(editRawSettings(step, JSON.stringify(step)).patch).toBeUndefined();
  });

  it.each([
    ['a list', '[]'],
    ['a string', '"approve"'],
    ['a number', '5'],
    ['null', 'null'],
  ])('refuses %s, which is not a set of settings', (_name, text) => {
    // Merged into the step, a string would scatter its letters across the
    // settings as keys "0", "1", "2" and so on.
    const { draft, patch } = editRawSettings(step, text);

    expect(patch).toBeUndefined();
    expect(rawEditorView(step, draft).problem).toBeTruthy();
  });

  it('refuses an empty box rather than clearing every setting', () => {
    const { draft, patch } = editRawSettings(step, '');

    expect(patch).toBeUndefined();
    expect(rawEditorView(step, draft).text).toBe('');
    expect(rawEditorView(step, draft).problem).toBeTruthy();
  });

  it('gives way to a change made elsewhere, rather than undoing it', () => {
    // The name field sits beside this editor, and a quick fix can change the
    // step while it is open. A draft typed against the old settings would
    // write them back on its next keystroke.
    const { draft } = editRawSettings(step, '{ "label": "Approve", ');
    const renamed = { ...step, label: 'Approve the expense' };

    expect(rawEditorView(renamed, draft)).toEqual({ text: JSON.stringify(renamed, null, 2) });
  });
});
