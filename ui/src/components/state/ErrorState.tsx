import { Button, Code, Collapse, Group, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { AlertTriangle, ChevronDown, RefreshCw, WifiOff } from 'lucide-react';
import { errorMessage } from '../../services/shared/errors';

interface ErrorStateProps {
  /** The thrown value. Narrowed here rather than at every call site. */
  error: unknown;
  /** What the app was doing, in plain language: "load your tasks". */
  action?: string;
  onRetry?: () => void;
  size?: 'sm' | 'md';
}

/**
 * The state a surface shows when its request failed.
 *
 * Most products show a red box reading "Something went wrong", which tells the
 * user nothing they did not already know. This one answers the three questions
 * they actually have:
 *
 *  1. **What failed?** Named in terms of the task, not the endpoint.
 *  2. **Is it me or you?** A network failure gets different wording and a
 *     different icon from a server failure, because the useful response is
 *     different — check your connection, versus try again or report it.
 *  3. **What now?** A retry button, always, when the caller can retry.
 *
 * The raw message is available but collapsed. A business user should never
 * have to read it; the person they escalate to needs it to be one click away,
 * not buried in a browser console.
 */
export function ErrorState({ error, action = 'load this', onRetry, size = 'md' }: ErrorStateProps) {
  const [detailOpen, { toggle }] = useDisclosure(false);
  const compact = size === 'sm';

  const message = errorMessage(error);
  const offline = typeof navigator !== 'undefined' && navigator.onLine === false;
  const networkish =
    offline || /network|fetch|connection|timeout|ECONN|Failed to fetch/i.test(message);

  const Icon = networkish ? WifiOff : AlertTriangle;
  const title = networkish ? 'Cannot reach the server' : `Could not ${action}`;
  const explanation = networkish
    ? 'Check your connection. Your work is not lost — nothing was sent.'
    : 'The server rejected the request. Trying again often resolves it.';

  return (
    // alert, not status: a failure should interrupt, because the user is
    // otherwise left looking at a surface that appears merely empty.
    <Stack align="center" gap={compact ? 'xs' : 'sm'} py={compact ? 'lg' : 48} px="md" role="alert">
      <ThemeIcon size={compact ? 40 : 56} radius="xl" variant="light" color="red" aria-hidden>
        <Icon size={compact ? 20 : 26} strokeWidth={1.75} />
      </ThemeIcon>

      <Title order={compact ? 5 : 4} ta="center">
        {title}
      </Title>

      <Text c="dimmed" size="sm" ta="center" maw={420} lh={1.6}>
        {explanation}
      </Text>

      <Group gap="xs" mt="xs">
        {onRetry && (
          <Button onClick={onRetry} leftSection={<RefreshCw size={16} />} variant="light">
            Try again
          </Button>
        )}
        <Button
          variant="subtle"
          color="gray"
          size="compact-sm"
          onClick={toggle}
          rightSection={
            <ChevronDown
              size={14}
              style={{
                transform: detailOpen ? 'rotate(180deg)' : undefined,
                transition: 'transform 150ms ease',
              }}
            />
          }
          aria-expanded={detailOpen}
        >
          Technical details
        </Button>
      </Group>

      <Collapse expanded={detailOpen}>
        <Code block maw={520} style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
          {message}
        </Code>
      </Collapse>
    </Stack>
  );
}
