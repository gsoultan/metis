import { Stack, Group, Text, ThemeIcon, Grid, TextInput, Alert } from '@mantine/core';
import { User, Info } from 'lucide-react';
import type { NodeConfigProps } from '../PropertyPanel';

export function ManualTaskConfig({ data, onUpdate }: NodeConfigProps) {
  // Users data available if needed for assignment UI

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md">
            <User size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Manual Instructions</Text>
        </Group>

        <Grid gap="md">
          <Grid.Col span={{ base: 12 }}>
            <TextInput
              label="Assigned Actor"
              placeholder="e.g. Warehouse Manager"
              description="The human role or person who performs this action"
              value={data.actor || ''}
              onChange={(e) => onUpdate({ actor: e.target.value })}
            />
          </Grid.Col>
        </Grid>
        <Alert icon={<Info size={16} />} color="blue" variant="light">
          Manual Tasks represent physical actions that a human must acknowledge in the system.
        </Alert>
      </Stack>
    </Stack>
  );
}
