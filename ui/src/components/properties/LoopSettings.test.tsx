import { afterEach, describe, expect, it, mock } from 'bun:test';
import type { ComponentType } from 'react';

import { NODE_VOCABULARY } from '../../domain/bpmnVocabulary';
import { appStoreDouble, resetAppStore, setAppState } from '../../testing/appStoreDouble';
import { labelledControl, visibleText } from '../../testing/markup';
import { renderMarkup } from '../../testing/renderMarkup';
import type { BPMNNodeData } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';

mock.module('../../store/useAppStore', () => ({ useAppStore: appStoreDouble }));

const { CONFIG_REGISTRY } = await import('./nodeConfigRegistry');

/**
 * Every kind of step BPMN calls an activity: a task of any kind, a call
 * activity, a sub-process. Any of them can repeat, and the engine runs a loop
 * on any of them, so each one's panel has to say so.
 *
 * Found by name rather than listed, so a new kind of task is checked without
 * anyone remembering to add it here. The guard before this one checked only
 * the panels that already had loop settings, so it could not notice the four
 * that had none.
 */
const ACTIVITY_KINDS = Object.entries(NODE_VOCABULARY)
  .filter(([, entry]) => / Task$|^Call Activity$|^Sub-Process$/.test(entry.bpmnName))
  .map(([kind]) => kind);

afterEach(resetAppStore);

function render(kind: string, settings: Record<string, unknown>, expertMode: boolean): string {
  const Panel: ComponentType<NodeConfigProps> = CONFIG_REGISTRY[kind];
  setAppState({ expertMode });
  const data = { label: 'Check', nodeType: kind, ...settings } as BPMNNodeData;
  return renderMarkup(<Panel data={data} onUpdate={() => undefined} nodeId="check" nodes={[]} edges={[]} />);
}

const THREE_AT_ONCE = { multiInstanceType: 'parallel', loopCardinality: 3 };

it('finds every kind of activity', () => {
  // Guards the search itself: a renamed entry would otherwise match nothing,
  // and every check below would pass having checked nothing.
  expect(ACTIVITY_KINDS).toEqual(expect.arrayContaining([
    'userTask', 'serviceTask', 'scriptTask', 'manualTask', 'businessRuleTask', 'callActivity', 'subProcess',
  ]));
});

it.each(ACTIVITY_KINDS)('has a panel for a %s', (kind) => {
  expect(CONFIG_REGISTRY[kind]).toBeDefined();
});

/**
 * A step that repeats says so in both modes, and expert mode shows more, not less.
 *
 * Expert mode replaced the sentence with the loop editor, and the editor had no
 * field for a count and took any kind of repeat it did not know for no repeat
 * at all. A step set to run three times said "Runs 3 times, all at the same
 * time" in basic mode, and in expert mode showed empty fields and no 3.
 */
describe.each(ACTIVITY_KINDS)('the panel for a %s', (kind) => {
  it('says how often the step runs in basic mode', () => {
    const text = visibleText(render(kind, THREE_AT_ONCE, false));

    expect(text).toContain('Runs 3 times, all at the same time.');
    expect(text).toContain('Turn on Expert mode to change it.');
  });

  it('still says it in expert mode', () => {
    expect(visibleText(render(kind, THREE_AT_ONCE, true))).toContain('Runs 3 times, all at the same time.');
  });

  it('shows the count in expert mode, where it can be changed', () => {
    expect(labelledControl(render(kind, THREE_AT_ONCE, true), 'Loop Cardinality')?.value).toBe('3');
  });

  it('shows a kind of repeat the editor does not recognise as it is, not as no repeat', () => {
    const html = render(kind, { multiInstanceType: 'standard', collection: 'orders' }, true);

    expect(labelledControl(html, 'Multi-instance')).toHaveProperty('checked');
    expect(labelledControl(html, 'Execution')?.value).toContain('standard');
  });

  it('says nothing about repeating in basic mode when the step runs once', () => {
    expect(visibleText(render(kind, {}, false))).not.toContain('Repeats');
  });
});
