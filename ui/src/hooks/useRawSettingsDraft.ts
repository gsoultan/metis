import { useState } from 'react';

import { editRawSettings, liveDraft, rawEditorView, type RawDraft } from '../domain/disclosure';

/** What the raw schema editor shows, and what typing in it does. */
export interface RawSettingsEditor {
  text: string;
  problem?: string;
  change: (text: string) => void;
}

/**
 * The raw schema editor's state: what has been typed and not yet applied.
 *
 * What is typed stays as typed and applies once it parses. The text used to be
 * rebuilt from the settings on every keystroke, so a keystroke that left the
 * JSON invalid vanished as it was typed, and no new key could be started.
 *
 * A draft that a change made elsewhere has replaced is dropped the moment it
 * is seen, in the render itself, as React has a component adjust its state to
 * new props. Only set aside, it came back, error and all, once the step was
 * back to the settings it was typed against.
 */
export function useRawSettingsDraft(
  settings: Record<string, unknown>,
  onApply: (patch: Record<string, unknown>) => void,
): RawSettingsEditor {
  const [draft, setDraft] = useState<RawDraft | null>(null);
  const live = liveDraft(settings, draft);
  if (live !== draft) setDraft(live);
  const view = rawEditorView(settings, live);

  return {
    ...view,
    change: (text) => {
      const edit = editRawSettings(settings, text);
      setDraft(edit.draft);
      if (edit.patch !== undefined) onApply(edit.patch);
    },
  };
}
