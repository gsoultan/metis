import { Button, Group, Paper, Stack, Text, ThemeIcon } from '@mantine/core';
import { MousePointerClick, PanelLeftOpen, Play } from 'lucide-react';

/**
 * What an empty canvas says to someone who has just arrived.
 *
 * It used to greet them with a blank grid and a validation panel reading
 * "This process is empty", which names the problem and offers no way out.
 * This says what to do and gives the control that does it: most processes
 * begin with a start, so that is one click away.
 */
export function EmptyCanvasHint({
  paletteOpen,
  onAddStart,
  onShowPalette,
}: {
  paletteOpen: boolean;
  onAddStart: () => void;
  onShowPalette: () => void;
}) {
  return (
    <Paper p="lg" withBorder radius="lg" shadow="sm" bg="var(--mantine-color-body)" maw={380}>
      <Stack gap="sm" align="center" ta="center">
        <ThemeIcon size={48} radius="xl" variant="light" color="indigo">
          <MousePointerClick size={24} />
        </ThemeIcon>
        <Text fw={700}>Start with a step</Text>
        <Text size="sm" c="dimmed">
          A process is a sequence of steps. {paletteOpen ? 'Pick one from the building blocks on the left' : 'Open the building blocks'} —
          click to add it, or drag it where it goes — then drag from a step’s edge to connect the next.
        </Text>
        <Group gap="xs" justify="center">
          <Button size="xs" leftSection={<Play size={14} />} onClick={onAddStart}>
            Add a start
          </Button>
          {!paletteOpen && (
            <Button size="xs" variant="default" leftSection={<PanelLeftOpen size={14} />} onClick={onShowPalette}>
              Show building blocks
            </Button>
          )}
        </Group>
      </Stack>
    </Paper>
  );
}
