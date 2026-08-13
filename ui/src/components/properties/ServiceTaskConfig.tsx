import {
  ActionIcon,
  Box,
  Button,
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

import { useConnectors } from '../../hooks/useConnectors';
import { useAppStore } from '../../store/useAppStore';
import { asText, asTextMap } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { ConnectorCatalog, MappingTable, MultiInstanceConfig, NodeTestModal } from './CommonProperties';
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
  const implementation = asText(data.implementation, 'push');
  const { data: connectorsData } = useConnectors();
  const { expertMode } = useAppStore();
  const [testModalOpened, setTestModalOpened] = useState(false);

  const connectors = connectorsData?.connectors ?? [];
  const selectedConnector = connectors.find((c) => c.id === asText(data.connector_id));

  const options = [
    { value: 'push', label: 'Call a web address', description: 'We send the request and wait for the answer' },
    { value: 'connector', label: 'Use a connector', description: 'Slack, email and the rest, already set up' },
    { value: 'external', label: 'Let a worker pick it up', description: 'Your own program asks for work and reports back' },
    ...(expertMode
      ? [{ value: 'script', label: 'Run a script here', description: 'A little JavaScript, sandboxed' }]
      : []),
  ];

  return (
    <Stack gap="xl">
      <PropertySection title="What it calls" hint="Everything below follows from this.">
        <Select
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
            <ConnectorCatalog onSelect={(c) => onUpdate({ connector_id: c.id, connector_instance_id: '' })} />
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
                        onClick={() => onUpdate({ connector_id: undefined, connector_instance_id: undefined })}
                      >
                        <Trash2 size={14} />
                      </ActionIcon>
                    </Group>
                  </Group>
                </Paper>
              )}
            </PropertySection>

            <PropertySection
              title="If the names differ"
              hint="Only needed when the connector calls things differently from your process."
            >
              <MappingTable
                title="SENDING"
                sourceLabel="Your variable"
                targetLabel="Their field"
                mapping={asTextMap(data.inputs)}
                onUpdate={(m) => onUpdate({ inputs: m })}
              />
              <MappingTable
                title="RECEIVING"
                sourceLabel="Their field"
                targetLabel="Store it as"
                mapping={asTextMap(data.outputs)}
                onUpdate={(m) => onUpdate({ outputs: m })}
              />
            </PropertySection>
          </>
        )
      )}

      {implementation === 'push' && (
        <PropertySection title="Where to call" hint="The process waits for the answer before carrying on.">
          <TextInput
            label="Web address"
            placeholder="https://api.example.com/webhook"
            value={asText(data.url)}
            onChange={(e) => onUpdate({ url: e.target.value })}
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
            value={asText(data.topic)}
            onChange={(e) => onUpdate({ topic: e.target.value })}
          />
        </PropertySection>
      )}

      {implementation === 'script' && expertMode && (
        <PropertySection title="The script" hint="Runs here, with the process variables available to it.">
          <Textarea
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

      {expertMode && <MultiInstanceConfig data={data} onUpdate={onUpdate} />}

      <NodeTestModal
        nodeId="test"
        data={data}
        opened={testModalOpened}
        onClose={() => setTestModalOpened(false)}
      />
    </Stack>
  );
}
