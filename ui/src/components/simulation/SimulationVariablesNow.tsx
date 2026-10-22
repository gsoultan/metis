import { Group, Stack, Text } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';

import { formatValue } from '../../domain/simulationScenario';
import type { SimulationController } from '../../hooks/useSimulation';
import { Section } from './SimulationSection';

/**
 * The variables as they stand at the cursor.
 *
 * Folded by default: it matters when a value is wrong, and until then it is
 * four lines of noise under the thing people are actually reading.
 */
export function VariablesAtCursor({ sim }: { sim: SimulationController }) {
  const [opened, { toggle }] = useDisclosure(false);
  const names = Object.keys(sim.variables);
  if (names.length === 0) return null;

  return (
    <Section label="Variables now" count={names.length} opened={opened} collapsible onToggle={toggle}>
      <Stack gap={2}>
        {names.map((name) => {
          const changed = sim.changedKeys.includes(name);
          return (
            <Group key={name} justify="space-between" gap="sm" wrap="nowrap">
              <Group gap={4} wrap="nowrap" style={{ minWidth: 0 }}>
                <Text size="xs" c="dimmed" truncate>
                  {name}
                </Text>
                {/* A glyph, not a colour: a change marked only by colour is a
                    change some people never see. */}
                {changed && (
                  <Text size="xs" c="blue.7" aria-label="changed at this step">
                    ✎
                  </Text>
                )}
              </Group>
              <Text size="xs" fw={changed ? 600 : 400} truncate ff="monospace">
                {formatValue(sim.variables[name])}
              </Text>
            </Group>
          );
        })}
      </Stack>
    </Section>
  );
}
