import { 
  Title, 
  Text, 
  Paper, 
  Stack, 
  Group, 
  Switch, 
  Divider, 
  Button,
  Box,
  SimpleGrid,
  ActionIcon
} from '@mantine/core';
import { 
  Moon, 
  Sun, 
  Trash2, 
  Key,
  ShieldCheck,
  ShieldOff
} from 'lucide-react';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { ComingSoonButton } from '../components/state/ComingSoon';

export function Settings() {
  const { theme, toggleTheme, expertMode, setExpertMode } = useAppStore();

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Application Settings" 
        description="Configure your workspace and preferences."
      />

      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="xl">
        <Stack gap="lg">
          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={5} mb="lg">Appearance</Title>
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
                    checked={expertMode} 
                    onChange={(event) => setExpertMode(event.currentTarget.checked)} 
                    size="md" 
                  />
                </Group>
              </Group>
            </Stack>
          </Paper>

          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={5} mb="lg">Notifications</Title>
            <Stack gap="md">
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">Email Notifications</Text>
                  <Text size="xs" c="dimmed">Receive task assignments and status updates</Text>
                </Box>
                <Switch defaultChecked size="md" />
              </Group>
              <Divider />
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">Push Notifications</Text>
                  <Text size="xs" c="dimmed">Receive alerts in your browser</Text>
                </Box>
                <Switch size="md" />
              </Group>
            </Stack>
          </Paper>
        </Stack>

        <Stack gap="lg">
          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={5} mb="lg">Security & API</Title>
            <Stack gap="md">
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">Two-Factor Authentication</Text>
                  <Text size="xs" c="dimmed">Add an extra layer of security to your account</Text>
                </Box>
                <ComingSoonButton variant="light" color="blue" size="xs" label="Two-factor authentication is not implemented yet">Enable</ComingSoonButton>
              </Group>
              <Divider />
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">API Keys</Text>
                  <Text size="xs" c="dimmed">Manage tokens for external API access</Text>
                </Box>
                <Button variant="outline" color="gray" size="xs" leftSection={<Key size={14} />}>Manage</Button>
              </Group>
            </Stack>
          </Paper>

          <Paper p="xl" radius="lg" withBorder shadow="sm" style={{ borderColor: 'var(--mantine-color-red-2)' }}>
            <Title order={5} mb="lg" c="red">Danger Zone</Title>
            <Stack gap="md">
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm">Clear Cache</Text>
                  <Text size="xs" c="dimmed">Reset local storage and application data</Text>
                </Box>
                <ComingSoonButton variant="light" color="red" size="xs" label="Clearing local application data is not implemented yet">Clear</ComingSoonButton>
              </Group>
              <Divider />
              <Group justify="space-between">
                <Box>
                  <Text fw={600} size="sm" c="red">Delete Account</Text>
                  <Text size="xs" c="dimmed">Permanently delete your account and all data</Text>
                </Box>
                <Button variant="filled" color="red" size="xs" leftSection={<Trash2 size={14} />}>Delete</Button>
              </Group>
            </Stack>
          </Paper>
        </Stack>
      </SimpleGrid>
      
      {/*
        Neither of these buttons was ever wired up, so the entire page was
        decorative: a user could change a setting, press Save, and get no
        feedback of any kind. Settings that DO persist (theme, expert mode)
        already save themselves through the store on change, which is why the
        page needs no save button once the unwired ones are removed.
      */}
      <Group justify="flex-end" mt="xl">
        <ComingSoonButton variant="default" label="Restoring defaults is not implemented yet">
          Reset to Defaults
        </ComingSoonButton>
      </Group>
    </Stack>
  );
}
