/**
 * Where the run is, and the clock it is on. One line.
 *
 * This was a five-button video transport with a speed selector. It is now play,
 * two steps and a scrubber, because the scrubber already does "back to the
 * start" and "skip to the end" and a row of buttons that duplicate a control
 * beside them is a row of buttons nobody reads. Speed moved behind the overflow
 * menu; the arrow keys step, which is how anybody drives this after the first
 * minute.
 *
 * The clock stays, at full weight. A `PT24H` timer is not a day of waiting, it
 * is one step — and unless something says a day passed, a designer reads the
 * trace as though it all happened at once and never notices the SLA they just
 * wrote.
 */
import { ActionIcon, Badge, Group, Menu, Slider, Text, Tooltip } from '@mantine/core';
import { ChevronLeft, ChevronRight, Clock, MoreHorizontal, Pause, Play } from 'lucide-react';

import type { PlaySpeed, SimulationController } from '../../hooks/useSimulation';

const SPEEDS: PlaySpeed[] = [0.5, 1, 2, 4];

export function SimulationTransport({ sim }: { sim: SimulationController }) {
  const hasRun = sim.run !== null && sim.max >= 0;
  if (!hasRun) return null;

  return (
    <Group
      wrap="nowrap"
      gap="sm"
      px="md"
      py={6}
      style={{
        borderTop: '1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-4))',
        backgroundColor: 'light-dark(var(--mantine-color-white), var(--mantine-color-dark-7))',
      }}
    >
      <Group gap={2} wrap="nowrap">
        <Tooltip label="Back a step (←)" withArrow>
          <ActionIcon variant="subtle" color="gray" onClick={sim.stepBack} disabled={sim.cursor === 0} aria-label="Back a step">
            <ChevronLeft size={16} />
          </ActionIcon>
        </Tooltip>

        <Tooltip label={sim.playing ? 'Pause' : 'Play'} withArrow>
          <ActionIcon variant="light" onClick={sim.togglePlay} aria-label={sim.playing ? 'Pause' : 'Play'}>
            {sim.playing ? <Pause size={15} /> : <Play size={15} />}
          </ActionIcon>
        </Tooltip>

        <Tooltip label="Forward a step (→)" withArrow>
          <ActionIcon variant="subtle" color="gray" onClick={sim.stepForward} disabled={sim.atEnd} aria-label="Forward a step">
            <ChevronRight size={16} />
          </ActionIcon>
        </Tooltip>
      </Group>

      <Slider
        flex={1}
        size="xs"
        min={0}
        max={Math.max(sim.max, 0)}
        value={sim.cursor}
        onChange={sim.moveTo}
        label={(value) => `Step ${value + 1}`}
        aria-label="Scrub through the run"
        styles={{ root: { minWidth: 100 }, markLabel: { display: 'none' } }}
      />

      <Badge
        variant="default"
        radius="sm"
        fw={500}
        leftSection={<Clock size={11} aria-hidden />}
        style={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}
      >
        {sim.clockLabel}
      </Badge>

      <Text size="xs" c="dimmed" style={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
        {sim.cursor + 1} of {sim.max + 1}
      </Text>

      <Menu position="top-end" withinPortal>
        <Menu.Target>
          <ActionIcon variant="subtle" color="gray" size="sm" aria-label="Playback options">
            <MoreHorizontal size={15} />
          </ActionIcon>
        </Menu.Target>
        <Menu.Dropdown>
          <Menu.Label>Speed</Menu.Label>
          {SPEEDS.map((value) => (
            <Menu.Item key={value} onClick={() => sim.setSpeed(value)} fw={sim.speed === value ? 700 : 400}>
              {value}x
            </Menu.Item>
          ))}
        </Menu.Dropdown>
      </Menu>
    </Group>
  );
}
