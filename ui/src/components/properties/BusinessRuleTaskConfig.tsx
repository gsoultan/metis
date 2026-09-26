import { Alert, NumberInput, Select, Stack, Text } from '@mantine/core';

import { decisionOptions } from '../../domain/decisionPicker';
import { useDecisionSummaries } from '../../hooks/useDecisions';
import type { NodeConfigProps } from '../PropertyPanel';
import { asNumber, asText, asTextMap } from '../../types/bpmn';
import { MappingTable } from './CommonProperties';
import { LoopSettings } from './LoopSettings';
import { PropertySection } from './PropertySection';

/**
 * A step that looks something up in a decision table.
 *
 * The table decides; this step says which one to ask and what to call the
 * answer. That is the whole of it, so the form says it in those terms rather
 * than as "DMN Configuration" and a "Decision Key".
 */
export function BusinessRuleTaskConfig({ data, onUpdate }: NodeConfigProps) {
  // Every decision of the project, once each: the picker was filled from the
  // first page of the list — 25 rows, a row per version — so a decision saved
  // twice was offered twice, which the Select refuses, and a decision past the
  // first 25 rows could not be chosen.
  const { data: listed } = useDecisionSummaries();
  const chosen = asText(data.decision_key);
  const options = decisionOptions(listed?.items ?? [], chosen);
  const version = asNumber(data.decision_version);

  return (
    <Stack gap="xl">
      <PropertySection
        title="The decision to apply"
        hint="Its results are added to the process, under the names its result columns have."
      >
        <Select
          label="Decision table"
          placeholder={options.length ? 'Choose a decision' : 'No decisions in this project yet'}
          data={options}
          value={chosen || null}
          onChange={(value) => onUpdate({ decision_key: value })}
          disabled={options.length === 0}
          searchable
          clearable
        />
        {listed?.truncated && (
          <Text size="xs" c="dimmed">
            This project has {listed.total} decisions; the list holds the first {listed.items.length}, in order of key.
          </Text>
        )}

        <NumberInput
          label="Version"
          description="Leave at 0 to always use the live version, which is almost always what you want."
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

      <LoopSettings data={data} onUpdate={onUpdate} />
    </Stack>
  );
}
