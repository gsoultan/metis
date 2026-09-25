import { 
  Title, 
  Text, 
  Paper, 
  Stack, 
  Group, 
  Switch, 
  Divider, 
  Box,
  SimpleGrid,
  ActionIcon
} from '@mantine/core';
import { 
  Moon, 
  Sun, 
  ShieldCheck,
  ShieldOff
} from 'lucide-react';
import { useAppStore } from '../store/useAppStore';
import { hasRole } from '../domain/access';
import { PRIVILEGED_ROLE } from '../domain/roles';
import { EnvironmentSettings } from '../components/EnvironmentSettings';
import { PageHeader } from '../components/PageHeader';
import { useTranslation } from '../i18n/context';

export function Settings() {
  const { t } = useTranslation();
  // Environments are administrative: the list alone says where every runtime's
  // database lives. The server refuses a non-admin either way; hiding it here
  // is so nobody is shown a control that will only ever say no.
  const isAdmin = useAppStore((state) => hasRole(state.user, PRIVILEGED_ROLE));
  const theme = useAppStore((state) => state.theme);
  const toggleTheme = useAppStore((state) => state.toggleTheme);
  const expertMode = useAppStore((state) => state.expertMode);
  const setExpertMode = useAppStore((state) => state.setExpertMode);

  return (
    <Stack gap="xl">
      <PageHeader 
        title={t('page.settings.title')}
        description={t('page.settings.subtitle')}
      />

      {/*
        Full width, above the preference grid: an environment names a database
        and a port, which is infrastructure rather than a preference, and it is
        the thing an administrator opens this page to change.
      */}
      {isAdmin && <EnvironmentSettings />}

      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="xl">
        <Stack gap="lg">
          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={2} size="h5" mb="lg">Appearance</Title>
            <Stack gap="md">
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">Interface Theme</Text>
                  <Text size="xs" c="dimmed">Choose between light and dark mode</Text>
                </Box>
                <Group gap={0}>
                  <ActionIcon aria-label="Use light theme" 
                    variant={theme === 'light' ? 'filled' : 'light'} 
                    onClick={() => theme === 'dark' && toggleTheme()}
                    size="lg"
                    radius="md"
                  >
                    <Sun size={18} />
                  </ActionIcon>
                  <ActionIcon aria-label="Use dark theme" 
                    variant={theme === 'dark' ? 'filled' : 'light'} 
                    onClick={() => theme === 'light' && toggleTheme()}
                    size="lg"
                    radius="md"
                    ml="xs"
                  >
                    <Moon size={18} />
                  </ActionIcon>
                </Group>
              </Group>
              
              <Divider />

              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">Expert Mode</Text>
                  <Text size="xs" c="dimmed">Show advanced technical settings and schemas</Text>
                </Box>
                <Group gap="xs">
                  {expertMode ? <ShieldCheck size={16} color="green" /> : <ShieldOff size={16} color="gray" />}
                  <Switch 
                    aria-label="Expert mode"
                    checked={expertMode} 
                    onChange={(event) => setExpertMode(event.currentTarget.checked)} 
                    size="md" 
                  />
                </Group>
              </Group>
            </Stack>
          </Paper>
          {/*
            A "Notifications" panel with email and push switches stood here.
            Neither was wired to anything — one even rendered as already on —
            so a user could turn on email notifications, get none, and not know
            whether the feature or their mail was broken. Notifications arrive
            in the bell in the header; there is nothing to configure yet.
          */}
        </Stack>

        {/*
          "Security & API" (Two-Factor Authentication, API Keys), the "Danger
          Zone" (Clear Cache) and "Reset to Defaults" all stood here as disabled
          controls. Four of this page's six controls did nothing, so a page
          called Application Settings was two thirds unavailable — and the
          Danger Zone was a red-bordered section whose only action was greyed
          out, which alarms without informing.

          A disabled control teaches when the feature exists and is unavailable
          to *you*. It misleads when the feature does not exist at all. None of
          these is built, so none of them is rendered.

          Two-factor authentication is tracked as a real gap in
          .junie/security-plan.md rather than advertised here as a grey button.
        */}
      </SimpleGrid>
    </Stack>
  );
}
