import { Alert, Box, Card, Group, Paper, Stack, Text, ThemeIcon } from '@mantine/core';
import { AlertCircle, Code, Info } from 'lucide-react';

import { useAppStore } from '../store/useAppStore';
import { RawSchemaEditor } from './RawSchemaEditor';

/**
 * The element's settings as JSON in Expert mode, and in basic mode a note on
 * where they went.
 *
 * The editor stays on the page in basic mode, hidden rather than removed, so
 * JSON typed into it and not yet applied is still there when Expert mode
 * comes back. Rendered only in Expert mode, it was unmounted as the mode was
 * turned off, and what it held was thrown away without a word.
 */
export function RawSchemaCard({
  settings,
  onApply,
}: {
  settings: Record<string, unknown>,
  onApply: (patch: Record<string, unknown>) => void,
}) {
  const expertMode = useAppStore((state) => state.expertMode);

  return (
    <>
      <Box hidden={!expertMode}>
        <Card withBorder radius="md" p="xl" shadow="sm">
          <Stack gap="md">
            <Group gap="xs" mb="xs">
              <ThemeIcon variant="light" color="orange">
                <Code size={18} />
              </ThemeIcon>
              <Text fw={700} size="lg">Raw Schema</Text>
            </Group>

            <Text size="xs" c="dimmed">Underlying JSON structure of this element</Text>

            <RawSchemaEditor settings={settings} onApply={onApply} />

            <Alert color="orange" icon={<AlertCircle size={16} />} py="xs">
              <Text size="10px" fw={500}>Caution: Manual JSON modification may cause unexpected behavior if properties are invalid.</Text>
            </Alert>
          </Stack>
        </Card>
      </Box>

      {!expertMode && (
        <Paper withBorder p="xl" radius="md" bg="blue.0" style={{ borderStyle: 'dashed' }}>
          <Stack gap="xs" align="center" py="md">
            <Info size={32} color="var(--mantine-color-blue-4)" />
            <Text fw={700} ta="center">Simplified view</Text>
            <Text size="xs" c="dimmed" ta="center">
              Advanced settings appear here only once a step uses them. Turn on Expert mode at the top
              of this panel to change them, and to see the raw schema and an API example.
            </Text>
          </Stack>
        </Paper>
      )}
    </>
  );
}
