import {
  ActionIcon,
  Box,
  Button,
  Code,
  Group,
  Paper,
  PasswordInput,
  Select,
  Stack,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core';
import { Play, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { clearedStepFields, stepFieldPatch, stepFieldValue } from '../../domain/connectorStep';
import { advancedVisibility, CHANGE_IN_EXPERT_MODE, implementationOptions, loopSummary } from '../../domain/disclosure';
import { serviceImplementation, storedWebAddress, storedWorkerTopic } from '../../domain/serviceImplementation';
import { useConnectors } from '../../hooks/useConnectors';
import { useAppStore } from '../../store/useAppStore';
import { asText, asTextMap } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { ConnectorCatalog, MappingTable, MultiInstanceConfig, NodeTestModal } from './CommonProperties';
import { ConnectorStepFields } from './ConnectorStepFields';
import { PropertySection } from './PropertySection';

/**
 * A step that calls another system.
 *
 * The first choice decides everything after it, so it is a choice with an
 * explanation rather than a dropdown of protocol names: "HTTP Push (Remote
 * Endpoint)" and "External Worker (Long Polling)" describe the mechanism to
 * someone who already knows which one they want.
 */
export function ServiceTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const implementation = serviceImplementation(data as Record<string, unknown>);
  const { data: connectorsData } = useConnectors();
  const expertMode = useAppStore((state) => state.expertMode);
  const [testModalOpened, setTestModalOpened] = useState(false);

  const connectors = connectorsData?.connectors ?? [];
  const selectedConnector = connectors.find((c) => c.id === asText(data.connector_id));
  // A connector that asks the step for fields of its own — a database lookup's
  // query — has them drawn here.
  const stepSchema = selectedConnector?.node_schema ?? [];
  const takesStepFields = stepSchema.length > 0;

  const options = implementationOptions(expertMode, implementation);
  const script = implementation === 'script' ? advancedVisibility(expertMode, data.script) : 'hidden';
  const repeats = loopSummary(data);
  const loop = advancedVisibility(expertMode, repeats);

  return (
    <Stack gap="xl">
      <PropertySection title="What it calls" hint="Everything below follows from this.">
        <Select
          aria-label="Implementation"
          data={options.map(({ value, label }) => ({ value, label }))}
          value={implementation}
          onChange={(val) => onUpdate({ implementation: val })}
          allowDeselect={false}
        />
        <Text size="xs" c="dimmed">
          {options.find((option) => option.value === implementation)?.description}
        </Text>
      </PropertySection>

      {implementation === 'connector' && (
        !data.connector_id ? (
          <PropertySection title="Choose a connector">
            <ConnectorCatalog
              onSelect={(c) => onUpdate({ connector_id: c.id, connector_instance_id: '', ...clearedStepFields() })}
            />
          </PropertySection>
        ) : (
          <>
            <PropertySection title="Connector">
              {selectedConnector && (
                <Paper withBorder p="sm" radius="md">
                  <Group justify="space-between" wrap="nowrap">
                    <Box style={{ minWidth: 0 }}>
                      <Text size="sm" fw={600}>{selectedConnector.name}</Text>
                      <Text size="xs" c="dimmed" lineClamp={2}>{selectedConnector.description}</Text>
                    </Box>
                    <Group gap={4} wrap="nowrap">
                      <Button
                        size="compact-xs"
                        variant="light"
                        leftSection={<Play size={12} />}
                        onClick={() => setTestModalOpened(true)}
                      >
                        Try it
                      </Button>
                      <ActionIcon
                        aria-label="Remove this connector"
                        size="sm"
                        variant="subtle"
                        color="red"
                        onClick={() =>
                          onUpdate({ connector_id: undefined, connector_instance_id: undefined, ...clearedStepFields() })
                        }
                      >
                        <Trash2 size={14} />
                      </ActionIcon>
                    </Group>
                  </Group>
                </Paper>
              )}
            </PropertySection>

            {takesStepFields ? (
              <ConnectorStepFields schema={stepSchema} data={data as Record<string, unknown>} onUpdate={onUpdate} />
            ) : (
            <PropertySection
              title="If the names differ"
              hint="Leave these empty to send every process value and keep the whole answer. List anything, and only what is listed is sent or kept."
            >
              <MappingTable
                title="SENDING"
                sourceLabel="Their field"
                targetLabel="From the process"
                mapping={asTextMap(stepFieldValue(data as Record<string, unknown>, 'input_mapping'))}
                onUpdate={(m) => onUpdate(stepFieldPatch('input_mapping', m))}
              />
              <MappingTable
                title="RECEIVING"
                sourceLabel="Store it as"
                targetLabel="From their answer"
                mapping={asTextMap(stepFieldValue(data as Record<string, unknown>, 'output_mapping'))}
                onUpdate={(m) => onUpdate(stepFieldPatch('output_mapping', m))}
              />
            </PropertySection>
            )}
          </>
        )
      )}

      {implementation === 'push' && (
        <PropertySection title="Where to call" hint="The process waits for the answer before carrying on.">
          <TextInput
            label="Web address"
            placeholder="https://api.example.com/webhook"
            value={storedWebAddress(data as Record<string, unknown>)}
            // The older name goes as the address is edited, so clearing the
            // field clears the address rather than bringing the old one back.
            onChange={(e) => onUpdate({ httpUrl: e.target.value, url: undefined })}
          />
          <PasswordInput
            label="Access token"
            placeholder="Only if they need one"
            description="Sent as an Authorization header. Stored encrypted."
            value={asText(data.auth_token)}
            onChange={(e) => onUpdate({ auth_token: e.target.value })}
          />
        </PropertySection>
      )}

      {implementation === 'external' && (
        <PropertySection
          title="What to call it"
          hint="Your worker asks for work under this name, does it, and reports back. The process waits meanwhile."
        >
          <TextInput
            label="Topic"
            placeholder="e.g. process-invoice"
            value={storedWorkerTopic(data as Record<string, unknown>)}
            onChange={(e) => onUpdate({ externalTopic: e.target.value, topic: undefined })}
          />
        </PropertySection>
      )}

      {script === 'summary' && (
        <PropertySection title="The script" hint={CHANGE_IN_EXPERT_MODE}>
          <Code block>{asText(data.script)}</Code>
        </PropertySection>
      )}

      {script === 'edit' && (
        <PropertySection title="The script" hint="Runs here, with the process variables available to it.">
          <Textarea
            aria-label="Script"
            placeholder="// the process variables are in `vars`"
            minRows={10}
            autosize
            styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)', fontSize: 12 } }}
            value={asText(data.script)}
            onChange={(e) => onUpdate({ script: e.target.value })}
          />
          <Group justify="flex-end">
            <Button size="compact-xs" variant="light" leftSection={<Play size={12} />} onClick={() => setTestModalOpened(true)}>
              Try it
            </Button>
          </Group>
        </PropertySection>
      )}

      {loop === 'summary' && (
        <PropertySection title="Repeats" hint={CHANGE_IN_EXPERT_MODE}>
          <Text size="sm">{repeats}</Text>
        </PropertySection>
      )}

      {loop === 'edit' && <MultiInstanceConfig data={data} onUpdate={onUpdate} />}

      <NodeTestModal
        data={data}
        opened={testModalOpened}
        onClose={() => setTestModalOpened(false)}
      />
    </Stack>
  );
}
