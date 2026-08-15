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
}

export function DefaultErrorFallback({ error, onReset }: DefaultErrorFallbackProps) {
  return (
    <Center h="100%">
      <Stack align="center" gap="md" maw={480}>
        <Alert
          icon={<AlertCircle size={20} />}
          title="Something went wrong"
          color="red"
          radius="md"
          w="100%"
        >
          <Stack gap="xs">
            <Text size="sm">
              An unexpected error occurred while rendering this section. You can try reloading,
              or navigate back and retry.
            </Text>
            {error?.message && (
              <Text size="xs" c="dimmed" ff="monospace">
                {error.message}
              </Text>
            )}
          </Stack>
        </Alert>

        <Button variant="light" color="red" onClick={onReset}>
          Try again
        </Button>
      </Stack>
    </Center>
  );
}
