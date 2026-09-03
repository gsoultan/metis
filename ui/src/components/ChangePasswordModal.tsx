import { Modal, Stack, PasswordInput, Button, Group, Text, Alert } from '@mantine/core';
import { ShieldAlert } from 'lucide-react';
import { z } from 'zod';
import { useForm, fieldProps, zodField } from './form/AppForm';
import { useChangeOwnPassword } from '../hooks/useUser';
import { useAppStore } from '../store/useAppStore';
import { notifications } from '@mantine/notifications';
import { MIN_PASSWORD_LENGTH } from '../domain/password';

interface ChangePasswordModalProps {
  opened: boolean;
  onClose: () => void;
}

/**
 * Changing one's own password.
 *
 * The current password is asked for because the server demands it, and the
 * server demands it because otherwise a stolen session token would be enough to
 * lock the real owner out of their own account permanently.
 *
 * The rules are duplicated here rather than only enforced server-side so the
 * user finds out before submitting, but the server is still the authority — a
 * refusal is surfaced, not swallowed.
 */
export function ChangePasswordModal({ opened, onClose }: ChangePasswordModalProps) {
  const changePassword = useChangeOwnPassword();
  const clearAuth = useAppStore((state) => state.clearAuth);

  const form = useForm({
    defaultValues: { currentPassword: '', newPassword: '', confirmPassword: '' },
    onSubmit: async ({ value }) => {
      try {
        await changePassword.mutateAsync({
          currentPassword: value.currentPassword,
          newPassword: value.newPassword,
        });
        notifications.show({
          title: 'Password changed',
          message: 'Every session has ended, including this one. Sign in with your new password.',
          color: 'green',
        });
        form.reset();
        onClose();
        // The token in hand was issued before the change, so the server will
        // refuse it from here on. Clearing it sends the user to the login
        // screen deliberately, rather than letting them meet a wall of 401s
        // and conclude the change broke something.
        clearAuth();
      } catch (error) {
        // The server refuses a wrong current password without saying which
        // field was wrong, so the message is shown as-is rather than pinned to
        // an input.
        notifications.show({
          title: 'Could not change your password',
          message: error instanceof Error ? error.message : 'The server refused the change.',
          color: 'red',
        });
      }
    },
  });

  const handleClose = () => {
    form.reset();
    onClose();
  };

  return (
    <Modal opened={opened} onClose={handleClose} title="Change password" centered>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
      >
        <Stack gap="md">
          <Alert icon={<ShieldAlert size={16} />} color="blue" variant="light">
            <Text size="sm">
              Every signed-in session ends, including this one — you will be asked to sign in
              again. That is the point: if you are changing this because somebody else may have
              your password, their session has to end too.
            </Text>
          </Alert>

          <form.Field name="currentPassword" validators={{ onChange: zodField(z.string().min(1, 'Enter your current password')) }}>
            {(field) => (
              <PasswordInput
                {...fieldProps(field)}
                label="Current password"
                placeholder="The password you signed in with"
                autoComplete="current-password"
                data-autofocus
              />
            )}
          </form.Field>

          <form.Field
            name="newPassword"
            validators={{
              onChange: zodField(
                z.string().min(MIN_PASSWORD_LENGTH, `Use at least ${MIN_PASSWORD_LENGTH} characters`),
              ),
            }}
          >
            {(field) => (
              <PasswordInput
                {...fieldProps(field)}
                label="New password"
                placeholder={`At least ${MIN_PASSWORD_LENGTH} characters`}
                autoComplete="new-password"
              />
            )}
          </form.Field>

          <form.Subscribe selector={(state) => state.values.newPassword}>
            {(newPassword) => (
              <form.Field
                name="confirmPassword"
                validators={{
                  onChange: zodField(
                    z.string().refine((value) => value === newPassword, 'The two passwords do not match'),
                  ),
                }}
              >
                {(field) => (
                  <PasswordInput
                    {...fieldProps(field)}
                    label="Confirm new password"
                    placeholder="Type it again"
                    autoComplete="new-password"
                  />
                )}
              </form.Field>
            )}
          </form.Subscribe>

          <Group justify="flex-end" mt="sm">
            <Button variant="subtle" onClick={handleClose} disabled={changePassword.isPending}>
              Cancel
            </Button>
            {/* Disabled until the form is genuinely submittable, so a click
                never silently does nothing. */}
            <form.Subscribe selector={(state) => state.canSubmit}>
              {(canSubmit) => (
                <Button type="submit" loading={changePassword.isPending} disabled={!canSubmit}>
                  Change password
                </Button>
              )}
            </form.Subscribe>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
