import { Button, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { CheckCircle2, Rocket } from 'lucide-react';
import { signInAddress } from '../../domain/setupWizard';

interface SetupCompleteProps {
  fromEnvironment: boolean;
  /** The server was serving only the wizard, and starts on the saved configuration by itself. */
  wizardOnly?: boolean;
  httpAddress?: string;
  publicURL?: string;
  onComplete: () => void;
}

/**
 * The wizard's last step: what setup did, and what to do next.
 *
 * Three ways it can finish. A server set up from its environment is already
 * running on the database and keys setup used. One that was serving only the
 * wizard starts on what setup saved, by itself, in a few seconds — possibly on
 * another address. Any other keeps the database and keys it started with
 * until it restarts, and the account setup created may be in a database it is
 * not reading.
 */
export function SetupComplete({ fromEnvironment, wizardOnly = false, httpAddress = '', publicURL = '', onComplete }: SetupCompleteProps) {
  const elsewhere =
    wizardOnly && typeof window !== 'undefined'
      ? signInAddress(new URL(window.location.href), httpAddress, publicURL)
      : null;
  const goToSignIn = () => {
    if (elsewhere) {
      window.location.assign(elsewhere);
      return;
    }
    onComplete();
  };

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
      ) : wizardOnly ? (
        <Text ta="center" c="dimmed">
          Your configuration has been saved to <strong>config.yaml</strong> with encrypted credentials.<br />
          Metis is starting on the database you chose. This takes a few seconds, and then you can sign in
          with your administrator account{elsewhere ? <> at <strong>{elsewhere.replace(/\/login$/, '')}</strong></> : null}.
        </Text>
      ) : (
        <Text ta="center" c="dimmed">
          Your configuration has been saved to <strong>config.yaml</strong> with encrypted credentials.<br />
          <strong>Restart Metis to run on it</strong>, then sign in with your administrator account. Until it
          restarts, this server keeps the database and keys it started with, so your new account may not work yet.
        </Text>
      )}
      {/* Not a wizard step: setup is anonymous, and every saved setting names
          the administrator who chose it and why. */}
      <Text ta="center" size="sm" c="dimmed">
        Once you are signed in, set up email, single sign-on and retention under{' '}
        <strong>Settings → Server settings</strong>.
      </Text>
      <Button size="lg" mt="md" onClick={goToSignIn} rightSection={<Rocket size={18} />}>
        {fromEnvironment ? 'Get Started' : 'Go to sign-in'}
      </Button>
    </Stack>
  );
}
