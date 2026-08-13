import { Alert, Stack, Text, TextInput } from '@mantine/core';

import type { NodeConfigProps } from '../PropertyPanel';
import { asText } from '../../types/bpmn';
import { PropertySection } from './PropertySection';

/**
 * Something a person does away from the system, then confirms here.
 */
export function ManualTaskConfig({ data, onUpdate }: NodeConfigProps) {
  return (
    <Stack gap="xl">
      <PropertySection
        title="Who does this"
        hint="Leave empty if anyone can pick it up."
      >
        <TextInput
          label="Person or role"
          placeholder="e.g. Warehouse manager"
          value={asText(data.actor)}
          onChange={(e) => onUpdate({ actor: e.target.value })}
        />
      </PropertySection>

      <Alert variant="light" color="gray" p="sm">
        <Text size="xs">
          The process waits here until someone confirms the work is done. Nothing is
          asked of them beyond that — use a form step if you need them to record something.
        </Text>
      </Alert>
    </Stack>
  );
}
