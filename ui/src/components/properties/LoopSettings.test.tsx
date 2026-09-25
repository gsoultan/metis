import { afterEach, describe, expect, it, mock } from 'bun:test';
import type { ComponentType } from 'react';

import { appStoreDouble, resetAppStore, setAppState } from '../../testing/appStoreDouble';
import { labelledControl, visibleText } from '../../testing/markup';
import { renderMarkup } from '../../testing/renderMarkup';
import type { BPMNNodeData } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';

mock.module('../../store/useAppStore', () => ({ useAppStore: appStoreDouble }));

const { ScriptTaskConfig } = await import('./ScriptTaskConfig');
const { ServiceTaskConfig } = await import('./ServiceTaskConfig');
const { UserTaskConfig } = await import('./UserTaskConfig');

const PANELS: Array<[string, ComponentType<NodeConfigProps>]> = [
  ['ScriptTaskConfig', ScriptTaskConfig],
  ['ServiceTaskConfig', ServiceTaskConfig],
  ['UserTaskConfig', UserTaskConfig],
];

afterEach(resetAppStore);

function render(Panel: ComponentType<NodeConfigProps>, settings: Record<string, unknown>, expertMode: boolean): string {
  setAppState({ expertMode });
  const data = { label: 'Check', nodeType: 'userTask', ...settings } as BPMNNodeData;
  return renderMarkup(<Panel data={data} onUpdate={() => undefined} />);
}

const THREE_AT_ONCE = { multiInstanceType: 'parallel', loopCardinality: 3 };

/**
 * A step that repeats says so in both modes, and expert mode shows more, not less.
 *
 * Expert mode replaced the sentence with the loop editor, and the editor had no
 * field for a count and took any kind of repeat it did not know for no repeat
 * at all. A step set to run three times said "Runs 3 times, all at the same
 * time" in basic mode, and in expert mode showed empty fields and no 3.
 */
describe.each(PANELS)('%s', (_name, Panel) => {
  it('says how often a step runs in basic mode', () => {
    const text = visibleText(render(Panel, THREE_AT_ONCE, false));

    expect(text).toContain('Runs 3 times, all at the same time.');
    expect(text).toContain('Turn on Expert mode to change it.');
  });

  it('still says it in expert mode', () => {
    expect(visibleText(render(Panel, THREE_AT_ONCE, true))).toContain('Runs 3 times, all at the same time.');
  });

  it('shows the count in expert mode, where it can be changed', () => {
    expect(labelledControl(render(Panel, THREE_AT_ONCE, true), 'Loop Cardinality')?.value).toBe('3');
  });

  it('shows a kind of repeat the editor does not recognise as it is, not as no repeat', () => {
    const html = render(Panel, { multiInstanceType: 'standard', collection: 'orders' }, true);

    expect(labelledControl(html, 'Multi-instance')).toHaveProperty('checked');
    expect(labelledControl(html, 'Execution')?.value).toContain('standard');
  });
});
