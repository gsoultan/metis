import { describe, expect, it } from 'bun:test';
import { useState } from 'react';

import { playScript } from '../testing/playScript';
import { useRawSettingsDraft } from './useRawSettingsDraft';

type Settings = Record<string, unknown>;

/** The editor over a step's settings, its updates merged as the designer merges them. */
function useEditorOver(start: Settings) {
  const [settings, setSettings] = useState(start);
  const editor = useRawSettingsDraft(settings, (patch) => setSettings((current) => ({ ...current, ...patch })));
  return { settings, setSettings, editor };
}

type Subject = ReturnType<typeof useEditorOver>;

const step: Settings = { label: 'Approve', nodeType: 'userTask', assignee: 'ana' };
const pretty = (settings: Settings) => JSON.stringify(settings, null, 2);
const shown = ({ editor }: Subject) => ({ text: editor.text, problem: editor.problem });

describe('the raw schema editor', () => {
  it('keeps what is typed while it does not parse', () => {
    const seen = playScript(() => useEditorOver(step), [({ editor }) => editor.change('{ "label": "Approve", ')]);

    expect(seen[1].editor.text).toBe('{ "label": "Approve", ');
    expect(seen[1].editor.problem).toStartWith('This is not valid JSON yet, so it has not been applied.');
  });

  it('keeps what is typed once it applies, in whatever order', () => {
    const typed = '{"assignee": "bo", "label": "Approve", "nodeType": "userTask"}';
    const seen = playScript(() => useEditorOver(step), [({ editor }) => editor.change(typed), () => undefined]);

    expect(seen[2].settings.assignee).toBe('bo');
    expect(shown(seen[2])).toEqual({ text: typed });
  });

  /**
   * A draft that a change made elsewhere has replaced is gone, and stays gone.
   *
   * It used to be set aside rather than dropped. Typing invalid JSON, renaming
   * the step, then renaming it back brought the old text and its error back,
   * long after the person had moved on from it.
   */
  it('drops a draft another change has replaced, and does not bring it back', () => {
    const renamed = { ...step, label: 'Approve the expense' };
    const seen = playScript(() => useEditorOver(step), [
      ({ editor }) => editor.change('{ "label": "Approve", '),
      ({ setSettings }) => setSettings(renamed),
      ({ setSettings }) => setSettings(step),
    ]);

    expect(shown(seen[2])).toEqual({ text: pretty(renamed) });
    expect(shown(seen[3])).toEqual({ text: pretty(step) });
  });
});
