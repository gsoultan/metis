import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  Grid, 
  Select, 
  NumberInput, 
  Divider 
} from '@mantine/core';
import { LayoutGrid, RefreshCw } from 'lucide-react';
import { useDecisions } from '../../hooks/useProcess';
import { MappingTable } from './CommonProperties';
import type { NodeConfigProps } from '../PropertyPanel';

export function BusinessRuleTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const { data: decisionsData } = useDecisions();
  const decisions = (decisionsData as any)?.decisions || [];

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="grape" radius="md">
            <LayoutGrid size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">DMN Configuration</Text>
        </Group>

        <Grid gap="md">
          <Grid.Col span={{ base: 12, sm: 8 }}>
            <Select
              label="Decision Key"
              description="Key of the DMN decision to execute"
              placeholder="Select a decision"
              data={decisions.map((d: any) => ({ value: d.key, label: `${d.name} (${d.key})` }))}
              value={data.decision_key || ''}
              onChange={(val) => onUpdate({ decision_key: val })}
              searchable
              clearable
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 4 }}>
            <NumberInput
              label="Version"
              description="0 = Latest"
              value={data.decision_version || 0}
              onChange={(val) => onUpdate({ decision_version: Number(val) || 0 })}
            />
          </Grid.Col>
        </Grid>
      </Stack>

      <Divider variant="dashed" />

      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md">
            <RefreshCw size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Data Mapping</Text>
        </Group>

        <MappingTable 
          title="Input Mapping (Process Variable → Decision Input)" 
          mapping={data.input_mapping || {}} 
          onUpdate={(m) => onUpdate({ input_mapping: m })} 
        />

        <MappingTable 
          title="Output Mapping (Decision Output → Process Variable)" 
          mapping={data.output_mapping || {}} 
          onUpdate={(m) => onUpdate({ output_mapping: m })} 
        />
      </Stack>
    </Stack>
  );
}
