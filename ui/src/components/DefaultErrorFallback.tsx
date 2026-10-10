/**
 * What an ErrorBoundary shows when it catches something.
 *
 * Its own file because react-refresh needs a module to export components and
 * nothing else, and the boundary itself is a class.
 */
import { Alert, Button, Center, Stack, Text } from '@mantine/core';
import { AlertCircle } from 'lucide-react';

interface DefaultErrorFallbackProps {
  error?: Error;
  onReset?: () => void;
  /** Overrides for a case that is not a crash — an address that leads nowhere. */
  title?: string;
  description?: string;
  resetLabel?: string;
}

export function DefaultErrorFallback({
  error,
  onReset,
  title = 'Something went wrong',
  description = 'An unexpected error occurred while rendering this section. You can try reloading, or navigate back and retry.',
  resetLabel = 'Try again',
}: DefaultErrorFallbackProps) {
  return (
    <Center h="100%">
      <Stack align="center" gap="md" maw={480}>
        <Alert
          icon={<AlertCircle size={20} />}
          title={title}
          color="red"
          radius="md"
          w="100%"
        >
          <Stack gap="xs">
            <Text size="sm">{description}</Text>
            {error?.message && (
              <Text size="xs" c="dimmed" ff="monospace">
                {error.message}
              </Text>
            )}
          </Stack>
        </Alert>

        <Button variant="light" color="red" onClick={onReset}>
          {resetLabel}
        </Button>
      </Stack>
    </Center>
  );
}
