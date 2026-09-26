import { Box, Button, Divider, Group, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { z } from 'zod';
import { fieldProps, useForm, zodField } from './form/AppForm';
import { useUpdateOwnProfile } from '../hooks/useUser';
import { failureMessage } from '../services/shared/errors';
import type { ApiOrganizationUser } from '../services/types';
import { useAppStore } from '../store/useAppStore';

// Validation lives in a schema so the form and the request payload cannot
// disagree about what is required. The server refuses a longer name, and an
// email that is not one bare address, so the form says so first.
const nameField = z.string().trim().min(1, 'Full name is required').max(120, 'Full name is too long');
const displayNameField = z.string().trim().min(1, 'Display name is required').max(60, 'Display name is too long');
const emailField = z
  .string()
  .trim()
  .max(320, 'This address is too long')
  .refine((value) => value === '' || z.email().safeParse(value).success, 'Enter one address, like name@example.com');

interface ProfileFormValues {
  name: string;
  displayName: string;
  email: string;
}

interface ProfileFormProps {
  /** The account as the server holds it. The form starts from this. */
  profile: ApiOrganizationUser;
  onChangePassword: () => void;
}

/**
 * The part of the signed-in person's account they may change themselves.
 *
 * It saves to /users/me. It used to save through the administrators' update,
 * so it failed for everybody else. It also sent the username as the email,
 * and it offered an "Organization" field that was never sent.
 */
export function ProfileForm({ profile, onChangePassword }: ProfileFormProps) {
  const updateProfile = useUpdateOwnProfile();
  const user = useAppStore((state) => state.user);
  const token = useAppStore((state) => state.token);
  const setAuth = useAppStore((state) => state.setAuth);

  const form = useForm({
    defaultValues: {
      name: profile.full_name ?? '',
      displayName: profile.display_name ?? '',
      email: profile.email ?? '',
    },
    onSubmit: async ({ value }) => handleSubmit(value),
  });

  const handleSubmit = async (values: ProfileFormValues) => {
    const saved = {
      full_name: values.name.trim(),
      display_name: values.displayName.trim(),
      email: values.email.trim(),
    };
    try {
      await updateProfile.mutateAsync(saved);

      // The header and sidebar read the names from the session.
      if (user && token) {
        setAuth({ ...user, name: saved.full_name, displayName: saved.display_name }, token);
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
        <form.Field name="email" validators={{ onChange: zodField(emailField) }}>
          {(field) => (
            <TextInput
              label="Email Address"
              placeholder="name@example.com"
              description="Where notifications about your work are sent"
              {...fieldProps(field)}
            />
          )}
        </form.Field>
        <TextInput
          label="Username"
          value={profile.username}
          description="What you sign in with. Only an administrator can change it."
          disabled
        />

        <Divider my="md" label="Security" labelPosition="center" />

        <Box>
          <Text fw={700} size="sm">Password</Text>
          {/*
            "Last changed 3 months ago" was hardcoded, and the button had
            no handler. A control that does nothing on click is worse than
            one that is visibly unavailable: the user cannot tell the
            difference between "not built" and "broken".
          */}
          <Button variant="light" color="blue" size="xs" onClick={onChangePassword}>
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
                loading={updateProfile.isPending}
                disabled={!canSubmit || !isDirty}
              >
                Save Profile
              </Button>
            </Group>
          )}
        </form.Subscribe>
      </Stack>
    </form>
  );
}
