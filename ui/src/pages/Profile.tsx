import { 
  Title, 
  Text, 
  Paper, 
  Stack, 
  Group, 
  Avatar,  
  Divider, 
  TextInput, 
  Button, 
  SimpleGrid,
  ThemeIcon,
  Box
} from '@mantine/core';
import { Mail, Building2,} from 'lucide-react';
import { useState } from 'react';
import { ChangePasswordModal } from '../components/ChangePasswordModal';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { z } from 'zod';
import { useForm, fieldProps, zodField } from '../components/form/AppForm';
import { useUpdateUser } from '../hooks/useUser';
import { notifications } from '@mantine/notifications';
import { roleLabels } from '../domain/roles';
import { failureMessage } from '../services/shared/errors';
import { useTranslation } from '../i18n/context';

export function Profile() {
  const { t } = useTranslation();
  const { user, setAuth, token } = useAppStore();
  const updateUser = useUpdateUser();
  const [changingPassword, setChangingPassword] = useState(false);

  // Validation lives in a schema so the form and the request payload cannot
  // disagree about what is required.
  const nameField = z.string().trim().min(1, 'Full name is required').max(120, 'Full name is too long');
  const displayNameField = z.string().trim().min(1, 'Display name is required').max(60, 'Display name is too long');
  const optionalField = z.string().trim().max(120, 'This is too long');

  const form = useForm({
    defaultValues: {
      name: user?.name ?? '',
      displayName: user?.displayName ?? '',
      organization: user?.organization ?? '',
    },
    onSubmit: async ({ value }) => handleSubmit(value),
  });

  if (!user) return null;

  const handleSubmit = async (values: {
    name: string;
    displayName: string;
    organization: string;
  }) => {
    try {
      // The profile edits a person's own name and details, never their roles:
      // "Job Title" is free text, and submitting it as a role turned a user
      // with ["ADMIN","USER"] who only changed their name into one holding the
      // single bogus role "ADMIN, USER". Roles are managed under People.
      await updateUser.mutateAsync({
        id: user.id,
        full_name: values.name,
        display_name: values.displayName,
        organization: values.organization,
        email: user.username,
      });

      // Update local store
      if (token) {
        setAuth({
          ...user,
          name: values.name,
          displayName: values.displayName,
          organization: values.organization,
        }, token);
      }

      notifications.show({
        title: 'Profile Updated',
        message: 'Your profile has been successfully updated.',
        color: 'green',
      });
    } catch (err) {
      notifications.show({
        title: 'Could not update your profile',
        message: failureMessage('update your profile', err),
        color: 'red',
      });
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title={t('page.profile.title')}
        description={t('page.profile.subtitle')}
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
            <Title order={2} size="h3" mt="md">{user.displayName || user.name}</Title>
            <Text c="dimmed" size="sm">{roleLabels(user.role)}</Text>
            
            <Divider my="lg" />

            <Stack gap="sm">
              <Group gap="sm" wrap="nowrap">
                <ThemeIcon variant="light" color="gray" size="sm">
                  <Mail size={14} />
                </ThemeIcon>
                <Text size="xs" truncate>{user.username || 'No email on file'}</Text>
              </Group>
              {/*
                A join date and a location were hardcoded here ("Joined March
                2024", "Berlin, Germany"). Neither is stored on the user, so
                every account displayed the same fictional biography.
              */}
            </Stack>
          </Paper>

          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={3} size="h5" mb="md">Your Organizations</Title>
            <Stack gap="sm">
              {user.organizations?.map((org) => (
                <Paper key={org.id} withBorder p="xs" radius="md" bg="gray.0">
                  <Group justify="space-between">
                    <Group gap="xs">
                      <Building2 size={16} color="var(--mantine-color-blue-6)" />
                      <Text size="sm" fw={600}>{org.name}</Text>
                    </Group>
                  </Group>
                </Paper>
              ))}
            </Stack>
          </Paper>
        </Stack>

        <Paper p="xl" radius="lg" withBorder shadow="sm" style={{ gridColumn: 'span 2' }}>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              event.stopPropagation();
              void form.handleSubmit();
            }}
          >
            <Title order={2} size="h4" mb="lg">Public Profile</Title>
            <Stack gap="md">
              <SimpleGrid cols={2}>
                <form.Field name="name" validators={{ onChange: zodField(nameField) }}>
                  {(field) => <TextInput label="Full Name" placeholder="Your name" withAsterisk {...fieldProps(field)} />}
                </form.Field>
                <form.Field name="displayName" validators={{ onChange: zodField(displayNameField) }}>
                  {(field) => (
                    <TextInput
                      label="Display Name"
                      placeholder="Public name"
                      description="Shown to other people in your organization"
                      withAsterisk
                      {...fieldProps(field)}
                    />
                  )}
                </form.Field>
              </SimpleGrid>
              <form.Field name="organization" validators={{ onChange: zodField(optionalField) }}>
                {(field) => <TextInput label="Organization" placeholder="Organization" {...fieldProps(field)} />}
              </form.Field>
              <TextInput label="Email Address" placeholder="Email" value={user.username} disabled />
              
              <Divider my="md" label="Security" labelPosition="center" />
              
              <Box>
                <Text fw={700} size="sm">Password</Text>
                {/*
                  "Last changed 3 months ago" was hardcoded, and the button had
                  no handler. A control that does nothing on click is worse than
                  one that is visibly unavailable: the user cannot tell the
                  difference between "not built" and "broken".
                */}
                <Button variant="light" color="blue" size="xs" onClick={() => setChangingPassword(true)}>
                  Change Password
                </Button>
              </Box>
  
              <Divider my="md" />
  
              {/*
                The submit button reflects real form state: disabled until
                something has actually changed and the values are valid, so the
                user is never left wondering why nothing happened on click.
              */}
              <form.Subscribe selector={(state) => ({ canSubmit: state.canSubmit, isDirty: state.isDirty })}>
                {({ canSubmit, isDirty }) => (
                  <Group justify="flex-end">
                    <Button variant="default" onClick={() => form.reset()} disabled={!isDirty}>
                      Discard Changes
                    </Button>
                    <Button
                      type="submit"
                      color="indigo"
                      loading={updateUser.isPending}
                      disabled={!canSubmit || !isDirty}
                    >
                      Save Profile
                    </Button>
                  </Group>
                )}
              </form.Subscribe>
            </Stack>
          </form>
        </Paper>
      </SimpleGrid>

      <ChangePasswordModal opened={changingPassword} onClose={() => setChangingPassword(false)} />
    </Stack>
  );
}
