import { Button, Group, Modal, MultiSelect, PasswordInput, Stack, Text, TextInput } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useState } from 'react';

import { accountCreation, accountEdit, accountForm, type AccountFormValues } from '../../domain/accountSave';
import { MIN_PASSWORD_LENGTH } from '../../domain/password';
import { ROLE_OPTIONS } from '../../domain/roles';
import { useCreateUser, useSetOrganizationRoles, useUpdateUser } from '../../hooks/useUser';
import { useViewerAccess } from '../../hooks/useViewerAccess';
import { useTranslation } from '../../i18n/context';
import { errorMessage } from '../../services/shared/errors';
import type { ApiOrganizationUser } from '../../services/types';

const ROLE_CHOICES = ROLE_OPTIONS.map(({ value, label, description }) => ({ value, label: `${label} — ${description}` }));

interface AccountDialogProps {
  opened: boolean;
  onClose: () => void;
  /** The account being edited, or null for a new one. */
  account: ApiOrganizationUser | null;
  /** The organization being worked in, which a new account joins. */
  organization: { id: string; name: string };
}

/**
 * The dialog that creates or edits one account.
 *
 * It holds two kinds of role, as the server does: the ones held in this
 * organization, which its administrators grant, and the ones held in every
 * organization, which only a platform administrator may change — shown to
 * everybody else, and not editable. Each is saved only when it changed; see
 * domain/accountSave.ts.
 *
 * Its fields start from the account it was opened for: the page mounts it for
 * each opening rather than copying the account into it with an effect.
 */
export function AccountDialog({ opened, onClose, account, organization }: AccountDialogProps) {
  const { t } = useTranslation();
  const viewer = useViewerAccess();
  const createUser = useCreateUser();
  const updateUser = useUpdateUser();
  const setOrganizationRoles = useSetOrganizationRoles();
  const [form, setForm] = useState<AccountFormValues>(() => accountForm(account));
  const set = <K extends keyof AccountFormValues>(field: K, value: AccountFormValues[K]) =>
    setForm((current) => ({ ...current, [field]: value }));

  const passwordTooShort = form.password.length > 0 && form.password.length < MIN_PASSWORD_LENGTH;
  const canSubmit = account
    ? form.fullName.trim().length > 0
    : form.username.trim().length > 0 && form.fullName.trim().length > 0 && form.password.length >= MIN_PASSWORD_LENGTH;
  const saving = createUser.isPending || updateUser.isPending || setOrganizationRoles.isPending;

  const save = async () => {
    if (account) {
      const edit = accountEdit(account, form, viewer.mayChangeGlobalRoles);
      if (edit.update) {
        await updateUser.mutateAsync({ id: account.id, ...edit.update });
      }
      if (edit.rolesHere) {
        await setOrganizationRoles.mutateAsync({ id: account.id, roles: edit.rolesHere });
      }
      return `${form.fullName} was updated.`;
    }
    await createUser.mutateAsync(accountCreation(organization, form, viewer.mayChangeGlobalRoles));
    return `${form.fullName} can sign in now.`;
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    try {
      const message = await save();
      notifications.show({ title: account ? 'Saved' : 'Added', message, color: 'green' });
      onClose();
    } catch (error: unknown) {
      notifications.show({ title: 'Could not save it', message: errorMessage(error, 'Failed to save the account'), color: 'red' });
    }
  };

  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={700}>{account ? 'Edit account' : 'New account'}</Text>} radius="lg">
      <Stack gap="md">
        <TextInput
          label="Username"
          placeholder="Enter username"
          required
          value={form.username}
          onChange={(e) => set('username', e.currentTarget.value)}
          disabled={!!account}
        />
        {!account && (
          <PasswordInput
            label="Password"
            description={`At least ${MIN_PASSWORD_LENGTH} characters`}
            placeholder="Enter password"
            required
            value={form.password}
            onChange={(e) => set('password', e.currentTarget.value)}
            error={passwordTooShort ? `Needs at least ${MIN_PASSWORD_LENGTH} characters` : undefined}
          />
        )}
        <TextInput
          label="Full Name"
          placeholder="Enter full name"
          required
          value={form.fullName}
          onChange={(e) => set('fullName', e.currentTarget.value)}
        />
        <TextInput
          label="Display Name"
          placeholder="Enter display name"
          value={form.displayName}
          onChange={(e) => set('displayName', e.currentTarget.value)}
        />
        <TextInput
          label="Organization"
          description={account ? undefined : 'New people join the organization you are working in'}
          value={account?.organization?.name ?? organization.name}
          disabled
        />
        <TextInput
          label="Email"
          placeholder="Enter email address"
          value={form.email}
          onChange={(e) => set('email', e.currentTarget.value)}
        />
        <MultiSelect
          label={t('access.rolesHere')}
          description={t('access.rolesHereHint')}
          placeholder={t('access.rolesPlaceholder')}
          data={ROLE_CHOICES}
          value={form.rolesHere}
          onChange={(roles) => set('rolesHere', roles)}
        />
        <MultiSelect
          label={t('access.rolesEverywhere')}
          description={viewer.mayChangeGlobalRoles ? t('access.rolesEverywhereHint') : t('access.rolesEverywhereReadOnly')}
          placeholder={viewer.mayChangeGlobalRoles ? t('access.rolesPlaceholder') : undefined}
          data={ROLE_CHOICES}
          value={form.rolesEverywhere}
          onChange={(roles) => set('rolesEverywhere', roles)}
          disabled={!viewer.mayChangeGlobalRoles}
        />
        <Group justify="flex-end" mt="md">
          <Button variant="light" onClick={onClose}>Cancel</Button>
          <Button onClick={handleSubmit} loading={saving} disabled={!canSubmit}>
            {account ? 'Update' : 'Create'}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
