import { Button, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { CheckCircle2, Rocket } from 'lucide-react';

/**
 * The wizard's last step: what setup did, and what to do next.
 *
 * A server set up from its environment is already running on the database and
 * keys setup used. One whose configuration setup has just written is not: it
 * keeps the database and keys it started with until it restarts, and the
 * account setup created may be in a database it is not reading.
 */
export function SetupComplete({ fromEnvironment, onComplete }: { fromEnvironment: boolean; onComplete: () => void }) {
  return (
    <Stack align="center" gap="md" mt="xl" py="xl">
      <ThemeIcon size={80} radius={100} color="green" variant="light">
        <CheckCircle2 size={50} />
      </ThemeIcon>
      <Title order={2}>Setup Complete!</Title>
      {fromEnvironment ? (
        <Text ta="center" c="dimmed">
          Metis BPM has been successfully initialized.<br />
          This server reads its database and keys from its environment, so there was nothing to save.<br />
          You can now log in with your administrator account.
        </Text>
      ) : (
        <Text ta="center" c="dimmed">
          Your configuration has been saved to <strong>config.yaml</strong> with encrypted credentials.<br />
          <strong>Restart Metis to run on it</strong>, then sign in with your administrator account. Until it
          restarts, this server keeps the database and keys it started with, so your new account may not work yet.
        </Text>
      )}
      <Button size="lg" mt="md" onClick={onComplete} rightSection={<Rocket size={18} />}>
        {fromEnvironment ? 'Get Started' : 'Go to sign-in'}
      </Button>
    </Stack>
  );
}
