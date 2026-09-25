import { describe, expect, it } from 'bun:test';

import { editRawSettings, rawEditorView, type RawDraft } from './rawSettings';

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

  it('refuses one setting under two names, which would save only one of them', () => {
    // httpUrl is the panel's name for http_url. With both in the text the
    // editor would show two addresses and the step would save one.
    const { draft, patch } = editRawSettings(step, '{"httpUrl": "https://a.example", "http_url": "https://b.example"}');

    expect(patch).toBeUndefined();
    expect(rawEditorView(step, draft).problem).toContain('"httpUrl" and "http_url" are the same setting');
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
