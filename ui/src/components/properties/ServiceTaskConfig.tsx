import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  Select, 
  Grid, 
  TextInput, 
  Divider, 
  Button, 
  Paper, 
  Box, 
  ActionIcon, 
  Textarea
} from '@mantine/core';
import { Globe, Zap, Settings, Play, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useConnectors } from '../../hooks/useProcess';
import { useAppStore } from '../../store/useAppStore';
import { MappingTable, MultiInstanceConfig, NodeTestModal, ConnectorCatalog } from './CommonProperties';
import { HelpTooltip } from '../LowCodeComponents';
import type { NodeConfigProps } from '../PropertyPanel';
import { asText, asTextMap } from '../../types/bpmn';

export function ServiceTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const implementation = asText(data.implementation, 'push');
  const { data: connectorsData } = useConnectors();
  const { expertMode } = useAppStore();
  const [testModalOpened, setTestModalOpened] = useState(false);
  
  const connectors = (connectorsData as any)?.connectors || [];
  const selectedConnector = connectors.find((c: any) => c.id === data.connector_id);

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="teal" radius="md">
            <Globe size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Service Protocol</Text>
        </Group>

        <Grid gap="md">
          <Grid.Col span={{ base: 12 }}>
            <Select
              label={
                <Group gap={4} wrap="nowrap">
                  <Text size="sm" fw={500}>Implementation Type</Text>
                  <HelpTooltip label="Choose how the service task logic is executed." link="https://docs.gobpm.io/service-tasks" />
                </Group>
              }
              description="How the task logic is executed"
              size="md"
              data={[
                { value: 'push', label: 'HTTP Push (Remote Endpoint)' },
                { value: 'external', label: 'External Worker (Long Polling)' },
                { value: 'connector', label: 'Marketplace Connector' },
                ...(expertMode ? [{ value: 'script', label: 'Internal Script (JS Sandbox)' }] : [])
              ]}
              value={implementation}
              onChange={(val) => onUpdate({ implementation: val })}
            />
          </Grid.Col>
        </Grid>
      </Stack>

      <Divider variant="dashed" />

      {implementation === 'connector' && (
        <Stack gap="md">
           {!data.connector_id ? (
             <ConnectorCatalog 
                onSelect={(c) => onUpdate({ connector_id: c.id, connector_instance_id: '' })} 
             />
           ) : (
             <Stack gap="md">
                <Group gap="xs">
                  <ThemeIcon variant="light" color="yellow" radius="md">
                    <Zap size={18} />
                  </ThemeIcon>
                  <Text fw={700} size="md">Selected Connector</Text>
                </Group>

                {selectedConnector && (
                  <Paper withBorder p="md" bg="gray.0" radius="md">
                    <Group gap="xs" wrap="nowrap">
                      <ThemeIcon size="xl" radius="md" color="yellow" variant="light">
                        <Zap size={24} />
                      </ThemeIcon>
                      <Box style={{ flex: 1 }}>
                        <Group justify="space-between">
                          <Text size="md" fw={700}>{selectedConnector.name}</Text>
                          <ActionIcon aria-label="Delete" 
                            size="sm" 
                            variant="subtle" 
                            color="red" 
                            onClick={() => onUpdate({ connector_id: undefined, connector_instance_id: undefined })}
                          >
                            <Trash2 size={14} />
                          </ActionIcon>
                        </Group>
                        <Text size="xs" c="dimmed">{selectedConnector.description}</Text>
                      </Box>
                    </Group>
                  </Paper>
                )}

                <Button 
                  size="xs" 
                  variant="light" 
                  color="indigo" 
                  leftSection={<Play size={14} />}
                  onClick={() => setTestModalOpened(true)}
                >
                  Test Connection
                </Button>

                <MappingTable 
                  title="Input Variables (Parent → Connector)" 
                  mapping={asTextMap(data.inputs)} 
                  onUpdate={(m) => onUpdate({ inputs: m })} 
                />

                <MappingTable 
                  title="Output Mapping (Connector → Parent)" 
                  mapping={asTextMap(data.outputs)} 
                  onUpdate={(m) => onUpdate({ outputs: m })} 
                />
             </Stack>
           )}
        </Stack>
      )}

      {implementation === 'push' && (
        <Stack gap="md">
          <TextInput 
            label="Endpoint URL" 
            placeholder="https://api.example.com/webhook"
            description="The URL that will receive a POST request when this task starts"
            size="md"
            value={asText(data.url)}
            onChange={(e) => onUpdate({ url: e.target.value })}
          />
          <TextInput 
            label="Secret / Auth Token" 
            placeholder="Optional bearer token"
            description="Sent in Authorization header"
            size="md"
            type="password"
            value={asText(data.auth_token)}
            onChange={(e) => onUpdate({ auth_token: e.target.value })}
          />
        </Stack>
      )}

      {implementation === 'external' && (
        <Stack gap="md">
          <TextInput 
            label="Topic Name" 
            placeholder="e.g. process-invoice"
            description="External workers will subscribe to this topic"
            size="md"
            value={asText(data.topic)}
            onChange={(e) => onUpdate({ topic: e.target.value })}
          />
        </Stack>
      )}

      {implementation === 'script' && expertMode && (
        <Stack gap="md">
           <Group gap="xs">
              <ThemeIcon variant="light" color="indigo" radius="md">
                <Settings size={18} />
              </ThemeIcon>
              <Text fw={700} size="md">Script Definition</Text>
           </Group>
           <Textarea 
              label="JavaScript Logic" 
              placeholder="// context contains 'vars' and 'data'..." 
              minRows={10}
              styles={{ input: { fontFamily: 'monospace', fontSize: '12px' } }}
              value={asText(data.script)}
              onChange={(e) => onUpdate({ script: e.target.value })}
           />
           <Button 
              size="xs" 
              variant="light" 
              color="indigo" 
              leftSection={<Play size={14} />}
              onClick={() => setTestModalOpened(true)}
            >
              Run Dry-Run Test
            </Button>
        </Stack>
      )}

      {expertMode && (
        <>
          <Divider variant="dashed" />
          <MultiInstanceConfig data={data} onUpdate={onUpdate} />
        </>
      )}

      <NodeTestModal 
        nodeId="test" 
        data={data} 
        opened={testModalOpened} 
        onClose={() => setTestModalOpened(false)} 
      />
    </Stack>
  );
}
