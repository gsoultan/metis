/**
 * What the case must end with.
 *
 * Built from a run rather than typed from nothing: "Expect this result" copies
 * how the run on screen ended and where, and the person adds the one or two
 * values that matter — the approver, the amount charged. Expecting every value
 * a run ends with is how a case starts failing on a timestamp.
 */
import { ActionIcon, Button, Group, Select, Stack, Text, TextInput } from '@mantine/core';
import { Plus, Target, X } from 'lucide-react';
import { v4 as uuidv4 } from 'uuid';

import { blankExpectation, expectationFromRun, type ExpectedOutcome, type Expectation } from '../../domain/simulationCase';
import type { SimulationController } from '../../hooks/useSimulation';

const OUTCOMES: { value: ExpectedOutcome; label: string }[] = [
  { value: '', label: 'However it ends' },
  { value: 'completed', label: 'It finishes' },
  { value: 'needs_answer', label: 'It stops to ask' },
  { value: 'incident', label: 'It raises an incident' },
];

export function SimulationExpect({
  sim,
  labelOf,
}: {
  sim: SimulationController;
  labelOf: (nodeId: string) => string;
}) {
  const expect = sim.active?.expect ?? blankExpectation();
  const change = (next: Partial<Expectation>) => sim.setExpect({ ...expect, ...next });

  return (
    <Stack gap={6}>
      <Button
        size="compact-xs"
        variant="light"
        leftSection={<Target size={13} />}
        onClick={() => sim.run !== null && sim.setExpect({ ...expectationFromRun(sim.run), variables: expect.variables })}
        disabled={sim.run === null}
        w="fit-content"
      >
        Expect this result
      </Button>

      <Select
        size="xs"
        label="How it ends"
        value={expect.outcome}
        onChange={(value) => change({ outcome: (value ?? '') as ExpectedOutcome })}
        data={OUTCOMES}
        allowDeselect={false}
        comboboxProps={{ withinPortal: true }}
      />

      {expect.endsAt !== '' && (
        <Group gap={4} wrap="nowrap">
          <Text size="xs" c="dimmed">
            At
          </Text>
          <Text size="xs" fw={500} truncate style={{ flex: 1, minWidth: 0 }}>
            {labelOf(expect.endsAt)}
          </Text>
          <ActionIcon
            variant="subtle"
            color="gray"
            size="sm"
            onClick={() => change({ endsAt: '' })}
            aria-label="Do not check where it ends"
          >
            <X size={13} />
          </ActionIcon>
        </Group>
      )}

      <Text size="xs" c="dimmed">
        Holding
      </Text>
      {expect.variables.map((row) => (
        <Group key={row.id} gap={4} wrap="nowrap">
          <TextInput
            size="xs"
            placeholder="name"
            value={row.name}
            onChange={(event) =>
              change({ variables: expect.variables.map((r) => (r.id === row.id ? { ...r, name: event.currentTarget.value } : r)) })
            }
            aria-label="Expected variable name"
            style={{ flex: 1, minWidth: 0 }}
          />
          <TextInput
            size="xs"
            placeholder="value"
            value={row.value}
            onChange={(event) =>
              change({ variables: expect.variables.map((r) => (r.id === row.id ? { ...r, value: event.currentTarget.value } : r)) })
            }
            aria-label={`Expected value for ${row.name || 'this variable'}`}
            style={{ flex: 1, minWidth: 0 }}
          />
          <ActionIcon
            variant="subtle"
            color="gray"
            size="sm"
            onClick={() => change({ variables: expect.variables.filter((r) => r.id !== row.id) })}
            aria-label={`Stop expecting ${row.name || 'this variable'}`}
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
        onClick={() => change({ variables: [...expect.variables, { id: uuidv4(), name: '', value: '' }] })}
        w="fit-content"
      >
        Add a value
      </Button>
    </Stack>
  );
}
