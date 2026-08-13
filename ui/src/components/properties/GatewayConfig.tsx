import { Alert, Select, Stack, Text } from '@mantine/core';

import type { NodeConfigProps } from '../PropertyPanel';
import { asText } from '../../types/bpmn';
import { PropertySection } from './PropertySection';

/**
 * A fork in the process: it reads the data and chooses which way to go.
 *
 * The fallback path matters more than its old name — "Default Flow Path" —
 * suggested. Without one, a value none of the conditions expect stops the
 * process dead, which is the commonest way a working process breaks later.
 */
export function GatewayConfig({ data, onUpdate, selectedNode, edges = [] }: NodeConfigProps) {
  const outgoing = selectedNode ? edges.filter((edge) => edge.source === selectedNode.id) : [];
  const options = outgoing.map((edge) => ({
    value: edge.id,
    label: typeof edge.label === 'string' && edge.label ? edge.label : `Path to ${edge.target}`,
  }));

  return (
    <Stack gap="xl">
      <PropertySection
        title="If nothing matches"
        hint="Each path leaving this step has its own condition. This one is taken when none of them are true."
      >
        <Select
          label="Fall back to"
          placeholder={options.length ? 'Stop with an error' : 'Draw the paths first'}
          data={options}
          value={asText(data.defaultFlow)}
          onChange={(value) => onUpdate({ defaultFlow: value })}
          disabled={options.length === 0}
          clearable
          searchable
        />

        {options.length === 0 ? (
          <Text size="xs" c="dimmed">
            Connect this step to what comes next, then choose which path to fall back to.
          </Text>
        ) : (
          <Alert variant="light" color={asText(data.defaultFlow) ? 'gray' : 'orange'} p="xs">
            <Text size="xs">
              {asText(data.defaultFlow)
                ? 'A value none of the conditions expect will take this path.'
                : 'With no fallback, a value none of the conditions expect stops the process here and raises an incident. That is deliberate — it is safer than guessing a path — but it is worth choosing one.'}
            </Text>
          </Alert>
        )}
      </PropertySection>
    </Stack>
  );
}
