import { Textarea } from '@mantine/core';

import { useRawSettingsDraft } from '../hooks/useRawSettingsDraft';

/** An element's settings as JSON, for someone who knows what they hold. */
export function RawSchemaEditor({
  settings,
  onApply,
}: {
  settings: Record<string, unknown>,
  onApply: (patch: Record<string, unknown>) => void,
}) {
  const editor = useRawSettingsDraft(settings, onApply);

  return (
    <Textarea
      label="Raw Node Schema"
      description="Changes apply as soon as the JSON is valid"
      placeholder="Raw JSON data"
      minRows={40}
      autosize
      maxRows={80}
      styles={{
        input: {
          fontFamily: 'monospace',
          fontSize: '11px',
          backgroundColor: 'var(--mantine-color-dark-8)',
          color: 'var(--mantine-color-gray-3)'
        }
      }}
      value={editor.text}
      error={editor.problem}
      onChange={(event) => editor.change(event.currentTarget.value)}
    />
  );
}
