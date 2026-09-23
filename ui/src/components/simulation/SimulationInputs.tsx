/**
 * What the case starts with, as rows rather than JSON.
 *
 * This used to be a textarea you typed `{"amount": 4200}` into. That is the
 * right control for the SDK sandbox, where the audience is writing a client and
 * JSON *is* the subject. It is the wrong control here: the audience is somebody
 * checking whether a £4,200 expense goes to the right approver, and making them
 * think about quoting rules to ask that question is a tax on the wrong people.
 *
 * So: a name, a value, and the value is read the way it was meant — `4200` is a
 * number, `EU` is a string, `true` is a boolean. Anyone who genuinely needs a
 * nested object can still type JSON into the value box and it is parsed as
 * JSON. The escape hatch is there without being in the way.
 */
import { ActionIcon, Button, Group, Stack, Text, TextInput } from '@mantine/core';
import { Plus, X } from 'lucide-react';

import type { SimulationController } from '../../hooks/useSimulation';

export function SimulationInputs({ sim }: { sim: SimulationController }) {
  const rows = sim.active?.variables ?? [];

  return (
    <Stack gap={6}>
      {rows.length === 0 && (
        <Text size="xs" c="dimmed">
          Nothing set. Most processes start with something — an amount, a region, an id.
        </Text>
      )}

      {rows.map((row) => (
        <Group key={row.id} gap={4} wrap="nowrap">
          <TextInput
            size="xs"
            placeholder="name"
            value={row.name}
            onChange={(event) => sim.setVariable(row.id, { name: event.currentTarget.value })}
            aria-label="Variable name"
            style={{ flex: 1, minWidth: 0 }}
          />
          <TextInput
            size="xs"
            placeholder="value"
            value={row.value}
            onChange={(event) => sim.setVariable(row.id, { value: event.currentTarget.value })}
            aria-label={`Value for ${row.name || 'this variable'}`}
            style={{ flex: 1, minWidth: 0 }}
          />
          <ActionIcon
            variant="subtle"
            color="gray"
            size="sm"
            onClick={() => sim.removeVariable(row.id)}
            aria-label={`Remove ${row.name || 'this variable'}`}
          >
            <X size={13} />
          </ActionIcon>
        </Group>
      ))}

      <Button
        size="compact-xs"
        variant="subtle"
        color="gray"
        leftSection={<Plus size={13} />}
        onClick={sim.addVariable}
        w="fit-content"
      >
        Add
      </Button>
    </Stack>
  );
}
