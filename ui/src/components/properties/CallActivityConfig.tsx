import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  Grid, 
  Select, 
  NumberInput, 
  Divider, 
  Alert, 
  Button 
} from '@mantine/core';
import { Zap } from 'lucide-react';
import { useDefinitions, useSubProcesses } from '../../hooks/useProcess';
import { MappingTable } from './CommonProperties';
import type { NodeConfigProps } from '../PropertyPanel';

export function CallActivityConfig({
  data,
  nodeId,
  onUpdate,
  instanceId,
  onViewInstance
}: NodeConfigProps) {
  const { data: defsData } = useDefinitions();
  const { data: subProcessesData } = useSubProcesses(instanceId || null);

  const definitions = (defsData as any)?.definitions || [];
  const subProcesses = (subProcessesData as any)?.instances || [];

  const activeSubProcess = subProcesses.find((s: any) => s.parent_node_id === (nodeId ?? ''));

  return (
    <Stack gap="xl">
      {activeSubProcess && onViewInstance && (
        <Alert icon={<Zap size={16} />} color="teal" variant="light">
          <Group justify="space-between">
            <Text size="sm">An active sub-process is running for this node.</Text>
            <Button 
              size="compact-xs" 
              variant="light" 
              color="teal" 
              onClick={() => onViewInstance(activeSubProcess.id, activeSubProcess.definition_id)}
            >
              Drill Down
            </Button>
          </Group>
        </Alert>
      )}

      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="teal" radius="md">
            <Zap size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Sub-Process Configuration</Text>
        </Group>

        <Grid gap="md">
          <Grid.Col span={{ base: 12, sm: 8 }}>
            <Select
              label="Called Process Definition"
              description="The process to start when this node is reached"
              placeholder="Select a process"
              data={definitions.map((d: any) => ({ value: d.key, label: `${d.name} (${d.key})` }))}
              value={data.called_process_key || ''}
              onChange={(val) => onUpdate({ called_process_key: val })}
              searchable
              clearable
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 4 }}>
            <NumberInput
              label="Version"
              description="0 = Latest"
              value={data.called_process_version || 0}
              onChange={(val) => onUpdate({ called_process_version: Number(val) || 0 })}
            />
          </Grid.Col>
        </Grid>
      </Stack>

      <Divider variant="dashed" />

      <Stack gap="md">
        <MappingTable 
          title="In Mapping (Parent → Child)" 
          mapping={data.in_mapping || {}} 
          onUpdate={(m) => onUpdate({ in_mapping: m })} 
        />

        <MappingTable 
          title="Out Mapping (Child → Parent)" 
          mapping={data.out_mapping || {}} 
          onUpdate={(m) => onUpdate({ out_mapping: m })} 
        />
      </Stack>
    </Stack>
  );
}
