/**
 * A real instance, run again.
 *
 * The question before a deploy is rarely "what does this do with made-up
 * numbers?" — it is "what would the refund that went wrong on Tuesday have
 * done on this?". So the case is not typed: the server reads it off the
 * instance (what it started with, who did each step and what they entered,
 * what its services answered), and the first run fills it in here, where it
 * can be seen and changed like any other case.
 *
 * Where it runs is the one choice: the diagram on screen, which is a draft
 * once it has been edited, or whatever is live now.
 */
import { Button, SegmentedControl, Stack, Text } from '@mantine/core';
import { History } from 'lucide-react';

import { replayVerdict } from '../../domain/simulationTrace';
import type { SimulationController } from '../../hooks/useSimulation';
import { SimulationVerdict } from './SimulationVerdict';

const STATUS_WORDS: Record<string, string> = {
  active: 'still running',
  completed: 'finished',
  cancelled: 'cancelled',
  suspended: 'suspended',
  failed: 'failed',
};

export function SimulationReplay({
  sim,
  instanceId,
  labelOf,
}: {
  sim: SimulationController;
  /** The instance the designer was opened on, when it was opened on one. */
  instanceId?: string;
  labelOf: (nodeId: string) => string;
}) {
  const replay = sim.active?.replay;

  if (replay === undefined) {
    if (instanceId === undefined) return null;
    return (
      <Stack gap={4}>
        <Button
          size="xs"
          variant="light"
          leftSection={<History size={14} />}
          onClick={() => sim.addReplayCase(instanceId)}
        >
          Replay the instance you opened
        </Button>
        <Text size="xs" c="dimmed">
          Runs its case — what it started with, who did each step — on this diagram or on what is live, and
          says where it would have gone differently.
        </Text>
      </Stack>
    );
  }

  const read = sim.run?.replay;
  const comparison = sim.run !== null ? replayVerdict(sim.run, labelOf) : null;

  return (
    <Stack gap={6}>
      <Text size="xs" c="dimmed">
        Replaying instance {replay.instanceId.slice(0, 8)}
        {read !== undefined && `, which ran on version ${read.version} and is ${STATUS_WORDS[read.status] ?? read.status}`}.
      </Text>
      <SegmentedControl
        size="xs"
        fullWidth
        value={replay.on}
        onChange={(value) => sim.setReplayOn(value as 'diagram' | 'live')}
        data={[
          { value: 'diagram', label: 'On this diagram' },
          { value: 'live', label: 'On what is live' },
        ]}
        aria-label="Where to replay the instance"
      />
      {comparison !== null && <SimulationVerdict verdict={comparison} />}
      {read?.notes.map((note) => (
        <Text key={note} size="xs" c="dimmed">
          {note}
        </Text>
      ))}
    </Stack>
  );
}
