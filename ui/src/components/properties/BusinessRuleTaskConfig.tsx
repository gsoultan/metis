import { Alert, NumberInput, Select, Stack, Text } from '@mantine/core';

import { useDecisions } from '../../hooks/useProcess';
import type { NodeConfigProps } from '../PropertyPanel';
import { asNumber, asText, asTextMap } from '../../types/bpmn';
import { MappingTable } from './CommonProperties';
import { PropertySection } from './PropertySection';

/**
 * A step that looks something up in a decision table.
 *
 * The table decides; this step says which one to ask and what to call the
 * answer. That is the whole of it, so the form says it in those terms rather
 * than as "DMN Configuration" and a "Decision Key".
 */
export function BusinessRuleTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const { data: decisionsData } = useDecisions();
  const decisions = decisionsData?.decisions ?? [];
  const chosen = asText(data.decision_key);
  const version = asNumber(data.decision_version);

  return (
    <Stack gap="xl">
      <PropertySection
        title="The decision to apply"
        hint="Its results are added to the process, under the names its result columns have."
      >
        <Select
          label="Decision table"
          placeholder={decisions.length ? 'Choose a decision' : 'No decisions in this project yet'}
          data={decisions.map((decision) => ({
            value: decision.key,
            label: decision.name || decision.key,
          }))}
          value={chosen}
          onChange={(value) => onUpdate({ decision_key: value })}
          disabled={decisions.length === 0}
          searchable
          clearable
        />

        <NumberInput
          label="Version"
          description="Leave at 0 to always use the current one, which is almost always what you want."
          min={0}
          value={version}
          onChange={(value) => onUpdate({ decision_version: Number(value) || 0 })}
        />

        {!chosen && (
          <Alert variant="light" color="orange" p="xs">
            <Text size="xs">
              Without a decision this step does nothing and the process carries straight on.
            </Text>
          </Alert>
        )}
      </PropertySection>

      <PropertySection
        title="If the names differ"
        hint="Only needed when the table's columns are named differently from your data. Leave both empty otherwise."
      >
        <MappingTable
          title="SENDING IN"
          sourceLabel="Your variable"
          targetLabel="The table's input"
          mapping={asTextMap(data.input_mapping)}
          onUpdate={(mapping) => onUpdate({ input_mapping: mapping })}
        />

        <MappingTable
          title="STORING THE ANSWER"
          sourceLabel="The table's result"
          targetLabel="Store it as"
          mapping={asTextMap(data.output_mapping)}
          onUpdate={(mapping) => onUpdate({ output_mapping: mapping })}
        />
      </PropertySection>
    </Stack>
  );
}
