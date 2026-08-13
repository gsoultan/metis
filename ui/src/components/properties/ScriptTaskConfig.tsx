import { Button, Group, Select, Stack, Text, Textarea, Tooltip } from '@mantine/core';
import { Play } from 'lucide-react';
import { useState } from 'react';

import { VariablePicker } from '../LowCodeComponents';
import { asText } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { MultiInstanceConfig, ScriptTestModal } from './CommonProperties';
import { PropertySection } from './PropertySection';
import { SCRIPT_TEMPLATES } from './scriptTemplates';

/**
 * A little code, run by the engine, between two steps.
 *
 * The editor is the point of the form, so it gets the room. The language and
 * where to put the answer sit above it in one line rather than as a section of
 * their own titled "Script Runtime".
 */
export function ScriptTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const [testModalOpened, setTestModalOpened] = useState(false);
  const script = asText(data.script);

  return (
    <Stack gap="xl">
      <PropertySection title="The script" hint="Runs as soon as the process reaches this step.">
        <Group grow align="flex-start">
          <Select
            label="Language"
            data={[
              { value: 'javascript', label: 'JavaScript' },
              { value: 'python', label: 'Python' },
              { value: 'groovy', label: 'Groovy' },
            ]}
            value={asText(data.scriptFormat, 'javascript')}
            onChange={(val) => onUpdate({ scriptFormat: val })}
            allowDeselect={false}
          />
          <VariablePicker
            label="Store the answer as"
            placeholder="e.g. total"
            value={asText(data.resultVariable)}
            onChange={(val) => onUpdate({ resultVariable: val })}
          />
        </Group>

        <Textarea
          placeholder="// the process variables are available by name, e.g. if (amount > 100) …"
          description="Use setVar(name, value) to add or change a variable."
          minRows={12}
          autosize
          styles={{
            input: {
              fontFamily: 'var(--mantine-font-family-monospace)',
              fontSize: 12,
              lineHeight: 1.6,
            },
          }}
          value={script}
          onChange={(e) => onUpdate({ script: e.target.value })}
        />

        <Group justify="space-between" align="center">
          <Group gap={6}>
            <Text size="xs" c="dimmed">Start from:</Text>
            {SCRIPT_TEMPLATES.map((template) => (
              <Tooltip key={template.name} label={template.description} withArrow>
                <Button
                  variant="default"
                  size="compact-xs"
                  radius="xl"
                  onClick={() => onUpdate({ script: script + (script ? '\n' : '') + template.code })}
                >
                  {template.name}
                </Button>
              </Tooltip>
            ))}
          </Group>

          <Button
            size="compact-xs"
            variant="light"
            leftSection={<Play size={12} />}
            onClick={() => setTestModalOpened(true)}
            disabled={!script}
          >
            Try it
          </Button>
        </Group>
      </PropertySection>

      <MultiInstanceConfig data={data} onUpdate={onUpdate} />

      <ScriptTestModal
        opened={testModalOpened}
        onClose={() => setTestModalOpened(false)}
        script={script}
        format={asText(data.scriptFormat, 'javascript')}
      />
    </Stack>
  );
}
