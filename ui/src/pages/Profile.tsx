import { 
  Title, 
  Text, 
  Paper, 
  Stack, 
  Group, 
  Avatar, 
  Badge, 
  Divider, 
  TextInput, 
  Button, 
  SimpleGrid,
  ThemeIcon,
  Box
} from '@mantine/core';
import { Mail, Building2, Calendar, MapPin } from 'lucide-react';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { useForm } from '@mantine/form';
import { useUpdateUser } from '../hooks/useUser';
import { notifications } from '@mantine/notifications';

export function Profile() {
  const { user, setAuth, token } = useAppStore();
  const updateUser = useUpdateUser();

  const form = useForm({
    initialValues: {
      name: user?.name || '',
      displayName: user?.displayName || '',
      organization: user?.organization || '',
      role: user?.role || '',
    },
  });

  if (!user) return null;

  const handleSubmit = async (values: typeof form.values) => {
    try {
      await updateUser.mutateAsync({
        id: user.id,
        full_name: values.name,
        display_name: values.displayName,
        organization: values.organization,
        email: user.username,
        roles: [values.role],
      });

      // Update local store
      if (token) {
        setAuth({
          ...user,
          name: values.name,
          displayName: values.displayName,
          organization: values.organization,
          role: values.role,
        }, token);
      }

      notifications.show({
        title: 'Profile Updated',
        message: 'Your profile has been successfully updated.',
        color: 'green',
      });
    } catch {
      notifications.show({
        title: 'Error',
        message: 'Failed to update profile.',
        color: 'red',
      });
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="User Profile" 
        description="Manage your personal information and account settings."
      />

      <SimpleGrid cols={{ base: 1, md: 3 }} spacing="xl">
        <Stack gap="xl" style={{ gridColumn: 'span 1' }}>
          <Paper p="xl" radius="lg" withBorder shadow="sm" ta="center">
            <Avatar 
              size={120} 
              radius={120} 
              mx="auto" 
              color="blue"
              variant="light"
            >
              {user.displayName?.charAt(0) || user.name?.charAt(0) || 'U'}
            </Avatar>
            <Title order={3} mt="md">{user.displayName || user.name}</Title>
            <Text c="dimmed" size="sm">{user.role}</Text>
            
            <Group justify="center" gap="xs" mt="md">
              <Badge variant="dot" color="green">Active</Badge>
              <Badge variant="outline" color="blue">Professional</Badge>
            </Group>

            <Divider my="lg" />

            <Stack gap="sm">
              <Group gap="sm" wrap="nowrap">
                <ThemeIcon variant="light" color="gray" size="sm">
                  <Mail size={14} />
                </ThemeIcon>
                <Text size="xs" truncate>{user.username || 'no-email@example.com'}</Text>
              </Group>
              <Group gap="sm" wrap="nowrap">
                <ThemeIcon variant="light" color="gray" size="sm">
                  <Calendar size={14} />
                </ThemeIcon>
                <Text size="xs">Joined March 2024</Text>
              </Group>
              <Group gap="sm" wrap="nowrap">
                <ThemeIcon variant="light" color="gray" size="sm">
                  <MapPin size={14} />
                </ThemeIcon>
                <Text size="xs">Berlin, Germany</Text>
              </Group>
            </Stack>
          </Paper>

          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={5} mb="md">Your Organizations</Title>
            <Stack gap="sm">
              {user.organizations?.map((org: any) => (
                <Paper key={org.id} withBorder p="xs" radius="md" bg="gray.0">
                  <Group justify="space-between">
                    <Group gap="xs">
                      <Building2 size={16} color="var(--mantine-color-blue-6)" />
                      <Text size="sm" fw={600}>{org.name}</Text>
                    </Group>
                    <Badge size="xs" variant="light">Owner</Badge>
                  </Group>
                </Paper>
              ))}
            </Stack>
          </Paper>
        </Stack>

        <Paper p="xl" radius="lg" withBorder shadow="sm" style={{ gridColumn: 'span 2' }}>
          <form onSubmit={form.onSubmit(handleSubmit)}>
            <Title order={4} mb="lg">Public Profile</Title>
            <Stack gap="md">
              <SimpleGrid cols={2}>
                <TextInput label="Full Name" placeholder="Your name" {...form.getInputProps('name')} />
                <TextInput label="Display Name" placeholder="Public name" {...form.getInputProps('displayName')} />
              </SimpleGrid>
              <TextInput label="Organization" placeholder="Organization" {...form.getInputProps('organization')} />
              <TextInput label="Email Address" placeholder="Email" value={user.username} disabled />
              <TextInput label="Job Title" placeholder="Your role" {...form.getInputProps('role')} />
              
              <Divider my="md" label="Security" labelPosition="center" />
              
              <Box>
                <Text fw={700} size="sm">Password</Text>
                <Text size="xs" c="dimmed" mb="sm">Last changed 3 months ago</Text>
                <Button variant="light" color="blue" size="xs">Change Password</Button>
              </Box>
  
              <Divider my="md" />
  
              <Group justify="flex-end">
                <Button variant="default" onClick={() => form.reset()}>Discard Changes</Button>
                <Button type="submit" color="indigo" loading={updateUser.isPending}>Save Profile</Button>
              </Group>
            </Stack>
          </form>
        </Paper>
      </SimpleGrid>
    </Stack>
  );
}
