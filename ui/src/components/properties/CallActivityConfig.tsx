import { Alert, Button, Group, NumberInput, Select, Stack, Text } from '@mantine/core';

import { useDefinitions, useSubProcesses } from '../../hooks/useProcess';
import { asNumber, asText, asTextMap } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { MappingTable } from './CommonProperties';
import { PropertySection } from './PropertySection';

/**
 * A step that runs another process and waits for it.
 *
 * "Check the supplier" is a process in its own right; three others call it.
 * This says which one and, if the two use different names for the same thing,
 * how to translate between them.
 */
export function CallActivityConfig({ data, nodeId, onUpdate, instanceId, onViewInstance }: NodeConfigProps) {
  const { data: defsData } = useDefinitions();
  const { data: subProcessesData } = useSubProcesses(instanceId || null);

  const definitions = defsData?.definitions ?? [];
  const subProcesses = subProcessesData?.instances ?? [];

  // The call activity that started it is a nested object, not an id.
  const activeSubProcess = subProcesses.find((s) => s.parent_node?.id === (nodeId ?? ''));
  const chosen = asText(data.called_process_key);

  return (
    <Stack gap="xl">
      {activeSubProcess && onViewInstance && (
        <Alert variant="light" color="teal" p="sm">
          <Group justify="space-between" wrap="nowrap">
            <Text size="sm">This step is running right now.</Text>
            <Button
              size="compact-xs"
              variant="light"
              color="teal"
              onClick={() => onViewInstance(activeSubProcess.id, activeSubProcess.definition?.id ?? '')}
            >
              Open it
            </Button>
          </Group>
        </Alert>
      )}

      <PropertySection
        title="The process to run"
        hint="This one waits until that one finishes."
      >
        <Select
          label="Process"
          placeholder={definitions.length ? 'Choose a process' : 'No other processes yet'}
          data={definitions.map((d) => ({ value: d.key, label: d.name || d.key }))}
          value={chosen}
          onChange={(val) => onUpdate({ called_process_key: val })}
          disabled={definitions.length === 0}
          searchable
          clearable
        />

        <NumberInput
          label="Version"
          description="Leave at 0 to always use the current one."
          min={0}
          value={asNumber(data.called_process_version)}
          onChange={(val) => onUpdate({ called_process_version: Number(val) || 0 })}
        />

        {!chosen && (
          <Alert variant="light" color="orange" p="xs">
            <Text size="xs">
              Without a process named, this step stops the process when it is reached.
            </Text>
          </Alert>
        )}
      </PropertySection>

      <PropertySection
        title="Data between the two"
        hint="Leave both empty to hand over everything and take back everything."
      >
        <MappingTable
          title="GIVING IT"
          sourceLabel="Your variable"
          targetLabel="Its variable"
          mapping={asTextMap(data.in_mapping)}
          onUpdate={(m) => onUpdate({ in_mapping: m })}
        />
        <MappingTable
          title="TAKING BACK"
          sourceLabel="Its variable"
          targetLabel="Store it as"
          mapping={asTextMap(data.out_mapping)}
          onUpdate={(m) => onUpdate({ out_mapping: m })}
        />
      </PropertySection>
    </Stack>
  );
}
