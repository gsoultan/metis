import { Alert, Button, Code, Group, JsonInput, Select, Stack, Text } from '@mantine/core';
import { Wand2 } from 'lucide-react';

import type { NodeConfigProps } from '../PropertyPanel';
import { asText } from '../../types/bpmn';
import { PropertySection } from './PropertySection';

/** A starting point, so the field is never a blank box. */
const EXAMPLE = JSON.stringify(
  { amount: 2400, currency: 'GBP', description: 'Conference tickets', submittedBy: 'alice' },
  null,
  2,
);

/**
 * How a process begins, and what it begins with.
 *
 * The sample data is the useful part. A process is a sequence of steps reading
 * and writing one bag of variables, and until the bag has something in it the
 * diagram cannot say what any step will see — so every question about data had
 * to be answered by starting a real instance and looking at it afterwards.
 *
 * What is typed here is not sent anywhere and does not change how the process
 * runs. It is what the panel traces through the diagram, so each step can show
 * the values it will be working with rather than a list of names.
 */
export function StartEventConfig({ data, onUpdate }: NodeConfigProps) {
  const sample = asText(data.sampleData);
  const invalid = sample.trim() !== '' && !isJSONObject(sample);

  return (
    <Stack gap="xl">
      <PropertySection title="How it starts" hint="What causes a new process to begin.">
        <Select
          data={[
            { value: 'manual', label: 'Someone starts it' },
            { value: 'timer', label: 'On a schedule' },
            { value: 'message', label: 'Another system sends a message' },
          ]}
          value={asText(data.startedBy, 'manual')}
          onChange={(value) => onUpdate({ startedBy: value ?? 'manual' })}
          allowDeselect={false}
        />
      </PropertySection>

      <PropertySection
        title="Sample data"
        hint="The data a process starts with. Used to show what each step will see — it is not sent anywhere and does not affect how the process runs."
      >
        <JsonInput
          placeholder={EXAMPLE}
          value={sample}
          onChange={(value) => onUpdate({ sampleData: value })}
          validationError="This is not valid JSON yet"
          formatOnBlur
          autosize
          minRows={6}
          error={invalid ? 'Sample data should be an object, like the example' : undefined}
        />

        <Group justify="space-between" align="center">
          <Text size="xs" c="dimmed">
            Names here are the names steps read, so <Code>amount</Code> is what a decision looks up.
          </Text>
          {sample.trim() === '' && (
            <Button
              size="compact-xs"
              variant="light"
              leftSection={<Wand2 size={12} />}
              onClick={() => onUpdate({ sampleData: EXAMPLE })}
            >
              Use an example
            </Button>
          )}
        </Group>

        {sample.trim() !== '' && !invalid && (
          <Alert variant="light" color="teal" p="xs">
            <Text size="xs">
              Select any step to see what it receives and what it leaves behind.
            </Text>
          </Alert>
        )}
      </PropertySection>
    </Stack>
  );
}

function isJSONObject(value: string): boolean {
  try {
    const parsed: unknown = JSON.parse(value);
    return !!parsed && typeof parsed === 'object' && !Array.isArray(parsed);
  } catch {
    return false;
  }
}
